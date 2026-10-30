package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ---- search across every project ----

// SearchOptions shape a search.
type SearchOptions struct {
	Case  bool   `json:"case"`  // match case (otherwise ignore it)
	Word  bool   `json:"word"`  // whole words only
	Regex bool   `json:"regex"` // the query is a regular expression
	Glob  string `json:"glob"`  // only paths like this, e.g. *.go
}

// SearchHit is one matching line.
type SearchHit struct {
	Project string `json:"project"` // its full path
	Path    string `json:"path"`    // inside the project
	Line    int    `json:"line"`
	Pre     string `json:"pre"` // the line around the first match
	Match   string `json:"match"`
	Post    string `json:"post"`
}

// SearchResult is what a search found.
type SearchResult struct {
	Hits []SearchHit `json:"hits"`
	More bool        `json:"more"` // stopped at maxHits
	Tool string      `json:"tool"` // ripgrep or git grep
}

const (
	maxHits        = 400
	maxHitsPerFile = 20
)

// SearchProjects searches the files of every project (what git doesn't
// ignore; never files that look like they hold secrets), the most recently
// used projects first.
func (a *App) SearchProjects(query string, o SearchOptions) (SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return SearchResult{Hits: []SearchHit{}}, nil
	}
	if o.Regex {
		if _, err := regexp.Compile(query); err != nil {
			return SearchResult{}, errors.New("not a valid regular expression")
		}
	}
	list := a.Projects()
	sort.SliceStable(list, func(i, j int) bool { return list[i].LastActive > list[j].LastActive })
	var roots []string
	rank := map[string]int{}
	for _, p := range list {
		if st, err := os.Stat(p.Path); err == nil && st.IsDir() {
			rank[p.Path] = len(roots)
			roots = append(roots, p.Path)
		}
	}
	// ripgrep searches in parallel: put each project's hits together, in
	// the projects' order, files by name
	order := func(r SearchResult) SearchResult {
		sort.SliceStable(r.Hits, func(i, j int) bool {
			a, b := r.Hits[i], r.Hits[j]
			if a.Project != b.Project {
				return rank[a.Project] < rank[b.Project]
			}
			if a.Path != b.Path {
				return a.Path < b.Path
			}
			return a.Line < b.Line
		})
		return r
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if cmd, err := a.sessions.Command(ctx, "", "rg", rgArgs(query, o, roots)...); err == nil {
		r, err := runRipgrep(cmd, roots)
		r.Tool = "ripgrep"
		return order(r), err
	}
	r := SearchResult{Hits: []SearchHit{}, Tool: "git grep"}
	for _, root := range roots {
		if len(r.Hits) >= maxHits {
			r.More = true
			break
		}
		r.Hits = append(r.Hits, gitGrep(ctx, root, query, o, maxHits-len(r.Hits))...)
	}
	return r, nil
}

func rgArgs(query string, o SearchOptions, roots []string) []string {
	args := []string{"--json", "--line-number", "--max-columns", "400", "--max-count", strconv.Itoa(maxHitsPerFile)}
	if o.Case {
		args = append(args, "--case-sensitive")
	} else {
		args = append(args, "--ignore-case")
	}
	if o.Word {
		args = append(args, "--word-regexp")
	}
	if !o.Regex {
		args = append(args, "--fixed-strings")
	}
	if g := strings.TrimSpace(o.Glob); g != "" {
		args = append(args, "--glob", g)
	}
	args = append(args, "-e", query, "--")
	return append(args, roots...)
}

// runRipgrep reads rg --json, stopping it at maxHits.
func runRipgrep(cmd *exec.Cmd, roots []string) (SearchResult, error) {
	r := SearchResult{Hits: []SearchHit{}}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return r, err
	}
	if err := cmd.Start(); err != nil {
		return r, err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 256<<10), 4<<20)
	for sc.Scan() {
		var m struct {
			Type string `json:"type"`
			Data struct {
				Path       struct{ Text string } `json:"path"`
				Lines      struct{ Text string } `json:"lines"`
				LineNumber int                   `json:"line_number"`
				Submatches []struct {
					Start int `json:"start"`
					End   int `json:"end"`
				} `json:"submatches"`
			} `json:"data"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.Type != "match" {
			continue
		}
		if len(r.Hits) >= maxHits {
			r.More = true
			_ = cmd.Process.Kill()
			break
		}
		file := m.Data.Path.Text
		root := ""
		for _, p := range roots {
			if within(p, file) && len(p) > len(root) {
				root = p
			}
		}
		rel, _ := filepath.Rel(root, file)
		if root == "" || secretFile(rel) {
			continue
		}
		start, end := 0, 0
		if len(m.Data.Submatches) > 0 {
			start, end = m.Data.Submatches[0].Start, m.Data.Submatches[0].End
		}
		r.Hits = append(r.Hits, hit(root, rel, m.Data.LineNumber, strings.TrimRight(m.Data.Lines.Text, "\r\n"), start, end))
	}
	_ = cmd.Wait() // exit 1: nothing found
	return r, nil
}

// gitGrep searches one project with git grep (without ripgrep).
func gitGrep(ctx context.Context, root, query string, o SearchOptions, limit int) []SearchHit {
	args := []string{"-C", root, "grep", "-n", "-I", "--no-color", "--column", "--untracked", "-m", strconv.Itoa(maxHitsPerFile)}
	if !o.Case {
		args = append(args, "-i")
	}
	if o.Word {
		args = append(args, "-w")
	}
	if o.Regex {
		args = append(args, "-E")
	} else {
		args = append(args, "-F")
	}
	args = append(args, "-e", query, "--")
	if g := strings.TrimSpace(o.Glob); g != "" {
		args = append(args, ":(glob)**/"+g)
	}
	out, _ := exec.CommandContext(ctx, "git", args...).Output()
	var hits []SearchHit
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(line, ":", 4)
		if len(parts) < 4 || secretFile(parts[0]) {
			continue
		}
		n, _ := strconv.Atoi(parts[1])
		col, _ := strconv.Atoi(parts[2])
		text := parts[3]
		start := max(0, col-1)
		end := min(len(text), start+len(query))
		if o.Regex {
			if re, err := regexp.Compile(map[bool]string{true: "", false: "(?i)"}[o.Case] + query); err == nil {
				if loc := re.FindStringIndex(text); loc != nil {
					start, end = loc[0], loc[1]
				}
			}
		}
		hits = append(hits, hit(root, parts[0], n, text, start, end))
		if len(hits) >= limit {
			break
		}
	}
	return hits
}

// hit cuts a matching line around its match (byte offsets start, end),
// keeping it short.
func hit(root, rel string, line int, text string, start, end int) SearchHit {
	if start > len(text) || end > len(text) || start > end {
		start, end = 0, 0
	}
	pre, match, post := text[:start], text[start:end], text[end:]
	pre = strings.TrimLeft(pre, " \t")
	if utf8.RuneCountInString(pre) > 60 {
		r := []rune(pre)
		pre = "…" + string(r[len(r)-60:])
	}
	if utf8.RuneCountInString(post) > 160 {
		post = string([]rune(post)[:160]) + "…"
	}
	return SearchHit{Project: root, Path: filepath.ToSlash(rel), Line: line, Pre: pre, Match: match, Post: post}
}
