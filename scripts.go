package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---- a project's scripts, one click away ----

// Script is something a project knows how to run: a package.json script, a
// make target, a just recipe, or a cargo or go command.
type Script struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	From    string `json:"from"` // package.json, Makefile, justfile, deno.json, Cargo.toml, go.mod
}

const maxScripts = 60

// Scripts finds what a project (or one of its worktrees) knows how to run.
func (a *App) Scripts(dir string) []Script {
	root, err := a.fileRoot(dir)
	if err != nil {
		return nil
	}
	return findScripts(root)
}

func findScripts(root string) []Script {
	var out []Script
	add := func(from string, names []string, cmd func(string) string) {
		for _, n := range names {
			if len(out) < maxScripts {
				out = append(out, Script{Name: n, Command: cmd(n), From: from})
			}
		}
	}
	if names := jsonKeys(filepath.Join(root, "package.json"), "scripts"); len(names) > 0 {
		run := packageManager(root)
		var keep []string
		for _, n := range names {
			switch n {
			case "preinstall", "install", "postinstall", "prepare", "prepublishOnly", "prepack", "postpack":
				continue
			}
			keep = append(keep, n)
		}
		add("package.json", keep, func(n string) string { return run + " " + shellWord(n) })
	}
	if names := jsonKeys(filepath.Join(root, "deno.json"), "tasks"); len(names) > 0 {
		add("deno.json", names, func(n string) string { return "deno task " + shellWord(n) })
	}
	for _, f := range []string{"Makefile", "makefile", "GNUmakefile"} {
		if names := makeTargets(filepath.Join(root, f)); len(names) > 0 {
			add("Makefile", names, func(n string) string { return "make " + shellWord(n) })
			break
		}
	}
	for _, f := range []string{"justfile", "Justfile", ".justfile"} {
		if names := justRecipes(filepath.Join(root, f)); len(names) > 0 {
			add("justfile", names, func(n string) string { return "just " + shellWord(n) })
			break
		}
	}
	if exists(filepath.Join(root, "Cargo.toml")) {
		names := []string{"build", "test", "clippy"}
		if exists(filepath.Join(root, "src", "main.rs")) {
			names = append([]string{"run"}, names...)
		}
		add("Cargo.toml", names, func(n string) string { return "cargo " + n })
	}
	if exists(filepath.Join(root, "go.mod")) {
		var cmds []string
		if exists(filepath.Join(root, "main.go")) {
			cmds = append(cmds, "go run .")
		}
		cmds = append(cmds, "go build ./...", "go test ./...", "go vet ./...")
		for _, c := range cmds {
			if len(out) < maxScripts {
				out = append(out, Script{Name: strings.Fields(c)[1], Command: c, From: "go.mod"})
			}
		}
	}
	return out
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// packageManager is how a project runs its package.json scripts, from its
// lockfile.
func packageManager(root string) string {
	switch {
	case exists(filepath.Join(root, "pnpm-lock.yaml")):
		return "pnpm run"
	case exists(filepath.Join(root, "yarn.lock")):
		return "yarn run"
	case exists(filepath.Join(root, "bun.lockb")), exists(filepath.Join(root, "bun.lock")):
		return "bun run"
	}
	return "npm run"
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=-]+$`)

// shellWord quotes a name for the shell when it needs it.
func shellWord(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// jsonKeys lists the keys of one object in a JSON file, in the file's order.
func jsonKeys(file, field string) []string {
	data, err := os.ReadFile(file)
	if err != nil || len(data) > 1<<20 {
		return nil
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil || len(top[field]) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(top[field]))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			break
		}
		if k, ok := t.(string); ok {
			keys = append(keys, k)
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			break
		}
	}
	return keys
}

var makeTarget = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_./-]*)\s*:([^=]|$)`)

// makeTargets lists a Makefile's targets, in order: not special ones
// (.PHONY), pattern rules or files the build writes (with a / or a dot in
// the name, like build/app or main.o).
func makeTargets(file string) []string {
	return scanNames(file, func(line string) string {
		m := makeTarget.FindStringSubmatch(line)
		if m == nil || strings.ContainsAny(m[1], "%/") || strings.Contains(m[1], ".") {
			return ""
		}
		return m[1]
	})
}

var justRecipe = regexp.MustCompile(`^@?([A-Za-z0-9_-]+)(\s[^:]*)?:([^=]|$)`)

// justRecipes lists a justfile's recipes, in order (not private ones, which
// start with _).
func justRecipes(file string) []string {
	return scanNames(file, func(line string) string {
		for _, kw := range []string{"set ", "alias ", "export ", "import ", "mod ", "[", "#"} {
			if strings.HasPrefix(line, kw) {
				return ""
			}
		}
		m := justRecipe.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(m[1], "_") {
			return ""
		}
		return m[1]
	})
}

// scanNames reads a file line by line (not indented ones) and keeps the
// names pick finds, once each.
func scanNames(file string, pick func(string) string) []string {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		if n := pick(line); n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}
