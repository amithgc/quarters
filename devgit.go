package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ---- commit, push and pull requests from the side panel ----
//
// Everything runs the user's own git and gh, with their credentials;
// Quarters keeps nothing. Files that look like they hold secrets are never
// committed, nor sent to the agent that drafts a commit message.

// gitRun runs git in dir and returns its output; a failure carries what
// git said.
func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), cmdErr(err, out)
}

// cmdErr is err with the last lines the program printed, which say why.
func cmdErr(err error, out []byte) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("it took too long, and was stopped")
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	if msg := strings.TrimSpace(strings.Join(lines, "\n")); msg != "" {
		return errors.New(msg)
	}
	return err
}

// changedPaths lists what git status sees changed in root (untracked files
// one by one).
func changedPaths(ctx context.Context, root string) ([]string, error) {
	cmd := gitCmd(ctx, "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	out, err := cmd.Output()
	if err != nil {
		return nil, cmdErr(err, out)
	}
	var paths []string
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		e := parts[i]
		if len(e) < 4 {
			continue
		}
		paths = append(paths, e[3:])
		if e[0] == 'R' || e[0] == 'C' {
			i++ // the old name of a rename follows
		}
	}
	return paths, nil
}

// forgetGit drops the cached git status of dir, so the panel shows the
// commit (or push) at once.
func (a *App) forgetGit(dir string) {
	a.fmu.Lock()
	delete(a.f.git, dir)
	a.fmu.Unlock()
	prs.Lock()
	delete(prs.m, dir)
	prs.Unlock()
}

// GitCommit stages every change git doesn't ignore in a project (or one of
// its worktrees) and commits it with message; with push, it pushes too.
// It says what it did.
func (a *App) GitCommit(dir, message string, push bool) (string, error) {
	root, err := a.fileRoot(dir)
	if err != nil {
		return "", err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return "", errors.New("write a commit message first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	paths, err := changedPaths(ctx, root)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", errors.New("nothing to commit: the working tree is clean")
	}
	var secret []string
	for _, p := range paths {
		if secretFile(p) {
			secret = append(secret, p)
		}
	}
	if len(secret) > 0 {
		return "", fmt.Errorf("not committed: %s may hold secrets. Add it to .gitignore (or remove it), then commit", strings.Join(secret, ", "))
	}
	defer a.forgetGit(root)
	if _, err := gitRun(ctx, root, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := gitRun(ctx, root, "commit", "-q", "-m", message); err != nil {
		return "", err
	}
	hash, _ := gitRun(ctx, root, "rev-parse", "--short", "HEAD")
	done := fmt.Sprintf("committed %s (%d file%s)", hash, len(paths), plural(len(paths)))
	if push {
		if _, err := a.push(root); err != nil {
			return done, fmt.Errorf("%s, but the push failed: %v", done, err)
		}
		done += " and pushed"
	}
	return done, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// GitPush pushes a project's branch, setting its upstream the first time.
func (a *App) GitPush(dir string) (string, error) {
	root, err := a.fileRoot(dir)
	if err != nil {
		return "", err
	}
	defer a.forgetGit(root)
	return a.push(root)
}

func (a *App) push(root string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	branch, err := gitRun(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "HEAD" {
		return "", errors.New("not on a branch (a detached HEAD): nothing to push")
	}
	args := []string{"push"}
	if _, err := gitRun(ctx, root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		args = []string{"push", "-u", "origin", "HEAD"} // the first push of this branch
	}
	cmd, err := a.sessions.Command(ctx, root, "git", args...)
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", cmdErr(err, out)
	}
	return "pushed " + branch, nil
}

// diffForMessage is the change to describe, for the agent drafting a commit
// message: tracked changes, then the start of each new file. Files that may
// hold secrets are left out; it's cut at 60 KB.
func diffForMessage(ctx context.Context, root string) (string, error) {
	const limit = 60 << 10
	var b strings.Builder
	out, err := gitCmd(ctx, "-C", root, "diff", "HEAD", "--no-color", "--no-ext-diff").Output()
	if err != nil {
		out, _ = gitCmd(ctx, "-C", root, "diff", "--cached", "--no-color", "--no-ext-diff").Output() // no commit yet
	}
	skip := false
	for _, line := range strings.SplitAfter(string(out), "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			f := line[strings.LastIndex(line, " b/")+3:]
			skip = secretFile(strings.TrimSpace(f))
		}
		if !skip {
			b.WriteString(line)
		}
	}
	untracked, _ := gitCmd(ctx, "-C", root, "ls-files", "--others", "--exclude-standard").Output()
	for _, f := range strings.Split(strings.TrimSpace(string(untracked)), "\n") {
		if f == "" || secretFile(f) || b.Len() > limit {
			continue
		}
		t := readText(filepath.Join(root, f))
		if len(t) > 2000 {
			t = t[:2000] + "\n…"
		}
		fmt.Fprintf(&b, "\nNew file %s:\n%s\n", f, t)
	}
	text := b.String()
	if strings.TrimSpace(text) == "" {
		return "", errors.New("nothing to describe: the working tree is clean")
	}
	if len(text) > limit {
		text = text[:limit] + "\n… (cut)"
	}
	return text, nil
}

var fence = regexp.MustCompile("(?s)^```[a-z]*\n(.*?)\n```$")

// DraftCommitMessage asks Claude Code (claude -p) to write a commit
// message for a project's changes, in the style of its recent commits.
func (a *App) DraftCommitMessage(dir string) (string, error) {
	root, err := a.fileRoot(dir)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	diff, err := diffForMessage(ctx, root)
	if err != nil {
		return "", err
	}
	recent, _ := gitRun(ctx, root, "log", "-8", "--format=%s")
	prompt := "Write a git commit message for the change below. The first line is a summary in the imperative mood, under 60 characters, with no full stop. " +
		"If the change needs explaining, add a blank line and a short body (what changed and why, wrapped at 72 characters). " +
		"Match the style of the repository's recent commit subjects. Reply with the message only: no quotes, no code fences, no preamble."
	if recent != "" {
		prompt += "\n\nRecent commit subjects:\n" + recent
	}
	cmd, err := a.sessions.Command(ctx, root, "claude", "-p", prompt, "--model", "haiku")
	if err != nil {
		return "", errors.New("✨ uses Claude Code to draft the message, and claude isn't installed here")
	}
	cmd.Stdin = strings.NewReader(diff)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", cmdErr(err, errOut.Bytes())
	}
	msg := strings.TrimSpace(out.String())
	if m := fence.FindStringSubmatch(msg); m != nil {
		msg = strings.TrimSpace(m[1])
	}
	if msg == "" {
		return "", errors.New("the agent didn't write a message; try again")
	}
	return msg, nil
}

// CreatePullRequest pushes a project's branch if it needs to, then opens a
// pull request for it with gh (title and body from its commits), and
// returns its address.
func (a *App) CreatePullRequest(dir string) (string, error) {
	root, err := a.fileRoot(dir)
	if err != nil {
		return "", err
	}
	defer a.forgetGit(root)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := a.sessions.Command(ctx, root, "gh"); err != nil {
		return "", errors.New("pull requests need the GitHub CLI: brew install gh, then gh auth login")
	}
	if _, err := a.push(root); err != nil {
		return "", fmt.Errorf("couldn't push the branch first: %v", err)
	}
	cmd, _ := a.sessions.Command(ctx, root, "gh", "pr", "create", "--fill")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", cmdErr(err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}

// PRView is a branch's pull request and how its checks went.
type PRView struct {
	Has     bool     `json:"has"`
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	URL     string   `json:"url"`
	State   string   `json:"state"` // OPEN, MERGED, CLOSED
	Draft   bool     `json:"draft"`
	Passed  int      `json:"passed"`
	Failed  []string `json:"failed"` // the checks that failed
	Pending int      `json:"pending"`
	NoGh    bool     `json:"noGh"` // the GitHub CLI isn't installed
	Note    string   `json:"note"` // why it couldn't be read
}

type prEntry struct {
	v    PRView
	at   time.Time
	busy bool
}

var prs = struct {
	sync.Mutex
	m map[string]prEntry
}{m: map[string]prEntry{}}

// PullRequest is the pull request of a project's branch, as gh last told
// it (asked again in the background after a minute).
func (a *App) PullRequest(dir string) PRView {
	root, err := a.fileRoot(dir)
	if err != nil {
		return PRView{}
	}
	prs.Lock()
	defer prs.Unlock()
	e := prs.m[root]
	if time.Since(e.at) > time.Minute && !e.busy {
		e.busy = true
		prs.m[root] = e
		go func() {
			v := a.readPR(root)
			prs.Lock()
			prs.m[root] = prEntry{v: v, at: time.Now()}
			prs.Unlock()
		}()
	}
	return e.v
}

func (a *App) readPR(root string) PRView {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd, err := a.sessions.Command(ctx, root, "gh", "pr", "view", "--json", "number,title,url,state,isDraft,statusCheckRollup")
	if err != nil {
		return PRView{NoGh: true}
	}
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	out, err := cmd.Output()
	if err != nil {
		msg := errOut.String()
		for _, none := range []string{"no pull requests found", "no default remote", "no git remotes", "known GitHub host", "not a git repository"} {
			if strings.Contains(msg, none) {
				return PRView{} // no pull request, or not a GitHub repository
			}
		}
		return PRView{Note: strings.TrimSpace(firstLine(msg))}
	}
	return parsePR(out)
}

// parsePR reads gh pr view --json number,title,url,state,isDraft,statusCheckRollup.
func parsePR(out []byte) PRView {
	var r struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		URL     string `json:"url"`
		State   string `json:"state"`
		IsDraft bool   `json:"isDraft"`
		Checks  []struct {
			Name       string `json:"name"`
			Context    string `json:"context"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			State      string `json:"state"`
		} `json:"statusCheckRollup"`
	}
	if json.Unmarshal(out, &r) != nil {
		return PRView{}
	}
	v := PRView{Has: true, Number: r.Number, Title: r.Title, URL: r.URL, State: r.State, Draft: r.IsDraft}
	for _, c := range r.Checks {
		name := c.Name
		if name == "" {
			name = c.Context
		}
		result := c.Conclusion // a check run
		if c.State != "" {
			result = c.State // a commit status
		}
		switch {
		case c.Status != "" && c.Status != "COMPLETED", result == "PENDING", result == "EXPECTED":
			v.Pending++
		case result == "SUCCESS", result == "NEUTRAL", result == "SKIPPED":
			v.Passed++
		default:
			v.Failed = append(v.Failed, name)
		}
	}
	return v
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
