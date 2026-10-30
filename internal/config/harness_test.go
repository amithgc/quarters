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
		// a switch of two words, added once and taken out of a project's command
		{Project{}, HarnessByID("devin"), true, "devin --permission-mode bypass"},
		{Project{Harness: "devin", Command: []string{"devin", "--permission-mode", "bypass", "--model", "x"}}, HarnessByID("devin"), false, "devin --model x"},
		// Cursor CLI's program is "agent"; Goose has no switch
		{Project{}, HarnessByID("cursor"), true, "agent --force"},
		{Project{}, HarnessByID("goose"), true, "goose"},
	}
	for _, tc := range cases {
		if got := strings.Join(c.AgentCommand(tc.p, tc.h, tc.yolo), " "); got != tc.want {
			t.Errorf("AgentCommand(%+v, %s, %v) = %q, want %q", tc.p, tc.h.ID, tc.yolo, got, tc.want)
		}
	}
}

func TestHarnessAdapters(t *testing.T) {
	for _, h := range Harnesses {
		if h.Interrupt == "" {
			t.Errorf("%s has no interrupt key", h.ID)
		}
		if h.Reader != "" && h.Reader != Claude {
			t.Errorf("%s names an unknown reader %q", h.ID, h.Reader)
		}
	}
	if c := HarnessByID(Claude); c.Interrupt != "\x1b" || c.MenuMarker != "❯" || c.Reader != Claude {
		t.Errorf("claude adapter = %+v", c)
	}
	if a := HarnessByID("aider"); a.Interrupt != "\x03" {
		t.Errorf("aider interrupt = %q", a.Interrupt)
	}
	if g := HarnessByID("goose"); g.Interrupt != "\x03" || len(g.Yolo) != 0 {
		t.Errorf("goose stops with ^C and has no yolo switch: %+v", g)
	}
	if o := HarnessByID("opencode"); o.Interrupt != "\x1b" || o.MenuMarker != "" || o.Reader != "" {
		t.Errorf("opencode should have the defaults: %+v", o)
	}
}
