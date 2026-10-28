package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionCountsEachResponseOnce(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	log := strings.Join([]string{
		`{"type":"permission-mode","permissionMode":"acceptEdits"}`,
		`{"type":"ai-title","aiTitle":"Fix the build"}`,
		`{"type":"user","timestamp":"` + now + `","message":{"role":"user","content":"hello"}}`,
		// One response logged as two lines repeating its usage.
		`{"type":"assistant","timestamp":"` + now + `","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}`,
		`{"type":"assistant","timestamp":"` + now + `","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"tool_use","name":"Bash"}],"usage":{"input_tokens":10,"output_tokens":7,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}`,
		`{"type":"user","timestamp":"` + now + `","message":{"role":"user","content":[{"type":"tool_result"}]}}`,
		`{"type":"assistant","timestamp":"` + now + `","message":{"id":"m2","model":"claude-opus-5-5","content":[{"type":"tool_use","name":"mcp__playwright__browser_click"}],"usage":{"input_tokens":1,"output_tokens":3,"cache_read_input_tokens":200,"cache_creation_input_tokens":0}}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	s, ok := NewReader().Session(dir)
	if !ok {
		t.Fatal("no session")
	}
	want := Usage{Input: 11, Output: 10, CacheRead: 300, CacheWrite: 20}
	if s.Usage != want {
		t.Errorf("usage = %+v, want %+v", s.Usage, want)
	}
	if s.Turns != 1 || s.Title != "Fix the build" || s.Mode != "acceptEdits" || s.Model != "claude-opus-5-5" {
		t.Errorf("got turns=%d title=%q mode=%q model=%q", s.Turns, s.Title, s.Mode, s.Model)
	}
	if s.Context != 201 {
		t.Errorf("context = %d, want 201", s.Context)
	}
	if len(s.Tools) != 1 || s.Tools[0] != (ToolCount{"Bash", 1}) || s.MCPUsed["playwright"] != 1 {
		t.Errorf("tools = %+v, mcp = %+v", s.Tools, s.MCPUsed)
	}
	if s.Activity[Buckets-1] != 11+10+20 {
		t.Errorf("latest activity = %d, want 41", s.Activity[Buckets-1])
	}
}

func TestParseBranch(t *testing.T) {
	b, a, be := parseBranch("main...origin/main [ahead 2, behind 1]")
	if b != "main" || a != 2 || be != 1 {
		t.Errorf("got %q %d %d", b, a, be)
	}
	if b, _, _ := parseBranch("No commits yet on dev"); b != "dev" {
		t.Errorf("got %q", b)
	}
}

func TestFindImage(t *testing.T) {
	dir := t.TempDir()
	lines := `{"type":"user","message":{"content":"hi"}}
{"type":"user","imagePasteIds":[1,2],"message":{"content":[{"type":"text","text":"[Image #1] [Image #2]"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAA"}},{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"BBB"}}]}}
`
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := FindImage(dir, 2, time.Time{}); got != "data:image/jpeg;base64,BBB" {
		t.Fatalf("image 2 = %q", got)
	}
	if got, _ := FindImage(dir, 1, time.Time{}); got != "data:image/png;base64,AAA" {
		t.Fatalf("image 1 = %q", got)
	}
	if _, err := FindImage(dir, 3, time.Time{}); err == nil {
		t.Fatal("image 3 should be missing")
	}
}
