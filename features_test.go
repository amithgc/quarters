package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/session"
)

// testApp is an App with a config listing one project (a git repository).
func testApp(t *testing.T) (*App, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "first")
	dir, _ = filepath.EvalSymlinks(dir)
	cfg, err := config.Load([]config.Project{{Path: dir}})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: cfg, convs: map[int]string{}}
	a.f.init()
	a.sessions = session.NewManager(func(string, any) {}, nil)
	return a, dir
}

func TestFeaturesDefaultAndSwitch(t *testing.T) {
	a, _ := testApp(t)
	if !a.config().On("restore") || a.config().On("remote_ask") {
		t.Fatal("wrong defaults")
	}
	if err := a.SetFeature("restore", false); err != nil || a.config().On("restore") {
		t.Fatalf("switch off: %v", err)
	}
	if err := a.SetFeature("no_such", true); err == nil {
		t.Error("unknown feature accepted")
	}
	if err := a.SavePrompts([]config.Prompt{{Name: "", Text: "run the tests"}, {}}); err != nil {
		t.Fatal(err)
	}
	if p := a.config().Prompts; len(p) != 1 || p[0].Name != "run the tests" {
		t.Errorf("prompts = %+v", p)
	}
	if err := a.SetUsageAlertAt(5); err == nil {
		t.Error("5% accepted")
	}
}

func TestClaudeCommandYolo(t *testing.T) {
	c := &config.Config{}
	no := false
	if got := strings.Join(c.ClaudeCommand(config.Project{}, true), " "); got != "claude --dangerously-skip-permissions" {
		t.Error(got)
	}
	c.Command = []string{"claude", "--dangerously-skip-permissions", "--model", "opus"}
	if got := strings.Join(c.ClaudeCommand(config.Project{}, false), " "); got != "claude --model opus" {
		t.Error(got)
	}
	if c.YoloFor(config.Project{Yolo: &no}) {
		t.Error("project yolo=false ignored")
	}
}

func TestDiffStaysInsideProject(t *testing.T) {
	a, dir := testApp(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644)
	d, err := a.Diff(dir, "a.txt")
	if err != nil || !strings.Contains(d.Diff, "+two") {
		t.Fatalf("diff = %+v, %v", d, err)
	}
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello\n"), 0o644)
	if d, _ := a.Diff(dir, "new.txt"); !d.Added || d.Diff != "hello\n" {
		t.Errorf("new file: %+v", d)
	}
	for _, bad := range []string{"../x", "/etc/passwd", filepath.Join(dir, "..", "proj2", "a")} {
		if _, err := a.Diff(dir, bad); err == nil {
			t.Errorf("%s was readable", bad)
		}
	}
	os.Symlink("/etc/hosts", filepath.Join(dir, "link"))
	if _, err := a.Diff(dir, "link"); err == nil {
		t.Error("a symlink out of the project was readable")
	}
	if _, err := a.Diff("/etc", "hosts"); err == nil {
		t.Error("a folder that is not a project was readable")
	}
}

func TestWorktrees(t *testing.T) {
	a, dir := testApp(t)
	wt, err := a.CreateWorktree(dir, "fix-login")
	if err != nil {
		t.Fatal(err)
	}
	if list := a.Worktrees(dir); len(list) != 2 || list[1].Branch != "fix-login" || !a.isWorktree(dir, wt) {
		t.Fatalf("worktrees = %+v", list)
	}
	for _, bad := range []string{"../evil", "-rf", "a b", ""} {
		if _, err := a.CreateWorktree(dir, bad); err == nil {
			t.Errorf("branch %q accepted", bad)
		}
	}
	os.WriteFile(filepath.Join(wt, "x"), []byte("x"), 0o644)
	if err := a.RemoveWorktree(dir, wt, false); err == nil {
		t.Error("removed a worktree with changes without force")
	}
	if err := a.RemoveWorktree(dir, dir, true); err == nil {
		t.Error("removed the project itself")
	}
	if err := a.RemoveWorktree(dir, wt, true); err != nil {
		t.Fatal(err)
	}
}

func TestDevLinks(t *testing.T) {
	a, _ := testApp(t)
	a.watchOutput(1, []byte("\x1b[32m  ➜  Local:   http://localhost:\x1b[1m5173\x1b[22m/\x1b[39m\n"))
	a.watchOutput(1, []byte("listening on http://0.0.0.0:8080/api, see "))
	a.watchOutput(1, []byte("http://127.0.0.1:30"))
	a.watchOutput(1, []byte("00/docs\nand https://example.com:443/x\n"))
	got := a.f.links[1].urls
	want := []string{"http://localhost:5173", "http://localhost:8080/api", "http://localhost:3000/docs"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("links = %v", got)
	}
	if a.OpenOnMac("https://example.com") == nil || a.OpenOnMac("file:///etc/passwd") == nil {
		t.Error("OpenOnMac opened a link that is not to this Mac")
	}
}

func TestSchedule(t *testing.T) {
	if s, err := cleanSchedule(config.Schedule{At: "9:05", Days: []string{"fri", "mon", "xyz"}}); err != nil || s.At != "09:05" || strings.Join(s.Days, ",") != "mon,fri" {
		t.Errorf("schedule = %+v, %v", s, err)
	}
	if _, err := cleanSchedule(config.Schedule{At: "25:00"}); err == nil {
		t.Error("25:00 accepted")
	}
	a, dir := testApp(t)
	a.cfg.Projects[0].Automations = []config.Automation{{Name: "dev", Tabs: []config.AutomationTab{{Command: "echo hi"}}, Schedule: &config.Schedule{At: "09:00"}}}
	defer a.sessions.CloseAll()
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.Local)
	a.runSchedules(day.Add(8*time.Hour + 59*time.Minute))
	if len(a.sessions.List()) != 0 {
		t.Fatal("ran early")
	}
	a.runSchedules(day.Add(9*time.Hour + 2*time.Minute))
	a.runSchedules(day.Add(9*time.Hour + 3*time.Minute))
	list := a.sessions.List()
	if len(list) != 1 {
		t.Fatalf("%d sessions, want 1", len(list))
	}
	var m tabMeta
	json.Unmarshal(list[0].Meta, &m)
	if m.AutoKey != dir+"::dev" || m.Group == nil || m.Group.AutoName != "dev" {
		t.Errorf("meta = %+v", m)
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	a, dir := testApp(t)
	defer a.sessions.CloseAll()
	meta, _ := json.Marshal(map[string]any{"key": "k1", "path": dir, "kind": "claude"})
	id, err := a.sessions.Start(dir, []string{"/bin/sleep", "5"}, 80, 24, "", meta, "mac")
	if err != nil {
		t.Fatal(err)
	}
	a.convs[id] = "0b6f2c3e-1d2a-4b5c-8d9e-0f1a2b3c4d5e"
	a.saveRestore()
	b := &App{cfg: a.cfg, convs: map[int]string{}}
	b.f.init()
	b.loadRestore()
	items := b.RestoreInfo()
	if len(items) != 1 || items[0].Conv != a.convs[id] || items[0].Kind != "claude" {
		t.Fatalf("restore = %+v", items)
	}
}

func TestDiffHidesSecrets(t *testing.T) {
	a, dir := testApp(t)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("local.cfg\n"), 0o644)
	for _, f := range []string{".env", "local.cfg", "server.pem"} {
		os.WriteFile(filepath.Join(dir, f), []byte("TOKEN=hunter2\n"), 0o644)
		d, err := a.Diff(dir, f)
		if err != nil || strings.Contains(d.Diff, "hunter2") {
			t.Errorf("%s: shown (%+v, %v)", f, d, err)
		}
	}
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("TOKEN=\n"), 0o644)
	if d, _ := a.Diff(dir, ".env.example"); !d.Added {
		t.Error(".env.example (no secrets) was hidden")
	}
}

func TestDiffOutsideGit(t *testing.T) {
	a, dir := testApp(t)
	os.RemoveAll(filepath.Join(dir, ".git"))
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello\n"), 0o644)
	if d, err := a.Diff(dir, "notes.txt"); err != nil || d.Diff != "hello\n" {
		t.Errorf("a file in a folder without git: %+v, %v", d, err)
	}
}

// Settings change while background work reads them (run with -race).
func TestSettingsChangeWhileRead(t *testing.T) {
	a, _ := testApp(t)
	done := make(chan bool)
	go func() {
		for i := 0; i < 200; i++ {
			a.config().On("restore")
			_ = a.Projects()
		}
		done <- true
	}()
	for i := 0; i < 50; i++ {
		if err := a.SetFeature("restore", i%2 == 0); err != nil {
			t.Fatal(err)
		}
		if err := a.SetCollapsed([]string{fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	<-done
}

// Claude Code's own Remote Control: new Claude sessions get the flag, named
// after the project; shells don't.
func TestClaudeRemoteControl(t *testing.T) {
	a, dir := testApp(t)
	var mu sync.Mutex
	var out strings.Builder
	a.sessions = session.NewManager(func(_ string, data any) {
		if o, ok := data.(session.Output); ok {
			b, _ := base64.StdEncoding.DecodeString(o.Data)
			mu.Lock()
			out.Write(b)
			mu.Unlock()
		}
	}, nil)
	defer a.sessions.CloseAll()
	if err := a.editConfig(func(c *config.Config) error { c.Command = []string{"/bin/sh", "-c", `echo "ARGS: $*"; sleep 5`, "sh"} // stays up so its output is read; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.SetClaudeRemote(true); err != nil || !a.Settings().ClaudeRemote {
		t.Fatalf("SetClaudeRemote: %v", err)
	}
	if _, err := a.StartSession(dir, "claude", 120, 20, nil); err != nil {
		t.Fatal(err)
	}
	want := "--remote-control " + filepath.Base(dir)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		mu.Lock()
		got := out.String()
		mu.Unlock()
		if strings.Contains(got, want) {
			return
		}
	}
	mu.Lock()
	defer mu.Unlock()
	t.Errorf("Claude started without %q: %q", want, out.String())
}

// Claude Code may name its log differently from the id a tab was started
// with: the tab then follows the newest log written since it started that
// no other tab has.
func TestConversationOfFindsTheRealLog(t *testing.T) {
	a, dir := testApp(t)
	logs := projectDir(dir)
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Minute).UnixMilli()
	write := func(id string) {
		if err := os.WriteFile(filepath.Join(logs, id+".jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	other, real := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	write(other)
	time.Sleep(20 * time.Millisecond)
	write(real)
	a.convs[1] = "33333333-3333-4333-8333-333333333333" // no such log
	a.convs[2] = real                                   // another tab's
	m := tabMeta{Path: dir, Started: started}
	if got := a.conversationOf(1, m, nil); got != other {
		t.Errorf("conversationOf = %s, want %s (the newest log no other tab has)", got, other)
	}
	if a.convs[1] != other {
		t.Error("the log found was not remembered")
	}
	a.convs[3] = real
	if got := a.conversationOf(3, m, nil); got != real {
		t.Errorf("a tab whose log exists: %s", got)
	}
}
