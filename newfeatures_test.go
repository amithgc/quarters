package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/projects"
	"github.com/amithgc/quarters/internal/stats"
)

// The editor opens only the agents' instruction files, and won't write
// over a version it hasn't seen.
func TestInstructionsEditor(t *testing.T) {
	a, dir := testApp(t)
	v, err := a.Instructions(dir)
	if err != nil || len(v.Files) != 1 || v.Files[0].Name != "CLAUDE.md" || v.Files[0].Exists {
		t.Fatalf("a new project should offer its agent's file to create: %+v %v", v, err)
	}
	for _, bad := range []string{"a.txt", "../CLAUDE.md", ".git/config", "/etc/hosts"} {
		if _, err := a.ReadInstructions(dir, bad); err == nil {
			t.Errorf("opened %s", bad)
		}
		if _, err := a.SaveInstructions(dir, bad, "x", ""); err == nil {
			t.Errorf("wrote %s", bad)
		}
	}
	if _, err := a.ReadInstructions(t.TempDir(), "CLAUDE.md"); err == nil {
		t.Error("opened a file outside the projects")
	}
	t0, _ := a.ReadInstructions(dir, "CLAUDE.md")
	t1, err := a.SaveInstructions(dir, "CLAUDE.md", "# Rules\nbe brief", t0.Hash)
	if err != nil || t1.Text != "# Rules\nbe brief\n" {
		t.Fatalf("save: %+v %v", t1, err)
	}
	if _, err := a.SaveInstructions(dir, "CLAUDE.md", "older", t0.Hash); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Errorf("wrote over a newer version: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("shared\n"), 0o644)
	os.Remove(filepath.Join(dir, "CLAUDE.md"))
	os.Symlink("AGENTS.md", filepath.Join(dir, "CLAUDE.md"))
	t2, _ := a.ReadInstructions(dir, "CLAUDE.md")
	if _, err := a.SaveInstructions(dir, "CLAUDE.md", "both", t2.Hash); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(filepath.Join(dir, "CLAUDE.md")); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the link was replaced by a file")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md")); string(b) != "both\n" {
		t.Errorf("AGENTS.md = %q", b)
	}
	if v, _ := a.Instructions(dir); len(v.Files) != 2 || v.Files[0].Lines != 1 {
		t.Errorf("files = %+v", v.Files)
	}
	n0, _ := a.Notes(dir)
	if _, err := a.SaveNotes(dir, "deploy: make ship\nmore", n0.Hash); err != nil {
		t.Fatal(err)
	}
	if v, _ := a.Instructions(dir); v.Notes != "deploy: make ship" {
		t.Errorf("notes = %q", v.Notes)
	}
	if _, err := os.Stat(filepath.Join(dir, ".quarters")); err == nil {
		t.Error("notes belong with Quarters' settings, not in the project")
	}
}

// Queued prompts go to running agents only, one at a time, once the agent
// is quiet and only after it worked on the previous one.
func TestPromptQueue(t *testing.T) {
	a, dir := testApp(t)
	agent := func(script string) int {
		meta, _ := json.Marshal(map[string]any{"kind": "claude", "path": dir})
		id, err := a.sessions.Start(dir, []string{"/bin/sh", "-c", script}, 80, 24, "", meta, "mac")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	defer a.sessions.CloseAll()
	// answers each prompt after a moment, like an agent at work
	id := agent(`echo ready; while read l; do sleep 0.5; echo "done $l"; done`)
	shellMeta, _ := json.Marshal(map[string]any{"kind": "shell", "path": dir})
	sh, _ := a.sessions.Start(dir, []string{"/bin/cat"}, 80, 24, "", shellMeta, "mac")
	if err := a.QueueAdd(sh, "ls"); err == nil {
		t.Error("queued a prompt for a shell")
	}
	if err := a.QueueAdd(id, "  "); err == nil {
		t.Error("queued an empty prompt")
	}
	for _, p := range []string{"one", "two", "three"} {
		if err := a.QueueAdd(id, p); err != nil {
			t.Fatal(err)
		}
	}
	a.QueueMove(id, 2, -1)
	a.QueueRemove(id, 0)
	if q := a.Queues(); len(q) != 1 || strings.Join(q[0].Items, ",") != "three,two" {
		t.Fatalf("queue = %+v", q)
	}
	time.Sleep(200 * time.Millisecond)
	if sent, _ := a.QueueNext(id); sent {
		t.Fatal("sent while the agent was still starting")
	}
	time.Sleep(queueQuiet + 200*time.Millisecond)
	if sent, err := a.QueueNext(id); !sent || err != nil {
		t.Fatalf("not sent when quiet: %v", err)
	}
	if sent, _ := a.QueueNext(id); sent {
		t.Error("sent twice in a row")
	}
	time.Sleep(queueQuiet + 800*time.Millisecond)
	if sent, _ := a.QueueNext(id); !sent {
		t.Error("the next one wasn't sent after the agent answered")
	}
	if q := a.Queues(); len(q) != 0 {
		t.Errorf("queue = %+v", q)
	}

	// an agent that never answers gets one prompt, not the next
	mute := agent(`echo ready; cat >/dev/null`)
	a.QueueAdd(mute, "a")
	a.QueueAdd(mute, "b")
	time.Sleep(queueQuiet + 200*time.Millisecond)
	if sent, _ := a.QueueNext(mute); !sent {
		t.Fatal("first prompt not sent")
	}
	time.Sleep(queueQuiet + 200*time.Millisecond)
	if sent, _ := a.QueueNext(mute); sent {
		t.Error("sent again though the agent didn't work on the last one")
	}
	a.sessions.Close(mute)
	if q := a.Queues(); len(q) != 0 {
		t.Errorf("an ended session kept its queue: %+v", q)
	}
}

// Usage adds up Claude Code's logs per project (worktrees included), each
// response once.
func TestUsageByProject(t *testing.T) {
	a, dir := testApp(t)
	root := projects.ClaudeRoot()
	now := time.Now()
	ts := now.UTC().Format(time.RFC3339Nano)
	old := now.AddDate(0, 0, -3).UTC().Format(time.RFC3339Nano)
	line := func(id, at string, in, out int) string {
		b, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": at, "message": map[string]any{
			"id": id, "usage": map[string]int{"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": 999, "cache_creation_input_tokens": 10}}})
		return string(b) + "\n"
	}
	write := func(path, text string) {
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(text), 0o644)
	}
	enc := projects.EncodeDir(dir)
	// one response logged twice (its blocks), another the same day, one 3 days ago
	write(filepath.Join(root, enc, "s1.jsonl"), line("m1", ts, 100, 5)+line("m1", ts, 100, 50)+line("m2", ts, 10, 0)+line("m0", old, 1000, 0))
	write(filepath.Join(root, enc, "s1", "subagents", "agent-a.jsonl"), line("m3", ts, 5, 5))
	write(filepath.Join(root, enc+"-worktrees-fix", "w.jsonl"), line("m4", ts, 40, 0))
	write(filepath.Join(root, "-somewhere-else", "x.jsonl"), line("m5", ts, 7, 0))
	v := a.Usage()
	if len(v.Projects) != 1 || len(v.Days) != stats.UsageDays {
		t.Fatalf("usage = %+v", v)
	}
	p := v.Projects[0]
	// today: (100+50+10) + (10+0+10) + (5+5+10) + (40+0+10)
	if p.Today != 160+20+20+50 || p.Week != p.Today+1010 || p.Month != p.Week {
		t.Errorf("project = today %d week %d month %d", p.Today, p.Week, p.Month)
	}
	if v.Other.Today != 17 || v.Total.Week != p.Week+17 {
		t.Errorf("other = %+v total = %+v", v.Other, v.Total)
	}
	_ = config.Claude
}
