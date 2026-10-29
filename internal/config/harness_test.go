package config

import (
	"strings"
	"testing"
)

func TestHarnessFor(t *testing.T) {
	c := &Config{}
	if got := c.HarnessFor(Project{}).ID; got != Claude {
		t.Errorf("default harness = %s, want claude", got)
	}
	c.Harness = "codex"
	if got := c.HarnessFor(Project{}).ID; got != "codex" {
		t.Errorf("global harness = %s, want codex", got)
	}
	if got := c.HarnessFor(Project{Harness: "gemini"}).ID; got != "gemini" {
		t.Errorf("project harness = %s, want gemini", got)
	}
	if got := HarnessByID("nonsense").ID; got != Claude {
		t.Errorf("unknown harness = %s, want claude", got)
	}
	if KnownHarness("nonsense") || !KnownHarness("") || !KnownHarness("aider") {
		t.Error("KnownHarness is wrong")
	}
}

func TestAgentCommand(t *testing.T) {
	c := &Config{Command: []string{"claude", "--model", "opus"}}
	codex := HarnessByID("codex")
	cases := []struct {
		p    Project
		h    Harness
		yolo bool
		want string
	}{
		{Project{}, HarnessByID(Claude), true, "claude --model opus --dangerously-skip-permissions"},
		// the global command is Claude Code's, not another agent's
		{Project{}, codex, true, "codex --dangerously-bypass-approvals-and-sandbox"},
		{Project{}, codex, false, "codex"},
		// a project's command is its own agent's
		{Project{Harness: "codex", Command: []string{"codex", "-m", "o4", "--dangerously-bypass-approvals-and-sandbox"}}, codex, false, "codex -m o4"},
		// … and not used when a Claude conversation is resumed there
		{Project{Harness: "codex", Command: []string{"codex", "-m", "o4"}}, HarnessByID(Claude), false, "claude --model opus"},
		// no permission switch: yolo adds nothing
		{Project{}, HarnessByID("opencode"), true, "opencode"},
	}
	for _, tc := range cases {
		if got := strings.Join(c.AgentCommand(tc.p, tc.h, tc.yolo), " "); got != tc.want {
			t.Errorf("AgentCommand(%+v, %s, %v) = %q, want %q", tc.p, tc.h.ID, tc.yolo, got, tc.want)
		}
	}
}
