package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadChat(t *testing.T) {
	log := strings.Join([]string{
		`{"type":"user","timestamp":"2026-10-28T10:00:00Z","message":{"role":"user","content":"Fix the login 403"}}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"<command-name>/clear</command-name>"}}`,
		`{"type":"assistant","timestamp":"2026-10-28T10:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"Looking."},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"npm test\nmore"}}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/x/web/login.js","old_string":"a","new_string":"b"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"12 passed"},{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"ok"}],"is_error":true}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent noise"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Fixed."}]}}`,
		`not json`,
	}, "\n")
	path := filepath.Join(t.TempDir(), "c.jsonl")
	if err := os.WriteFile(path, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := ReadChat(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, it := range c.Items {
		kinds = append(kinds, it.Kind)
	}
	if strings.Join(kinds, ",") != "user,text,tools,text" {
		t.Fatalf("items = %v", kinds)
	}
	tl := c.Items[2].Tools
	if len(tl) != 2 || tl[0].Detail != "npm test …" || tl[0].Result != "12 passed" || tl[1].Detail != "web/login.js" || tl[1].Old != "a" || tl[1].New != "b" || !tl[1].Error || tl[1].Result != "ok" {
		t.Errorf("tools = %+v", tl)
	}
	if v, _ := ChatVersion(path); v != c.Version || v == "" {
		t.Errorf("version %q vs %q", v, c.Version)
	}
	if c2, _ := ReadChat(path, 2); len(c2.Items) != 2 || c2.Items[1].Text != "Fixed." {
		t.Errorf("max items: %+v", c2.Items)
	}
}
