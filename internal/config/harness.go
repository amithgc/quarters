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
	// Yolo skips the agent's permission prompts (none when it has no
	// switch); it can be more than one word ("--permission-mode bypass").
	Yolo []string `json:"yolo"`
	// Continue picks up the last conversation in the folder; it goes right
	// after the program, so it can be a subcommand ("resume --last").
	Continue []string `json:"continue"`
	// Site is where to get it.
	Site string `json:"site"`

	// How the phone's message card talks to it (see frontend/dist/app.js,
	// layoutFor). Unknown agents get the defaults, which are safe for any
	// terminal program: the key bar's arrows and ↵ always work.

	// Interrupt is what ■ sends to stop the agent while it works. Default
	// Esc: it interrupts the agents that use it and does nothing harmful
	// in the others (^C quits several agents when pressed twice).
	Interrupt string `json:"interrupt,omitempty"`
	// MenuMarker is the mark the agent puts on a menu's chosen option
	// ("❯"), so a tap on an option chooses it. "" = no tapping, arrows.
	MenuMarker string `json:"menuMarker,omitempty"`
	// Reader names the conversation reader that turns the agent's own log
	// into the phone's chat view ("" = the terminal only).
	Reader string `json:"reader,omitempty"`
	// Instructions is the file in a project's folder the agent reads its
	// standing instructions from (the stats panel edits it).
	Instructions string `json:"instructions"`
}

// Claude is the default harness.
const Claude = "claude"

// Harnesses are the agents Quarters knows how to start.
var Harnesses = []Harness{
	{ID: Claude, Name: "Claude Code", Bin: "claude", Yolo: []string{"--dangerously-skip-permissions"}, Continue: []string{"--continue"}, Site: "https://claude.com/claude-code",
		Interrupt: "\x1b", MenuMarker: "❯", Reader: Claude, Instructions: "CLAUDE.md"},
	{ID: "codex", Name: "Codex", Bin: "codex", Yolo: []string{"--dangerously-bypass-approvals-and-sandbox"}, Continue: []string{"resume", "--last"}, Site: "https://github.com/openai/codex", Instructions: "AGENTS.md"},
	{ID: "gemini", Name: "Gemini CLI", Bin: "gemini", Yolo: []string{"--yolo"}, Continue: []string{"--resume", "latest"}, Site: "https://github.com/google-gemini/gemini-cli", Instructions: "GEMINI.md"},
	{ID: "antigravity", Name: "Antigravity", Bin: "agy", Yolo: []string{"--dangerously-skip-permissions"}, Continue: []string{"--continue"}, Site: "https://antigravity.google", Instructions: "AGENTS.md"},
	{ID: "opencode", Name: "OpenCode", Bin: "opencode", Continue: []string{"--continue"}, Site: "https://opencode.ai", Instructions: "AGENTS.md"},
	{ID: "copilot", Name: "GitHub Copilot CLI", Bin: "copilot", Yolo: []string{"--allow-all-tools"}, Continue: []string{"--continue"}, Site: "https://github.com/github/copilot-cli", Instructions: "AGENTS.md"},
	{ID: "devin", Name: "Devin", Bin: "devin", Yolo: []string{"--permission-mode", "bypass"}, Continue: []string{"--continue"}, Site: "https://docs.devin.ai/cli", Instructions: "AGENTS.md"},
	{ID: "aider", Name: "Aider", Bin: "aider", Yolo: []string{"--yes-always"}, Continue: []string{"--restore-chat-history"}, Site: "https://aider.chat",
		Interrupt: "\x03", Instructions: "CONVENTIONS.md"}, // Aider stops a reply with ^C, and ignores Esc
	{ID: "amp", Name: "Amp", Bin: "amp", Yolo: []string{"--dangerously-allow-all"}, Continue: []string{"threads", "continue", "--last"}, Site: "https://ampcode.com", Instructions: "AGENTS.md"},
	// Goose has no yolo switch (GOOSE_MODE=auto in its config does it), and
	// stops a reply with ^C
	{ID: "goose", Name: "Goose", Bin: "goose", Continue: []string{"session", "--resume"}, Site: "https://github.com/block/goose",
		Interrupt: "\x03", Instructions: ".goosehints"},
	{ID: "qwen", Name: "Qwen Code", Bin: "qwen", Yolo: []string{"--yolo"}, Continue: []string{"--continue"}, Site: "https://github.com/QwenLM/qwen-code", Instructions: "QWEN.md"},
	{ID: "cursor", Name: "Cursor CLI", Bin: "agent", Yolo: []string{"--force"}, Continue: []string{"--continue"}, Site: "https://cursor.com/cli", Instructions: "AGENTS.md"},
}

// defaultInterrupt is Esc (see Harness.Interrupt).
const defaultInterrupt = "\x1b"

func init() {
	for i := range Harnesses {
		if Harnesses[i].Interrupt == "" {
			Harnesses[i].Interrupt = defaultInterrupt
		}
	}
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
	for i := 0; i < len(base); i++ {
		if n := len(h.Yolo); n > 0 && i+n <= len(base) && slices.Equal(base[i:i+n], h.Yolo) {
			i += n - 1 // the switch is added back below when wanted
			continue
		}
		out = append(out, base[i])
	}
	if yolo {
		out = append(out, h.Yolo...)
	}
	return out
}

// ClaudeCommand is the command that starts Claude Code for p.
func (c *Config) ClaudeCommand(p Project, yolo bool) []string {
	return c.AgentCommand(p, HarnessByID(Claude), yolo)
}
