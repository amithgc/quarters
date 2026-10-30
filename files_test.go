package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFilesStayInsideProject(t *testing.T) {
	a, dir := testApp(t)
	outside := filepath.Join(filepath.Dir(dir), "secret.txt")
	os.WriteFile(outside, []byte("no\n"), 0o644)
	os.Symlink(outside, filepath.Join(dir, "link"))
	os.Symlink(filepath.Dir(dir), filepath.Join(dir, "up"))
	for _, bad := range []string{"../secret.txt", "/etc/hosts", "link", "up", "up/secret.txt", "a.txt/../../secret.txt"} {
		if _, err := a.ReadFile(dir, bad); err == nil {
			t.Errorf("read %s", bad)
		}
		if _, err := a.SaveFile(dir, bad, "x", ""); err == nil {
			t.Errorf("saved %s", bad)
		}
		if _, err := a.ListDir(dir, bad); err == nil && bad != "a.txt/../../secret.txt" {
			t.Errorf("listed %s", bad)
		}
	}
	if b, _ := os.ReadFile(outside); string(b) != "no\n" {
		t.Fatal("a file outside the project was written")
	}
	if _, err := a.ListDir("/etc", ""); err == nil {
		t.Error("listed a folder that is not a project")
	}
	// a name that only starts with two dots is inside
	os.WriteFile(filepath.Join(dir, "..notes"), []byte("hi\n"), 0o644)
	if f, err := a.ReadFile(dir, "..notes"); err != nil || f.Text != "hi\n" {
		t.Errorf("..notes: %+v, %v", f, err)
	}
}

func TestListDir(t *testing.T) {
	a, dir := testApp(t)
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n*.log\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "B.md"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "run.log"), nil, 0o644)
	v, err := a.ListDir(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	ignored := map[string]bool{}
	for _, e := range v.Entries {
		names = append(names, e.Name)
		ignored[e.Name] = e.Ignored
	}
	want := []string{"node_modules", "src", ".gitignore", "a.txt", "B.md", "run.log"} // folders first, no .git
	if !slices.Equal(names, want) {
		t.Errorf("entries %v, want %v", names, want)
	}
	if !ignored["node_modules"] || !ignored["run.log"] || ignored["a.txt"] {
		t.Errorf("ignored: %v", ignored)
	}
}

func TestReadAndSaveFile(t *testing.T) {
	a, dir := testApp(t)
	f, err := a.ReadFile(dir, "a.txt")
	if err != nil || f.Text != "one\n" {
		t.Fatalf("read: %+v, %v", f, err)
	}
	saved, err := a.SaveFile(dir, "a.txt", "two\n", f.Hash)
	if err != nil {
		t.Fatal(err)
	}
	// the agent edits it meanwhile: the old hash no longer saves
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("agent\n"), 0o644)
	if _, err := a.SaveFile(dir, "a.txt", "three\n", saved.Hash); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Errorf("saved over the agent's edit: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "agent\n" {
		t.Errorf("file is %q", b)
	}
	os.WriteFile(filepath.Join(dir, ".env"), []byte("TOKEN=hunter2\n"), 0o644)
	if f, err := a.ReadFile(dir, ".env"); err != nil || f.Text != "" || f.Note == "" {
		t.Errorf(".env: %+v, %v", f, err)
	}
	os.WriteFile(filepath.Join(dir, "bin"), []byte{0, 1, 2}, 0o644)
	if f, _ := a.ReadFile(dir, "bin"); f.Text != "" || f.Note == "" {
		t.Errorf("binary: %+v", f)
	}
	os.WriteFile(filepath.Join(dir, "p.png"), []byte("\x89PNG"), 0o644)
	if f, _ := a.ReadFile(dir, "p.png"); !strings.HasPrefix(f.Image, "data:image/png;base64,") {
		t.Errorf("picture: %+v", f)
	}
}

func TestFileStatusAndList(t *testing.T) {
	a, dir := testApp(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "new.go"), nil, 0o644)
	st, err := a.FileStatus(dir)
	if err != nil || st["a.txt"] != "M" || st["sub/new.go"] != "?" {
		t.Errorf("status %v, %v", st, err)
	}
	l, err := a.ListFiles(dir)
	if err != nil || !slices.Contains(l.Files, "a.txt") || !slices.Contains(l.Files, "sub/new.go") {
		t.Errorf("files %v, %v", l, err)
	}
	// a worktree is browsed like its project
	wt, err := a.CreateWorktree(dir, "side")
	if err != nil {
		t.Fatal(err)
	}
	if v, err := a.ListDir(wt, ""); err != nil || len(v.Entries) == 0 {
		t.Errorf("worktree: %+v, %v", v, err)
	}
}

func TestReadAndSaveConfig(t *testing.T) {
	a, _ := testApp(t)
	c, err := a.ReadConfig()
	if err != nil || !strings.Contains(c.Text, `"projects"`) {
		t.Fatalf("read: %+v, %v", c, err)
	}
	// a mistake is never saved: the error names its line
	if _, err := a.SaveConfig("{\n  \"projects\": [\n    {\"path\": 1}\n  ]\n}\n", c.Hash); err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Errorf("saved a mistake: %v", err)
	}
	// nor a change to remote access
	if _, err := a.SaveConfig(`{"projects": [], "remote": {"enabled": true, "lan": true}}`, c.Hash); err == nil || !strings.Contains(err.Error(), "remote access") {
		t.Errorf("changed remote access: %v", err)
	}
	saved, err := a.SaveConfig(`{"projects": [], "harness": "codex",}`, c.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if a.current().Harness != "codex" || len(a.current().Projects) != 0 {
		t.Errorf("not in use: %+v", a.current())
	}
	// saved since by Quarters itself: the old hash no longer saves
	if err := a.SetHarness("claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveConfig(`{"projects": []}`, saved.Hash); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Errorf("saved over a newer file: %v", err)
	}
}
