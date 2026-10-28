package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/amithgc/quarters/internal/projects"
	"github.com/amithgc/quarters/internal/stats"
)

// ---- git status of each project, refreshed in the background ----

type gitEntry struct {
	g  stats.Git
	at time.Time
}

func (a *App) gitOf(path string) (stats.Git, bool) {
	a.fmu.Lock()
	defer a.fmu.Unlock()
	e, ok := a.f.git[path]
	if (!ok || time.Since(e.at) > 15*time.Second) && a.f.gitBusy < 4 {
		a.f.gitBusy++
		if !ok {
			a.f.git[path] = gitEntry{at: time.Now()} // don't start it twice
		} else {
			e.at = time.Now()
			a.f.git[path] = e
		}
		go func() {
			g := stats.GitStatus(path)
			a.fmu.Lock()
			a.f.git[path] = gitEntry{g, time.Now()}
			a.f.gitBusy--
			a.fmu.Unlock()
		}()
	}
	return e.g, ok
}

// ---- what Claude changed: diffs ----

// FileDiff is a file's changes since the last commit.
type FileDiff struct {
	File  string `json:"file"` // relative to the project
	Diff  string `json:"diff"`
	Note  string `json:"note"`
	Added bool   `json:"added"` // new file (all lines added)
}

const maxDiff = 400 << 10

// Diff shows how a file in a project differs from the last commit.
func (a *App) Diff(path, file string) (FileDiff, error) {
	root := path
	if _, ok := a.project(path); !ok {
		return FileDiff{}, fmt.Errorf("not one of your projects")
	}
	full := file
	if !filepath.IsAbs(full) {
		full = filepath.Join(root, file)
	}
	full = filepath.Clean(full)
	// only files inside the project (or one of its worktrees)
	inside := func(dir string) bool {
		rel, err := filepath.Rel(dir, full)
		return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
	}
	if !inside(root) {
		found := false
		for _, w := range a.worktreeList(path) {
			if inside(w.Dir) {
				root, found = w.Dir, true
			}
		}
		if !found {
			return FileDiff{}, fmt.Errorf("%s is outside the project", file)
		}
	}
	if real, err := filepath.EvalSymlinks(full); err == nil {
		realRoot, _ := filepath.EvalSymlinks(root)
		if rel, err := filepath.Rel(realRoot, real); err != nil || strings.HasPrefix(rel, "..") {
			return FileDiff{}, fmt.Errorf("%s is outside the project", file)
		}
	}
	rel, _ := filepath.Rel(root, full)
	d := FileDiff{File: rel}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if gitIgnored(ctx, root, rel) || secretFile(rel) {
		d.Note = "Not shown: the file is ignored by git or may hold secrets."
		return d, nil
	}
	out, err := gitCmd(ctx, "-C", root, "diff", "--no-color", "--no-ext-diff", "HEAD", "--", rel).Output()
	if err == nil && len(out) > 0 {
		d.Diff = clipText(out)
		return d, nil
	}
	st, statErr := os.Stat(full)
	switch {
	case statErr != nil:
		d.Note = "The file is gone (deleted, or renamed since)."
	case st.IsDir():
		d.Note = "A folder."
	case err != nil:
		d.Note = "Not in a git repository — the whole file:"
		d.Diff, d.Added = readText(full), true
	default:
		tracked := gitCmd(ctx, "-C", root, "ls-files", "--error-unmatch", "--", rel).Run() == nil
		if tracked {
			d.Note = "No changes since the last commit."
		} else {
			d.Note = "A new file (not committed yet):"
			d.Diff, d.Added = readText(full), true
		}
	}
	return d, nil
}

// gitCmd runs git with paths taken literally (a file named ":(glob)*" is
// just a file) and without taking locks that would get in the way of the
// user's own git commands.
func gitCmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_LITERAL_PATHSPECS=1", "GIT_OPTIONAL_LOCKS=0")
	return cmd
}

// gitIgnored reports whether git ignores rel in root. Anything but a clear
// "not ignored" (git refusing a strange name, say) counts as ignored.
func gitIgnored(ctx context.Context, root, rel string) bool {
	out, err := exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "-q", "--", rel).CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false // not ignored
	case !errors.As(err, &exit), strings.Contains(string(out), "not a git repository"):
		return false // no git, or not a repository: nothing is ignored
	}
	return true
}

// secretFile reports whether a file looks like it holds keys or passwords,
// which are never shown whole (and so never sent to a browser).
func secretFile(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	if base == ".env" || strings.HasPrefix(base, ".env.") && !strings.HasSuffix(base, ".example") && !strings.HasSuffix(base, ".sample") {
		return true
	}
	for _, s := range []string{".pem", ".key", ".p12", ".pfx", ".keystore", ".jks"} {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	switch base {
	case "id_rsa", "id_ed25519", "id_ecdsa", ".npmrc", ".pypirc", ".netrc", "credentials", "credentials.json", "secrets.json", "service-account.json":
		return true
	}
	return false
}

func clipText(b []byte) string {
	if len(b) > maxDiff {
		b = append(b[:maxDiff], "\n… (cut)"...)
	}
	return string(b)
}

func readText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, maxDiff+1)
	n, _ := f.Read(buf)
	if bytes.IndexByte(buf[:n], 0) >= 0 {
		return "(binary file)"
	}
	return clipText(buf[:n])
}

// ---- git worktrees ----

// WorktreeView is a git worktree of a project.
type WorktreeView struct {
	Dir    string `json:"dir"`
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Main   bool   `json:"main"` // the project folder itself
	Dirty  int    `json:"dirty"`
}

// Worktrees lists a project's git worktrees with how many files changed in
// each (none when it is not a repository).
func (a *App) Worktrees(path string) []WorktreeView {
	out := a.worktreeList(path)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := range out {
		if st, err := gitCmd(ctx, "-C", out[i].Dir, "status", "--porcelain").Output(); err == nil {
			out[i].Dirty = bytes.Count(st, []byte("\n"))
		}
	}
	return out
}

// worktreeList lists a project's git worktrees, without their status.
func (a *App) worktreeList(path string) []WorktreeView {
	out := []WorktreeView{}
	if _, ok := a.project(path); !ok {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := gitCmd(ctx, "-C", path, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return out
	}
	var cur *WorktreeView
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			out = append(out, WorktreeView{Dir: strings.TrimPrefix(line, "worktree ")})
			cur = &out[len(out)-1]
			cur.Name = filepath.Base(cur.Dir)
			cur.Main = len(out) == 1
		case cur != nil && strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case cur != nil && line == "detached":
			cur.Branch = "(detached)"
		}
	}
	return out
}

func (a *App) isWorktree(path, dir string) bool {
	for _, w := range a.worktreeList(path) {
		if w.Dir == dir {
			return true
		}
	}
	return false
}

var branchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,59}$`)

// CreateWorktree makes a worktree of a project on a new branch (or an
// existing one) next to it, in <project>.worktrees/<name>, and returns its folder.
func (a *App) CreateWorktree(path, branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	if _, ok := a.project(path); !ok {
		return "", fmt.Errorf("not one of your projects")
	}
	if !branchRe.MatchString(branch) || strings.Contains(branch, "..") || strings.HasSuffix(branch, ".lock") || strings.HasSuffix(branch, "/") {
		return "", fmt.Errorf("use a simple branch name, e.g. fix-login")
	}
	dir := filepath.Join(filepath.Dir(path), filepath.Base(path)+".worktrees", strings.ReplaceAll(branch, "/", "-"))
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("%s already exists", projects.ShortPath(dir))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exists := gitCmd(ctx, "-C", path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
	args := []string{"-C", path, "worktree", "add"}
	if exists {
		args = append(args, "--", dir, branch)
	} else {
		args = append(args, "-b", branch, "--", dir)
	}
	if out, err := gitCmd(ctx, args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %s", strings.TrimSpace(string(out)))
	}
	return dir, nil
}

// RemoveWorktree deletes a worktree (its branch stays). A worktree with
// uncommitted changes is only removed with force.
func (a *App) RemoveWorktree(path, dir string, force bool) error {
	var w *WorktreeView
	list := a.worktreeList(path)
	for i := range list {
		if list[i].Dir == dir {
			w = &list[i]
		}
	}
	if w == nil || w.Main {
		return fmt.Errorf("not a worktree of this project")
	}
	for _, info := range a.sessions.List() {
		if m := metaOf(info); m.Cwd == dir && !info.Exited {
			return fmt.Errorf("close the tabs running in it first")
		}
	}
	args := []string{"-C", path, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := gitCmd(ctx, args...).CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "modified or untracked") {
			return fmt.Errorf("it has uncommitted changes")
		}
		return fmt.Errorf("git worktree remove: %s", msg)
	}
	return nil
}
