package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistorySearch(t *testing.T) {
	dir := t.TempDir()
	write := func(id, body string) { os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(body), 0o644) }
	write("aaaa", `{"type":"user","timestamp":"2026-10-01T10:00:00Z","message":{"role":"user","content":"Fix the Login button"}}
{"type":"assistant","timestamp":"2026-10-01T10:01:00Z","message":{"content":[{"type":"text","text":"The CSRF token was missing."}]}}
{"type":"ai-title","aiTitle":"Login fix"}
`)
	write("bbbb", `{"type":"user","timestamp":"2026-10-02T10:00:00Z","message":{"role":"user","content":[{"type":"text","text":"add dark mode"}]}}
`)
	write("cccc", `{"type":"user","timestamp":"2026-10-03T10:00:00Z","message":{"role":"user","content":[{"type":"tool_result","content":"x"}]}}
`)
	h := NewHistory()
	all := h.Search(dir, "", 0)
	if len(all) != 2 || all[0].ID != "bbbb" || all[1].Title != "Login fix" || all[1].First != "Fix the Login button" {
		t.Fatalf("all = %+v", all)
	}
	hits := h.Search(dir, "csrf", 0)
	if len(hits) != 1 || hits[0].ID != "aaaa" || !strings.Contains(strings.ToLower(hits[0].Match), "csrf") {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSkillsAndMCP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := t.TempDir()
	os.MkdirAll(filepath.Join(proj, ".claude", "skills", "deploy"), 0o755)
	os.WriteFile(filepath.Join(proj, ".claude", "skills", "deploy", "SKILL.md"), []byte("---\nname: deploy\ndescription: \"Ship it to prod\"\n---\nbody"), 0o644)
	os.MkdirAll(filepath.Join(home, ".claude", "skills", "notes"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "skills", "notes", "SKILL.md"), []byte("no front matter"), 0o644)
	os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers":{"gh":{"command":"/usr/bin/npx","args":["-y","@github/mcp","--token","ghp_SECRET"],"env":{"K":"v"}}},
		"projects":{"`+proj+`":{"mcpServers":{"api":{"type":"http","url":"https://api.example.com/mcp?key=SECRET","headers":{"Authorization":"x"}}}}}}`), 0o644)
	sk := Skills(proj)
	if len(sk) != 2 || sk[0].Name != "deploy" || sk[0].About != "Ship it to prod" || sk[1].Name != "notes" || sk[1].Scope != "user" {
		t.Fatalf("skills = %+v", sk)
	}
	m := MCPServers(proj)
	if len(m) != 2 || m[0].Name != "api" || m[0].Target != "api.example.com/mcp" || m[1].Target != "npx @github/mcp" {
		t.Fatalf("mcp = %+v", m)
	}
	for _, s := range m {
		if strings.Contains(s.Target, "SECRET") {
			t.Errorf("secret shown: %s", s.Target)
		}
	}
}
