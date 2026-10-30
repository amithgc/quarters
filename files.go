package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/amithgc/quarters/internal/config"
)

// ---- the file browser and its editor ----
//
// Every call names a folder (a project, or a worktree of one) and a path
// inside it. Nothing outside that folder is listed, read or written, links
// included, and files that look like they hold secrets are never read.

// FileEntry is a file or folder in the file browser.
type FileEntry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	Ignored bool   `json:"ignored"` // git ignores it
	Link    bool   `json:"link"`
}

// DirView is a folder's contents: folders first, then files, by name.
type DirView struct {
	Entries []FileEntry `json:"entries"`
	More    int         `json:"more"` // left out, past maxDirEntries
}

// FileList is every file in a folder (what git doesn't ignore), for the
// file browser's filter.
type FileList struct {
	Files []string `json:"files"`
	More  bool     `json:"more"` // cut at maxListFiles
}

// FileText is a file opened in the editor. When it can't be edited here,
// Note says why (Image holds a picture instead).
type FileText struct {
	Text  string `json:"text"`
	Hash  string `json:"hash"`
	Image string `json:"image"` // a picture, as a data: URL
	Note  string `json:"note"`
	Stamp string `json:"stamp"` // see FileStamp
}

const (
	maxDirEntries = 3000
	maxListFiles  = 20000
	maxImage      = 8 << 20
)

// fileRoot checks that dir is a project or a worktree of one.
func (a *App) fileRoot(dir string) (string, error) {
	dir = filepath.Clean(dir)
	if _, ok := a.project(dir); ok {
		return dir, nil
	}
	// asking git about every project takes a moment: remember for a minute
	if at, ok := a.worktrees.Load(dir); ok && time.Since(at.(time.Time)) < time.Minute {
		return dir, nil
	}
	for _, p := range a.config().Projects {
		if a.isWorktree(config.ExpandPath(p.Path), dir) {
			a.worktrees.Store(dir, time.Now())
			return dir, nil
		}
	}
	return "", fmt.Errorf("not one of your projects")
}

// filePath is rel inside dir, links followed, refused when it leads outside.
func (a *App) filePath(dir, rel string) (string, error) {
	root, err := a.fileRoot(dir)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the project", rel)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%s is gone (deleted, or renamed since)", rel)
		}
		return "", err
	}
	if !within(realRoot, real) {
		return "", fmt.Errorf("%s is outside the project", rel)
	}
	return real, nil
}

// within reports whether path is root or inside it.
func within(root, path string) bool {
	r, err := filepath.Rel(root, path)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// ListDir lists a folder of a project ("" or "." for the project itself).
func (a *App) ListDir(dir, rel string) (DirView, error) {
	full, err := a.filePath(dir, rel)
	if err != nil {
		return DirView{}, err
	}
	list, err := os.ReadDir(full)
	if err != nil {
		return DirView{}, err
	}
	var v DirView
	for _, e := range list {
		name := e.Name()
		if name == ".git" || name == ".DS_Store" {
			continue
		}
		if len(v.Entries) == maxDirEntries {
			v.More++
			continue
		}
		f := FileEntry{Name: name, Dir: e.IsDir(), Link: e.Type()&fs.ModeSymlink != 0}
		if st, err := os.Stat(filepath.Join(full, name)); err == nil { // through links
			f.Dir, f.Size = st.IsDir(), st.Size()
		}
		v.Entries = append(v.Entries, f)
	}
	sort.Slice(v.Entries, func(i, j int) bool {
		a, b := v.Entries[i], v.Entries[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	names := make([]string, len(v.Entries))
	for i, e := range v.Entries {
		names[i] = e.Name
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ignored := gitIgnoredNames(ctx, full, names)
	for i := range v.Entries {
		v.Entries[i].Ignored = ignored[v.Entries[i].Name]
	}
	return v, nil
}

// gitIgnoredNames is which of the names in folder dir git ignores (none
// when dir is not in a repository).
func gitIgnoredNames(ctx context.Context, dir string, names []string) map[string]bool {
	out := map[string]bool{}
	if len(names) == 0 {
		return out
	}
	// not gitCmd: check-ignore refuses literal pathspecs
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "check-ignore", "-z", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(names, "\x00") + "\x00")
	b, _ := cmd.Output() // exit 1: nothing ignored; 128: not a repository
	for _, n := range strings.Split(string(b), "\x00") {
		if n != "" {
			out[n] = true
		}
	}
	return out
}

// FileStatus is what git says changed in a project, by path inside it:
// "M" changed, "A" added, "D" deleted, "?" new (not in git yet).
func (a *App) FileStatus(dir string) (map[string]string, error) {
	root, err := a.fileRoot(dir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	prefix, err := gitCmd(ctx, "-C", root, "rev-parse", "--show-prefix").Output()
	if err != nil {
		return map[string]string{}, nil // not a repository
	}
	out, err := gitCmd(ctx, "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames", "--", ".").Output()
	if err != nil {
		return map[string]string{}, nil
	}
	// paths come relative to the top of the repository
	pre := strings.TrimSpace(string(prefix))
	st := map[string]string{}
	for _, rec := range strings.Split(string(out), "\x00") {
		if len(rec) < 4 || len(st) >= maxListFiles {
			continue
		}
		xy, path := rec[:2], rec[3:]
		if !strings.HasPrefix(path, pre) {
			continue
		}
		path = strings.TrimSuffix(path[len(pre):], "/")
		switch {
		case xy == "??":
			st[path] = "?"
		case strings.Contains(xy, "D"):
			st[path] = "D"
		case strings.Contains(xy, "A"):
			st[path] = "A"
		default:
			st[path] = "M"
		}
	}
	return st, nil
}

// ListFiles is every file in a project git doesn't ignore (outside git:
// every file, past folders whose names start with a dot).
func (a *App) ListFiles(dir string) (FileList, error) {
	root, err := a.fileRoot(dir)
	if err != nil {
		return FileList{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var l FileList
	if out, err := gitCmd(ctx, "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output(); err == nil {
		for _, f := range strings.Split(string(out), "\x00") {
			if f == "" {
				continue
			}
			if len(l.Files) == maxListFiles {
				l.More = true
				break
			}
			l.Files = append(l.Files, f)
		}
		return l, nil
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if len(l.Files) == maxListFiles {
			l.More = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, path)
		l.Files = append(l.Files, rel)
		return nil
	})
	return l, nil
}

var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".bmp": "image/bmp", ".ico": "image/x-icon",
}

// ReadFile opens a project's file for the editor.
func (a *App) ReadFile(dir, rel string) (FileText, error) {
	full, err := a.filePath(dir, rel)
	if err != nil {
		return FileText{}, err
	}
	if secretFile(rel) || secretFile(full) {
		return FileText{Note: "It may hold keys or passwords, so Quarters doesn't open it."}, nil
	}
	st, err := os.Stat(full)
	if err != nil {
		return FileText{}, err
	}
	if !st.Mode().IsRegular() {
		return FileText{}, fmt.Errorf("%s is not a file", rel)
	}
	if kind, ok := imageTypes[strings.ToLower(filepath.Ext(full))]; ok {
		if st.Size() > maxImage {
			return FileText{Note: "A picture too big to show here."}, nil
		}
		b, err := os.ReadFile(full)
		if err != nil {
			return FileText{}, err
		}
		return FileText{Image: "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(b)}, nil
	}
	if st.Size() > maxInstructions {
		return FileText{Note: fmt.Sprintf("Too big to edit here (%d KB; up to %d KB).", st.Size()>>10, maxInstructions>>10)}, nil
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return FileText{}, err
	}
	if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 || !utf8.Valid(b) {
		return FileText{Note: "Not a text file."}, nil
	}
	return FileText{Text: string(b), Hash: textHash(b), Stamp: stamp(st)}, nil
}

// FileStamp changes when a file does (its time and size), so the editor
// notices an agent's edit without reading the file again.
func (a *App) FileStamp(dir, rel string) (string, error) {
	full, err := a.filePath(dir, rel)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	return stamp(st), nil
}

func stamp(st fs.FileInfo) string { return fmt.Sprintf("%d.%d", st.ModTime().UnixNano(), st.Size()) }

// SaveFile writes a file the editor opened. hash is the text it opened (or
// last saved): when the file changed since, nothing is written.
func (a *App) SaveFile(dir, rel, text, hash string) (Text, error) {
	full, err := a.filePath(dir, rel)
	if err != nil {
		return Text{}, err
	}
	if secretFile(rel) || secretFile(full) {
		return Text{}, fmt.Errorf("%s may hold secrets: Quarters doesn't edit it", filepath.Base(rel))
	}
	return writeText(full, text, hash, filepath.Base(rel)+" changed since you opened it (an agent may have edited it)")
}

// OpenFile opens a project's file in its default app on the Mac.
func (a *App) OpenFile(dir, rel string) error {
	full, err := a.filePath(dir, rel)
	if err != nil {
		return err
	}
	return openWith(full, false).Run()
}

// RevealFile shows a project's file in Finder.
func (a *App) RevealFile(dir, rel string) error {
	full, err := a.filePath(dir, rel)
	if err != nil {
		return err
	}
	return revealCmd(full).Run()
}
