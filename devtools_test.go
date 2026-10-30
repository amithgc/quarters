package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s", args, out)
	}
	return strings.TrimSpace(string(out))
}

func TestFindScripts(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "package.json"), `{"scripts": {"dev": "vite", "postinstall": "x", "build": "vite build", "test:e2e": "playwright test"}}`)
	write(t, filepath.Join(dir, "pnpm-lock.yaml"), "")
	write(t, filepath.Join(dir, "Makefile"), ".PHONY: all\nall: build\nbuild/app: main.go\n\tgo build\nlint:\n\techo\nVAR := 1\n%.o: %.c\n")
	write(t, filepath.Join(dir, "justfile"), "set shell := [\"bash\"]\ndeploy target='prod':\n  echo\n_private:\n  echo\n")
	write(t, filepath.Join(dir, "go.mod"), "module x\n")
	write(t, filepath.Join(dir, "main.go"), "package main\n")
	var got []string
	for _, s := range findScripts(dir) {
		got = append(got, s.From+"|"+s.Name+"|"+s.Command)
	}
	want := []string{
		"package.json|dev|pnpm run dev", "package.json|build|pnpm run build", "package.json|test:e2e|pnpm run test:e2e",
		"Makefile|all|make all", "Makefile|lint|make lint",
		"justfile|deploy|just deploy",
		"go.mod|run|go run .", "go.mod|build|go build ./...", "go.mod|test|go test ./...", "go.mod|vet|go vet ./...",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scripts:\n got %q\nwant %q", got, want)
	}
}

func TestParseListeners(t *testing.T) {
	lsof := "p501\ncnode\nf23\nn*:5173\nf24\nn[::1]:5173\np88\ncControlCe\nn*:7000\np99\ncsshd\nn*:22\n"
	got := parseLsof(lsof)
	want := []listener{{5173, 501, "node"}, {7000, 88, "ControlCe"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lsof: %+v", got)
	}
	ss := "LISTEN 0 511 0.0.0.0:3000 0.0.0.0:* users:((\"node\",pid=4242,fd=19))\nLISTEN 0 128 0.0.0.0:22 0.0.0.0:*\n"
	if got := parseSS(ss); !reflect.DeepEqual(got, []listener{{3000, 4242, "node"}}) {
		t.Errorf("ss: %+v", got)
	}
}

func TestCommitRefusesSecrets(t *testing.T) {
	a, dir := testApp(t)
	gitOut(t, dir, "config", "user.email", "t@t")
	gitOut(t, dir, "config", "user.name", "t")
	if _, err := a.GitCommit(dir, "x", false); err == nil || !strings.Contains(err.Error(), "clean") {
		t.Errorf("clean tree: %v", err)
	}
	write(t, filepath.Join(dir, "a.txt"), "two\n")
	write(t, filepath.Join(dir, ".env"), "TOKEN=1\n")
	if _, err := a.GitCommit(dir, "  ", false); err == nil {
		t.Error("committed without a message")
	}
	if _, err := a.GitCommit(dir, "Change a", false); err == nil || !strings.Contains(err.Error(), ".env") {
		t.Errorf("committed a secret: %v", err)
	}
	os.Remove(filepath.Join(dir, ".env"))
	msg, err := a.GitCommit(dir, "Change a", false)
	if err != nil || !strings.HasPrefix(msg, "committed ") {
		t.Fatalf("commit: %q, %v", msg, err)
	}
	if got := gitOut(t, dir, "log", "-1", "--format=%s"); got != "Change a" {
		t.Errorf("last commit %q", got)
	}
}

func TestCheckpoints(t *testing.T) {
	a, dir := testApp(t)
	head := gitOut(t, dir, "rev-parse", "HEAD")
	write(t, filepath.Join(dir, "a.txt"), "staged\n")
	gitOut(t, dir, "add", "a.txt") // something staged: it must stay staged
	write(t, filepath.Join(dir, "a.txt"), "before\n")
	write(t, filepath.Join(dir, "notes.md"), "mine\n")
	write(t, filepath.Join(dir, ".env"), "TOKEN=1\n")
	id, err := checkpoint(dir, "Before a prompt")
	if err != nil || id == "" {
		t.Fatalf("checkpoint: %q, %v", id, err)
	}
	if again, err := checkpoint(dir, "same"); err != nil || again != "" {
		t.Errorf("an identical checkpoint was taken: %q, %v", again, err)
	}
	// the agent's turn
	write(t, filepath.Join(dir, "a.txt"), "after\n")
	os.Remove(filepath.Join(dir, "notes.md"))
	write(t, filepath.Join(dir, "new.go"), "package x\n")
	write(t, filepath.Join(dir, ".env"), "TOKEN=2\n")

	list, err := a.Checkpoints(dir)
	if err != nil || len(list) != 1 || list[0].Files != 3 || list[0].Label != "Before a prompt" {
		t.Fatalf("list: %+v, %v", list, err)
	}
	files, err := a.CheckpointFiles(dir, id)
	if err != nil || len(files) != 3 {
		t.Fatalf("files: %+v, %v", files, err)
	}
	if d, err := a.CheckpointDiff(dir, id, "a.txt"); err != nil || !strings.Contains(d.Diff, "-before") || !strings.Contains(d.Diff, "+after") {
		t.Errorf("diff: %+v, %v", d, err)
	}
	if err := a.RestoreCheckpoint(dir, id); err != nil {
		t.Fatal(err)
	}
	read := func(f string) string { b, _ := os.ReadFile(filepath.Join(dir, f)); return string(b) }
	if read("a.txt") != "before\n" || read("notes.md") != "mine\n" {
		t.Errorf("not restored: a=%q notes=%q", read("a.txt"), read("notes.md"))
	}
	if _, err := os.Stat(filepath.Join(dir, "new.go")); err == nil {
		t.Error("a file made after the checkpoint is still there")
	}
	if read(".env") != "TOKEN=2\n" {
		t.Errorf("a secret file was touched: %q", read(".env"))
	}
	if got := gitOut(t, dir, "rev-parse", "HEAD"); got != head {
		t.Error("HEAD moved")
	}
	if got := gitOut(t, dir, "show", ":a.txt"); got != "staged" {
		t.Errorf("the index changed: a.txt staged as %q", got)
	}
	if got := gitOut(t, dir, "stash", "list"); got != "" {
		t.Errorf("stash: %q", got)
	}
	// the restore saved how things were first, so it can be undone
	if list, _ := a.Checkpoints(dir); len(list) != 2 || !strings.HasPrefix(list[0].Label, "Before restoring") {
		t.Errorf("after restore: %+v", list)
	}
	if _, err := a.CheckpointFiles(dir, "../../etc"); err == nil {
		t.Error("a bad id was accepted")
	}
}

// A file staged, then edited to the same size in the same second: git's
// index can't tell from the file's size and time, so the snapshot must look.
func TestCheckpointSeesSameSizeEdit(t *testing.T) {
	_, dir := testApp(t)
	f := filepath.Join(dir, "a.txt")
	then := time.Now().Add(-10 * time.Second).Truncate(time.Second)
	write(t, f, "staged\n")
	os.Chtimes(f, then, then)
	gitOut(t, dir, "add", "a.txt")
	write(t, f, "before\n")
	os.Chtimes(f, then, then)
	os.Chtimes(filepath.Join(dir, ".git", "index"), then, then)
	if _, err := checkpoint(dir, "x"); err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, dir, "show", checkpointRefs+checkpointList(context.Background(), dir)[0].id+":a.txt"); got != "before" {
		t.Errorf("snapshotted a.txt as %q", got)
	}
}

func TestCheckpointOnEnter(t *testing.T) {
	a, dir := testApp(t)
	write(t, filepath.Join(dir, "a.txt"), "changed\n")
	id, err := a.sessions.Start(dir, []string{"/bin/sh", "-c", "sleep 5"}, 80, 24, "", []byte(`{"kind":"claude","path":"`+dir+`","title":"Claude"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.sessions.Close(id)
	a.beforeInput(id, "hello")                  // typing: no checkpoint
	a.beforeInput(id, "\x1b[200~a\rb\x1b[201~") // a paste: none either
	ctx := context.Background()
	if refs := checkpointList(ctx, dir); len(refs) != 0 {
		t.Fatalf("checkpoint before Enter: %v", refs)
	}
	a.beforeInput(id, "\r")
	for range 50 {
		if len(checkpointList(ctx, dir)) == 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("no checkpoint after Enter")
}

func TestGitGrepAndHit(t *testing.T) {
	_, dir := testApp(t)
	write(t, filepath.Join(dir, "src", "api.go"), "package api\n\nfunc OldAPI() {}\n")
	write(t, filepath.Join(dir, ".env"), "OLDAPI=secret\n")
	hits := gitGrep(context.Background(), dir, "oldapi", SearchOptions{}, 10)
	if len(hits) != 1 || hits[0].Path != "src/api.go" || hits[0].Line != 3 || hits[0].Match != "OldAPI" || hits[0].Pre != "func " {
		t.Errorf("hits: %+v", hits)
	}
	if hits := gitGrep(context.Background(), dir, "oldapi", SearchOptions{Case: true}, 10); len(hits) != 0 {
		t.Errorf("case: %+v", hits)
	}
}

func TestDayMarkdown(t *testing.T) {
	md := dayMarkdown(time.Date(2026, 10, 30, 18, 0, 0, 0, time.Local), []DayProject{
		{Name: "Shop", Commits: []string{"Add checkout"}, Chats: []string{"Fix the cart"}, Prompts: 3, Uncommitted: 2},
	})
	want := "## Friday 30 October\n\n**Shop**\n- Add checkout\n- Worked with the agent (3 prompts): Fix the cart\n- In progress: 2 files not committed yet\n"
	if md != want {
		t.Errorf("markdown:\n%s\nwant:\n%s", md, want)
	}
}

func TestRunRipgrep(t *testing.T) {
	root := t.TempDir()
	lines := []string{
		`{"type":"begin","data":{"path":{"text":"` + root + `/src/a.go"}}}`,
		`{"type":"match","data":{"path":{"text":"` + root + `/src/a.go"},"lines":{"text":"func OldAPI() {}\n"},"line_number":3,"submatches":[{"match":{"text":"OldAPI"},"start":5,"end":11}]}}`,
		`{"type":"match","data":{"path":{"text":"` + root + `/.env"},"lines":{"text":"OLDAPI=1\n"},"line_number":1,"submatches":[{"start":0,"end":6}]}}`,
		`{"type":"end","data":{}}`,
	}
	f := filepath.Join(t.TempDir(), "rg.json")
	write(t, f, strings.Join(lines, "\n")+"\n")
	r, err := runRipgrep(exec.Command("cat", f), []string{root})
	if err != nil || len(r.Hits) != 1 {
		t.Fatalf("hits: %+v, %v", r.Hits, err)
	}
	h := r.Hits[0]
	if h.Path != "src/a.go" || h.Line != 3 || h.Pre != "func " || h.Match != "OldAPI" || h.Post != "() {}" {
		t.Errorf("hit: %+v", h)
	}
}

func TestParsePR(t *testing.T) {
	out := `{"number":12,"title":"Add shipping","url":"https://github.com/x/y/pull/12","state":"OPEN","isDraft":false,"statusCheckRollup":[
		{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"SUCCESS"},
		{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"FAILURE"},
		{"__typename":"CheckRun","name":"e2e","status":"IN_PROGRESS","conclusion":""},
		{"__typename":"StatusContext","context":"ci/vercel","state":"SUCCESS"},
		{"__typename":"StatusContext","context":"ci/old","state":"PENDING"}]}`
	v := parsePR([]byte(out))
	if !v.Has || v.Number != 12 || v.Passed != 2 || v.Pending != 2 || !reflect.DeepEqual(v.Failed, []string{"lint"}) {
		t.Errorf("pr: %+v", v)
	}
}
