package stats

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// FileChange is one changed file.
type FileChange struct {
	Status string // two-letter porcelain status, trimmed (M, A, D, ??, …)
	Path   string
	Added  int
	Del    int
}

// Git is the working tree state.
type Git struct {
	IsRepo        bool
	Branch        string
	Ahead, Behind int
	Files         []FileChange
}

// GitStatus runs git status and git diff --numstat in dir.
func GitStatus(dir string) Git {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain=v1", "--branch", "--untracked-files=normal").Output()
	if err != nil {
		return Git{}
	}
	g := Git{IsRepo: true}
	index := map[string]int{}
	for i, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if i == 0 && strings.HasPrefix(line, "## ") {
			g.Branch, g.Ahead, g.Behind = parseBranch(line[3:])
			continue
		}
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if _, to, ok := strings.Cut(path, " -> "); ok {
			path = to
		}
		index[path] = len(g.Files)
		g.Files = append(g.Files, FileChange{Status: strings.TrimSpace(line[:2]), Path: strings.Trim(path, `"`)})
	}
	if len(g.Files) == 0 {
		return g
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "diff", "--numstat", "HEAD").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) != 3 {
				continue
			}
			path := parts[2]
			if _, to, ok := strings.Cut(path, " => "); ok {
				path = strings.TrimSuffix(to, "}")
			}
			if i, ok := index[path]; ok {
				g.Files[i].Added, _ = strconv.Atoi(parts[0])
				g.Files[i].Del, _ = strconv.Atoi(parts[1])
			}
		}
	}
	return g
}

// parseBranch reads "main...origin/main [ahead 2, behind 1]".
func parseBranch(s string) (branch string, ahead, behind int) {
	head, info, _ := strings.Cut(s, " [")
	branch, _, _ = strings.Cut(head, "...")
	branch = strings.TrimPrefix(branch, "No commits yet on ")
	for _, part := range strings.Split(strings.TrimSuffix(info, "]"), ", ") {
		if n, ok := strings.CutPrefix(part, "ahead "); ok {
			ahead, _ = strconv.Atoi(n)
		}
		if n, ok := strings.CutPrefix(part, "behind "); ok {
			behind, _ = strconv.Atoi(n)
		}
	}
	return branch, ahead, behind
}
