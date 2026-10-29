package config

import "slices"

// Harness is a coding agent that runs in a terminal tab: Claude Code,
// Codex, Gemini CLI and others. Claude Code is the default, and the only
// one whose conversations Quarters reads (history, stats, the phone's chat
// view); the others run as they are, in a terminal.
type Harness struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Bin is the program, looked up on the PATH.
	Bin string `json:"bin"`
	// Yolo skips the agent's permission prompts ("" when it has no switch).
	Yolo string `json:"yolo"`
	// Continue picks up the last conversation in the folder; it goes right
	// after the program, so it can be a subcommand ("resume --last").
	Continue []string `json:"continue"`
	// Site is where to get it.
	Site string `json:"site"`
}

// Claude is the default harness.
const Claude = "claude"

// Harnesses are the agents Quarters knows how to start.
var Harnesses = []Harness{
	{ID: Claude, Name: "Claude Code", Bin: "claude", Yolo: "--dangerously-skip-permissions", Continue: []string{"--continue"}, Site: "https://claude.com/claude-code"},
	{ID: "codex", Name: "Codex", Bin: "codex", Yolo: "--dangerously-bypass-approvals-and-sandbox", Continue: []string{"resume", "--last"}, Site: "https://github.com/openai/codex"},
	{ID: "gemini", Name: "Gemini CLI", Bin: "gemini", Yolo: "--yolo", Continue: []string{"--resume", "latest"}, Site: "https://github.com/google-gemini/gemini-cli"},
	{ID: "antigravity", Name: "Antigravity", Bin: "agy", Yolo: "--dangerously-skip-permissions", Continue: []string{"--continue"}, Site: "https://antigravity.google"},
	{ID: "opencode", Name: "OpenCode", Bin: "opencode", Continue: []string{"--continue"}, Site: "https://opencode.ai"},
	{ID: "copilot", Name: "GitHub Copilot CLI", Bin: "copilot", Yolo: "--allow-all-tools", Continue: []string{"--continue"}, Site: "https://github.com/github/copilot-cli"},
	{ID: "aider", Name: "Aider", Bin: "aider", Yolo: "--yes-always", Continue: []string{"--restore-chat-history"}, Site: "https://aider.chat"},
}

// HarnessByID finds a harness; an unknown id is Claude Code.
func HarnessByID(id string) Harness {
	if i := slices.IndexFunc(Harnesses, func(h Harness) bool { return h.ID == id }); i >= 0 {
		return Harnesses[i]
	}
	return Harnesses[0]
}

// KnownHarness reports whether id names a harness ("" is the default).
func KnownHarness(id string) bool {
	return id == "" || slices.ContainsFunc(Harnesses, func(h Harness) bool { return h.ID == id })
}

// HarnessFor is the agent that runs in p: its own choice, else the default.
func (c *Config) HarnessFor(p Project) Harness {
	if p.Harness != "" {
		return HarnessByID(p.Harness)
	}
	return HarnessByID(c.Harness)
}

// AgentCommand is the command that starts h for p. yolo adds the agent's
// switch for skipping permission prompts (and leaving it out removes it).
// A project's own command replaces its agent's; the global "command" is
// Claude Code's.
func (c *Config) AgentCommand(p Project, h Harness, yolo bool) []string {
	base := []string{h.Bin}
	if len(p.Command) > 0 && c.HarnessFor(p).ID == h.ID {
		base = p.Command
	} else if len(c.Command) > 0 && h.ID == Claude {
		base = c.Command
	}
	out := []string{}
	for _, a := range base {
		if h.Yolo == "" || a != h.Yolo {
			out = append(out, a)
		}
	}
	if yolo && h.Yolo != "" {
		out = append(out, h.Yolo)
	}
	return out
}

// ClaudeCommand is the command that starts Claude Code for p.
func (c *Config) ClaudeCommand(p Project, yolo bool) []string {
	return c.AgentCommand(p, HarnessByID(Claude), yolo)
}
