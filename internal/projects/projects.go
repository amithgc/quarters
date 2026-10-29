// Package projects finds the projects Claude Code has been used in, by
// reading the session logs under ~/.claude/projects, plus any added by hand.
package projects

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amithgc/quarters/internal/config"
)

// Project is one entry from the config, with what we know about it.
type Project struct {
	Name        string
	Path        string
	Group       string
	Icon        string // full path of the icon image, "" for none
	Command     []string
	Automations []config.Automation
	Yolo        *bool
	Harness     string // the project's own choice, "" for the default
	Color       string // "#rrggbb" or "accent", "" for one from the name
	Branch      string
	LastActive  time.Time
	Missing     bool // the folder does not exist
	// ClaudeDir is the folder under ~/.claude/projects holding its session
	// logs.
	ClaudeDir string
}

// ClaudeRoot is ~/.claude/projects.
func ClaudeRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// EncodeDir returns the folder name Claude Code uses for a project path.
func EncodeDir(path string) string { return nonAlnum.ReplaceAllString(path, "-") }

// Load lists the projects in the config, in config order.
func Load(cfg *config.Config) []Project {
	root := ClaudeRoot()
	list := make([]Project, 0, len(cfg.Projects))
	for _, cp := range cfg.Projects {
		if strings.TrimSpace(cp.Path) == "" {
			continue
		}
		path := config.ExpandPath(cp.Path)
		p := Project{Name: cp.Name, Path: path, Group: cp.Group, Command: cp.Command, Automations: cp.Automations, Yolo: cp.Yolo, Harness: cp.Harness, Color: cp.Color}
		if p.Name == "" {
			p.Name = filepath.Base(path)
		}
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			p.Missing = true
		}
		p.Icon = IconPath(cp)
		p.ClaudeDir = filepath.Join(root, EncodeDir(path))
		if _, mtime := newestLog(p.ClaudeDir); !mtime.IsZero() {
			p.LastActive = mtime
		}
		p.Branch = GitBranch(path)
		list = append(list, p)
	}
	return list
}

// IconPath is the full path of a project's icon ("" for none). A relative
// one is inside the project folder.
func IconPath(cp config.Project) string {
	icon := strings.TrimSpace(cp.Icon)
	switch {
	case icon == "":
		return ""
	case strings.HasPrefix(icon, "/") || strings.HasPrefix(icon, "~"):
		return config.ExpandPath(icon)
	default:
		return filepath.Join(config.ExpandPath(cp.Path), icon)
	}
}

// Seed suggests up to n projects for a new config: the folders Claude Code
// was used in most recently.
func Seed(n int) []config.Project {
	type found struct {
		path  string
		mtime time.Time
	}
	var all []found
	seen := map[string]bool{}
	entries, _ := os.ReadDir(ClaudeRoot())
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		newest, mtime := newestLog(filepath.Join(ClaudeRoot(), e.Name()))
		cwd := readCwd(newest)
		if cwd == "" || seen[cwd] || strings.HasPrefix(cwd, "/private/") ||
			strings.HasPrefix(cwd, "/var/") || strings.HasPrefix(cwd, "/tmp/") {
			continue
		}
		if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
			continue
		}
		seen[cwd] = true
		all = append(all, found{cwd, mtime})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mtime.After(all[j].mtime) })
	var out []config.Project
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, config.Project{Name: filepath.Base(all[i].path), Path: ShortPath(all[i].path)})
	}
	return out
}

// NewestLog returns the most recently modified session log in dir.
func NewestLog(dir string) string {
	p, _ := newestLog(dir)
	return p
}

// The project list asks for every project's newest log every couple of
// seconds, and a folder can hold hundreds of them. Between full looks, only
// the folder (a new log changes it) and the newest log are checked.
const logRescan = 30 * time.Second

var newest = struct {
	sync.Mutex
	m map[string]newestSeen
}{m: map[string]newestSeen{}}

type newestSeen struct {
	dirMod time.Time
	path   string
	at     time.Time
}

func newestLog(dir string) (string, time.Time) {
	st, err := os.Stat(dir)
	if err != nil {
		return "", time.Time{}
	}
	newest.Lock()
	e, ok := newest.m[dir]
	newest.Unlock()
	if ok && e.dirMod.Equal(st.ModTime()) && time.Since(e.at) < logRescan {
		if e.path == "" {
			return "", time.Time{}
		}
		if fi, err := os.Stat(e.path); err == nil {
			return e.path, fi.ModTime()
		}
	}
	path, mtime := scanLogs(dir)
	newest.Lock()
	newest.m[dir] = newestSeen{st.ModTime(), path, time.Now()}
	newest.Unlock()
	return path, mtime
}

func scanLogs(dir string) (string, time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}
	}
	var best string
	var bestT time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestT) {
			best, bestT = filepath.Join(dir, e.Name()), info.ModTime()
		}
	}
	return best, bestT
}

var cwdKey = []byte(`"cwd":"`)

// readCwd finds the first "cwd" field in a session log.
func readCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 32*1024*1024)
	for i := 0; i < 200 && sc.Scan(); i++ {
		line := sc.Bytes()
		k := bytes.Index(line, cwdKey)
		if k < 0 {
			continue
		}
		rest := line[k+len(cwdKey):]
		if end := bytes.IndexByte(rest, '"'); end > 0 {
			return string(rest[:end])
		}
	}
	return ""
}

// GitBranch reads the current branch from .git/HEAD without running git.
func GitBranch(path string) string {
	gitPath := filepath.Join(path, ".git")
	st, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	if !st.IsDir() {
		// Worktree or submodule: ".git" is a file holding "gitdir: <path>".
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return ""
		}
		dir := strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir:"))
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(path, dir)
		}
		gitPath = dir
	}
	head, err := os.ReadFile(filepath.Join(gitPath, "HEAD"))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(head))
	if ref, ok := strings.CutPrefix(s, "ref: refs/heads/"); ok {
		return ref
	}
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}

// ShortPath replaces the home directory with ~.
func ShortPath(path string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
