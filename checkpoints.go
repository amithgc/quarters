package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- checkpoints: the project's files before each agent prompt ----
//
// A checkpoint is a commit object of every file git doesn't ignore (secret
// looking ones aside), kept under refs/quarters/checkpoints/<unix ms>. It is
// written through a temporary index, so the branch, the index and the stash
// are never touched, and plain git push doesn't send it anywhere.

const (
	checkpointRefs  = "refs/quarters/checkpoints/"
	keepCheckpoints = 50
)

// CheckpointView is one checkpoint and what has changed since.
type CheckpointView struct {
	ID    string `json:"id"` // unix ms
	At    int64  `json:"at"`
	Label string `json:"label"`
	Files int    `json:"files"` // files changed since (to the next checkpoint, or now)
	Added int    `json:"added"`
	Del   int    `json:"del"`
}

// CheckpointFile is a file changed since a checkpoint.
type CheckpointFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // A, M, D
	Added  int    `json:"added"`
	Del    int    `json:"del"`
}

var checkpointMu sync.Map // root -> *sync.Mutex: one snapshot at a time per folder

func lockRoot(root string) func() {
	m, _ := checkpointMu.LoadOrStore(root, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// snapEnv runs git with a temporary index (a copy of the real one, so files
// that didn't change aren't read again) and a fixed author.
type snapEnv struct {
	root, index string
}

func newSnapEnv(ctx context.Context, root string) (*snapEnv, error) {
	real, err := gitRun(ctx, root, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return nil, errors.New("not a git repository")
	}
	f, err := os.CreateTemp("", "quarters-index-*")
	if err != nil {
		return nil, err
	}
	if src, err := os.Open(real); err == nil {
		_, _ = io.Copy(f, src)
		src.Close()
	}
	f.Close()
	// The copy keeps the index's time: git trusts a file whose size and time
	// match its index entry unless it changed in the index's own second, and
	// a newer copy would hide that (a file staged, then edited to the same
	// size within the second, would be snapshotted as staged).
	if st, err := os.Stat(real); err == nil {
		_ = os.Chtimes(f.Name(), st.ModTime(), st.ModTime())
	}
	return &snapEnv{root: root, index: f.Name()}, nil
}

func (e *snapEnv) close() { os.Remove(e.index) }

func (e *snapEnv) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", e.root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+e.index, "GIT_LITERAL_PATHSPECS=1",
		"GIT_AUTHOR_NAME=Quarters", "GIT_AUTHOR_EMAIL=quarters@localhost",
		"GIT_COMMITTER_NAME=Quarters", "GIT_COMMITTER_EMAIL=quarters@localhost")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), cmdErr(err, out)
}

// tree writes the working tree as it is now (what git doesn't ignore, minus
// secret looking files) and returns the tree's id.
func (e *snapEnv) tree(ctx context.Context) (string, error) {
	if _, err := e.git(ctx, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	files, _ := e.git(ctx, "ls-files", "-z")
	var secret []string
	for _, f := range strings.Split(files, "\x00") {
		if f != "" && secretFile(f) {
			secret = append(secret, f)
		}
	}
	if len(secret) > 0 {
		if _, err := e.git(ctx, append([]string{"rm", "-q", "--cached", "--ignore-unmatch", "--"}, secret...)...); err != nil {
			return "", err
		}
	}
	return e.git(ctx, "write-tree")
}

// checkpoint snapshots root's files, unless they're as the last checkpoint
// has them. It returns the new checkpoint's id ("" when none was needed).
func checkpoint(root, label string) (string, error) {
	defer lockRoot(root)()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	e, err := newSnapEnv(ctx, root)
	if err != nil {
		return "", err
	}
	defer e.close()
	tree, err := e.tree(ctx)
	if err != nil {
		return "", err
	}
	refs := checkpointList(ctx, root)
	if len(refs) > 0 {
		if last, _ := gitRun(ctx, root, "rev-parse", refs[0].ref+"^{tree}"); last == tree {
			return "", nil
		}
	}
	args := []string{"commit-tree", tree, "-m", label}
	if head, err := gitRun(ctx, root, "rev-parse", "-q", "--verify", "HEAD"); err == nil && head != "" {
		args = append(args, "-p", head)
	}
	commit, err := e.git(ctx, args...)
	if err != nil {
		return "", err
	}
	id := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if _, err := gitRun(ctx, root, "update-ref", checkpointRefs+id, commit); err != nil {
		return "", err
	}
	// keep the newest
	for _, old := range refs[min(len(refs), keepCheckpoints-1):] {
		_, _ = gitRun(ctx, root, "update-ref", "-d", old.ref)
	}
	return id, nil
}

type checkpointRef struct {
	ref, id, label string
}

// checkpointList lists root's checkpoints, newest first.
func checkpointList(ctx context.Context, root string) []checkpointRef {
	out, err := gitRun(ctx, root, "for-each-ref", "--format=%(refname)\t%(subject)", checkpointRefs)
	if err != nil || out == "" {
		return nil
	}
	var list []checkpointRef
	for _, line := range strings.Split(out, "\n") {
		ref, label, _ := strings.Cut(line, "\t")
		list = append(list, checkpointRef{ref, strings.TrimPrefix(ref, checkpointRefs), label})
	}
	sort.Slice(list, func(i, j int) bool {
		return len(list[i].id) > len(list[j].id) || len(list[i].id) == len(list[j].id) && list[i].id > list[j].id
	})
	return list
}

// numstat sums git diff --numstat between two trees (or commits).
func numstat(ctx context.Context, root, from, to string) (files, added, del int) {
	out, _ := gitRun(ctx, root, "diff", "--numstat", "--no-renames", from, to)
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		files++
		a, _ := strconv.Atoi(f[0]) // "-" for a binary file
		d, _ := strconv.Atoi(f[1])
		added += a
		del += d
	}
	return
}

// nowTree is root's working tree as it is now, written as a tree (no
// checkpoint, no ref).
func nowTree(ctx context.Context, root string) (string, error) {
	defer lockRoot(root)()
	e, err := newSnapEnv(ctx, root)
	if err != nil {
		return "", err
	}
	defer e.close()
	return e.tree(ctx)
}

// Checkpoints lists a project's checkpoints, newest first, each with what
// changed after it: until the next one, or until now for the newest.
func (a *App) Checkpoints(dir string) ([]CheckpointView, error) {
	root, err := a.fileRoot(dir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	refs := checkpointList(ctx, root)
	if len(refs) == 0 {
		return []CheckpointView{}, nil
	}
	now, err := nowTree(ctx, root)
	if err != nil {
		return nil, err
	}
	out := make([]CheckpointView, 0, len(refs))
	for i, r := range refs {
		next := now
		if i > 0 {
			next = refs[i-1].ref
		}
		ms, _ := strconv.ParseInt(r.id, 10, 64)
		v := CheckpointView{ID: r.id, At: ms, Label: r.label}
		v.Files, v.Added, v.Del = numstat(ctx, root, r.ref, next)
		out = append(out, v)
	}
	return out, nil
}

// checkpointCommit checks id and returns its ref.
func checkpointCommit(ctx context.Context, root, id string) (string, error) {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return "", errors.New("no such checkpoint")
	}
	ref := checkpointRefs + id
	if _, err := gitRun(ctx, root, "rev-parse", "-q", "--verify", ref); err != nil {
		return "", errors.New("that checkpoint is gone (only the last 50 are kept)")
	}
	return ref, nil
}

// CheckpointFiles lists the files changed since a checkpoint.
func (a *App) CheckpointFiles(dir, id string) ([]CheckpointFile, error) {
	root, err := a.fileRoot(dir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ref, err := checkpointCommit(ctx, root, id)
	if err != nil {
		return nil, err
	}
	now, err := nowTree(ctx, root)
	if err != nil {
		return nil, err
	}
	status, _ := gitRun(ctx, root, "diff", "--name-status", "--no-renames", ref, now)
	nums, _ := gitRun(ctx, root, "diff", "--numstat", "--no-renames", ref, now)
	byPath := map[string]*CheckpointFile{}
	var files []CheckpointFile
	for _, line := range strings.Split(status, "\n") {
		st, path, ok := strings.Cut(line, "\t")
		if ok {
			files = append(files, CheckpointFile{Path: path, Status: st[:1]})
		}
	}
	for i := range files {
		byPath[files[i].Path] = &files[i]
	}
	for _, line := range strings.Split(nums, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) == 3 && byPath[f[2]] != nil {
			byPath[f[2]].Added, _ = strconv.Atoi(f[0])
			byPath[f[2]].Del, _ = strconv.Atoi(f[1])
		}
	}
	return files, nil
}

// CheckpointDiff shows how a file changed since a checkpoint.
func (a *App) CheckpointDiff(dir, id, file string) (FileDiff, error) {
	root, err := a.fileRoot(dir)
	if err != nil {
		return FileDiff{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ref, err := checkpointCommit(ctx, root, id)
	if err != nil {
		return FileDiff{}, err
	}
	d := FileDiff{File: file}
	if secretFile(file) {
		d.Note = "Not shown: the file may hold secrets."
		return d, nil
	}
	now, err := nowTree(ctx, root)
	if err != nil {
		return d, err
	}
	out, err := gitCmd(ctx, "-C", root, "diff", "--no-color", "--no-ext-diff", "--no-renames", ref, now, "--", file).Output()
	if err != nil {
		return d, cmdErr(err, out)
	}
	if len(out) == 0 {
		d.Note = "No changes since the checkpoint."
	}
	d.Diff = clipText(out)
	return d, nil
}

// RestoreCheckpoint puts a project's files back as they were at a
// checkpoint: changed and deleted files come back, files made since go. It
// takes a checkpoint of how things are first, so a restore can be undone.
// The branch, the index and ignored files are left alone.
func (a *App) RestoreCheckpoint(dir, id string) error {
	root, err := a.fileRoot(dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ref, err := checkpointCommit(ctx, root, id)
	if err != nil {
		return err
	}
	ms, _ := strconv.ParseInt(id, 10, 64)
	if _, err := checkpoint(root, "Before restoring the checkpoint of "+time.UnixMilli(ms).Format("15:04:05")); err != nil {
		return fmt.Errorf("couldn't save how things are first: %v", err)
	}
	defer lockRoot(root)()
	defer a.forgetGit(root)
	e, err := newSnapEnv(ctx, root)
	if err != nil {
		return err
	}
	defer e.close()
	now, err := e.tree(ctx)
	if err != nil {
		return err
	}
	// files made since the checkpoint go
	made, _ := gitRun(ctx, root, "diff", "--name-only", "--no-renames", "--diff-filter=A", ref, now)
	for _, f := range strings.Split(made, "\n") {
		if f == "" || secretFile(f) {
			continue
		}
		full := filepath.Join(root, f)
		if within(root, full) {
			_ = os.Remove(full)
		}
	}
	// the rest is written back from the checkpoint, through the temporary index
	if _, err := e.git(ctx, "read-tree", ref); err != nil {
		return err
	}
	_, err = e.git(ctx, "checkout-index", "-a", "-f")
	return err
}

// lastCheckpoint keeps one checkpoint attempt per folder every few seconds:
// Enter pressed twice in a row (a menu, then a prompt) is one turn.
var lastCheckpoint sync.Map // root -> time.Time

// beforeInput takes a checkpoint when text typed into an agent's tab
// submits a prompt (Enter, not inside a paste).
func (a *App) beforeInput(id int, data string) {
	if !strings.Contains(data, "\r") || strings.HasPrefix(data, "\x1b[200~") || !a.current().On("checkpoints") {
		return
	}
	info, ok := a.sessions.Info(id)
	if !ok || info.Exited {
		return
	}
	m := metaOf(info)
	if m.Kind != "claude" || m.Path == "" {
		return
	}
	root := m.Path
	if m.Cwd != "" {
		root = m.Cwd
	}
	if t, ok := lastCheckpoint.Load(root); ok && time.Since(t.(time.Time)) < 5*time.Second {
		return
	}
	lastCheckpoint.Store(root, time.Now())
	label := "Before a prompt"
	if m.Title != "" {
		label += " in " + m.Title
	}
	go func() {
		if _, err := checkpoint(root, label); err != nil && !strings.Contains(err.Error(), "not a git repository") {
			a.emit("flash", "checkpoint: "+err.Error())
		}
	}()
}
