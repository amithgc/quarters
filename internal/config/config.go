// Package config loads and saves the Quarters settings file
// (~/.config/quarters/config.json), which also lists the projects.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Project is one entry in the projects list.
type Project struct {
	// Name shown in the list. Default: the folder name.
	Name string `json:"name,omitempty"`
	// Path of the project folder. "~" means your home folder.
	Path string `json:"path"`
	// Group puts the project under a heading (e.g. "Work", "Games").
	Group string `json:"group,omitempty"`
	// Icon is an image file shown next to the project (png, jpg, svg, ico,
	// webp). "~" allowed; relative paths are inside the project folder.
	Icon string `json:"icon,omitempty"`
	// Command starts Claude Code for this project instead of the global
	// command, e.g. ["claude", "--model", "sonnet"].
	Command []string `json:"command,omitempty"`
	// Automations are run by hand from the app: each opens a set of terminal
	// tabs, each running a command in a folder.
	Automations []Automation `json:"automations,omitempty"`
	// Yolo skips Claude Code's permission prompts in this project (true),
	// or asks for them (false). Unset: the global "yolo" setting.
	Yolo *bool `json:"yolo,omitempty"`
}

// Automation is a named set of terminal tabs to open together.
type Automation struct {
	Name string `json:"name"`
	// Layout arranges the tabs: "grid" (default), "columns", "rows" — all
	// visible at once as panes of one tab — or "tabs" (one tab each).
	Layout string          `json:"layout,omitempty"`
	Tabs   []AutomationTab `json:"tabs"`
	// Auto runs the automation by itself the first time the project is
	// opened after Quarters starts.
	Auto bool `json:"auto,omitempty"`
	// Cols and Rows are the relative sizes of the panes' columns and rows,
	// saved when you drag the lines between panes. Empty: equal sizes.
	Cols []float64 `json:"cols,omitempty"`
	Rows []float64 `json:"rows,omitempty"`
	// Schedule runs the automation by itself at a time of day.
	Schedule *Schedule `json:"schedule,omitempty"`
}

// Schedule is when an automation runs by itself.
type Schedule struct {
	At   string   `json:"at"`             // "09:00", local time
	Days []string `json:"days,omitempty"` // "mon".."sun"; empty: every day
}

// AutomationTab is one terminal of an automation.
type AutomationTab struct {
	// Name is the tab's label. Default: the command.
	Name string `json:"name,omitempty"`
	// Dir is where the command runs: relative to the project folder, or
	// absolute ("~" allowed). Default: the project folder.
	Dir string `json:"dir,omitempty"`
	// Command is typed into a login shell there, so the tab stays a shell
	// when the command ends or you stop it. Empty: just a shell.
	Command string `json:"command,omitempty"`
}

// Layout holds the panel widths (in columns), saved when you drag a panel
// edge.
type Layout struct {
	LeftWidth  int `json:"left_width,omitempty"`
	RightWidth int `json:"right_width,omitempty"`
	// Collapsed lists the project groups folded shut in the list.
	Collapsed []string `json:"collapsed,omitempty"`
}

// Remote is browser access to Quarters. Devices sign in with a passkey;
// they are paired from the Mac app, never from the browser.
type Remote struct {
	Enabled bool `json:"enabled,omitempty"`
	// URL is the public address browsers use, e.g. https://quarters.example.com
	// (a Cloudflare Tunnel or Tailscale Funnel in front of Listen).
	URL string `json:"url,omitempty"`
	// Listen is the local address of the web server. Default 127.0.0.1:8421.
	// Anything other than loopback needs TLSCert and TLSKey.
	Listen string `json:"listen,omitempty"`
	// LAN also opens it at this Mac's address on the home network (plain
	// HTTP, password sign-in only), e.g. http://192.168.1.20:8421.
	LAN     bool   `json:"lan,omitempty"`
	TLSCert string `json:"tls_cert,omitempty"`
	TLSKey  string `json:"tls_key,omitempty"`
}

// Prompt is a saved prompt.
type Prompt struct {
	Name string `json:"name"`
	Text string `json:"text"`
	// Send presses Enter after typing it.
	Send bool `json:"send,omitempty"`
}

// Feature is an optional feature that can be switched on and off.
type Feature struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	About   string `json:"about"`
	Default bool   `json:"default"`
	// MacOnly settings can only be changed on the Mac (security, or
	// meaningless elsewhere).
	MacOnly bool `json:"macOnly"`
}

// Features lists the optional features, in the order Settings shows them.
var Features = []Feature{
	{"web_push", "Phone alerts when the app is closed", "A notification on your phone when Claude is waiting, even when Quarters isn't open there (turn it on in the phone's 🔔).", true, false},
	{"keep_awake", "Keep the Mac awake", "Stops the Mac from sleeping while Claude is working or a phone is connected.", true, false},
	{"prompts", "Saved prompts", "One-tap prompts (⚡ in the tab bar, or on the phone's key bar).", true, false},
	{"keybar", "Phone key bar", "Esc, tab, ⇧tab, ctrl and arrow keys above a phone's keyboard while you type in a terminal (phone keyboards have none of them).", true, false},
	{"restore", "Resume sessions after a restart", "When Quarters starts, offers to reopen the Claude conversations and automations that were open.", true, false},
	{"remote_ask", "Remote sessions ask permission", "Claude started from a browser always asks before running tools, even in yolo projects.", false, true},
	{"usage_alerts", "Usage-limit warnings", "A notification when a plan limit passes the warning level.", true, false},
	{"history", "Search past conversations", "🕘 on the start screen and in the tab bar: search and resume a project's earlier conversations.", true, false},
	{"worktrees", "Git worktrees", "⎇ in the tab bar: run Claude on a separate copy (worktree) of the project, so several can work at once.", true, false},
	{"git_status", "Git status in the project list", "Branch, changed files and ahead/behind next to each project.", true, false},
	{"changes", "What Claude changed", "The files Claude edited this session, in the stats panel; tap one for its diff.", true, false},
	{"schedules", "Scheduled automations", "Automations with a time run by themselves (set it in the project's automation).", true, false},
	{"dev_links", "Dev server links", "Links to servers your terminals start (localhost:3000…), with whether they're up.", true, false},
	{"skills", "Skills & MCP servers", "The project's Claude Code skills and MCP servers in the stats panel.", true, false},
	{"hotkey", "Global hotkey", "Brings Quarters to the front from any app.", true, true},
}

// On reports whether a feature is switched on.
func (c *Config) On(key string) bool {
	if v, ok := c.Features[key]; ok {
		return v
	}
	for _, f := range Features {
		if f.Key == key {
			return f.Default
		}
	}
	return false
}

// YoloFor reports whether Claude skips permission prompts in p.
func (c *Config) YoloFor(p Project) bool {
	if p.Yolo != nil {
		return *p.Yolo
	}
	return c.Yolo == nil || *c.Yolo
}

// AlertAt is the usage warning level in percent.
func (c *Config) AlertAt() int {
	if c.UsageAlertAt <= 0 || c.UsageAlertAt > 100 {
		return 80
	}
	return c.UsageAlertAt
}

// HotkeyCombo is the global hotkey ("" when off).
func (c *Config) HotkeyCombo() string {
	if !c.On("hotkey") {
		return ""
	}
	if c.Hotkey == "" {
		return "ctrl+alt+space"
	}
	return c.Hotkey
}

// DefaultListen is where the remote web server listens unless set.
const DefaultListen = "127.0.0.1:8421"

// Config is the user-editable settings file.
type Config struct {
	// Projects, in the order they are listed.
	Projects []Project `json:"projects"`
	// Command used to start Claude Code. Default: ["claude",
	// "--dangerously-skip-permissions"] (no permission prompts).
	Command []string `json:"command,omitempty"`
	// Shell for shell tabs. Default: $SHELL, then /bin/zsh.
	Shell string `json:"shell,omitempty"`
	// Context window size in tokens. 0 = read the model from Claude Code's
	// settings ("[1m]" models have 1M).
	ContextWindow int `json:"context_window,omitempty"`
	// Mouse enables clicking, scrolling and dragging panel edges.
	Mouse *bool `json:"mouse,omitempty"`
	// Layout is the panel widths.
	Layout Layout `json:"layout"`
	// Remote is access from a browser (phone, another computer).
	Remote Remote `json:"remote"`
	// Features switches optional features on or off (see Features below
	// for the names and what is on by default).
	Features map[string]bool `json:"features,omitempty"`
	// Yolo skips Claude Code's permission prompts (default true).
	Yolo *bool `json:"yolo,omitempty"`
	// Prompts are saved prompts, typed into Claude with one tap.
	Prompts []Prompt `json:"prompts,omitempty"`
	// UsageAlertAt warns when a plan limit passes this percentage (default 80).
	UsageAlertAt int `json:"usage_alert_at,omitempty"`
	// Hotkey brings Quarters to the front from any app, e.g. "ctrl+alt+space".
	Hotkey string `json:"hotkey,omitempty"`

	path    string
	modTime time.Time
}

// Path returns the settings file location.
func Path() string { return filepath.Join(configRoot(), "quarters", "config.json") }

// configRoot is $XDG_CONFIG_HOME, or ~/.config.
func configRoot() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

// MigrateOld copies the settings of the app's earlier name (~/.config/harness)
// into its folder the first time it starts, so projects and paired devices
// carry over. The old folder is left as it was. It does nothing once the new
// folder exists.
func MigrateOld() error {
	dir := filepath.Dir(Path())
	old := filepath.Join(configRoot(), "harness")
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	entries, err := os.ReadDir(old)
	if err != nil {
		return nil // nothing to carry over
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(old, e.Name()))
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// Load reads the settings file. When it does not exist it is created with
// seed as its projects. On an error the returned Config is still usable
// (empty), so the app can start and show what is wrong.
func Load(seed []Project) (*Config, error) {
	c := &Config{path: Path()}
	data, err := os.ReadFile(c.path)
	if st, statErr := os.Stat(c.path); statErr == nil {
		c.modTime = st.ModTime()
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		c.Projects = seed
		return c, c.Save()
	case err != nil:
		return c, err
	}
	if err := json.Unmarshal(stripTrailingCommas(data), c); err != nil {
		return &Config{path: c.path, modTime: c.modTime}, describe(data, err)
	}
	return c, nil
}

// stripTrailingCommas drops a comma that comes right before a closing ] or }
// (outside strings): an easy slip when editing the list by hand.
func stripTrailingCommas(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString, escaped := false, false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
		} else if ch == '"' {
			inString = true
		} else if ch == ',' {
			j := i + 1
			for j < len(data) && strings.ContainsRune(" \t\r\n", rune(data[j])) {
				j++
			}
			if j < len(data) && (data[j] == ']' || data[j] == '}') {
				continue
			}
		}
		out = append(out, ch)
	}
	return out
}

// describe adds the line number to a JSON error.
func describe(data []byte, err error) error {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	off := int64(-1)
	if errors.As(err, &syn) {
		off = syn.Offset
	} else if errors.As(err, &typ) {
		off = typ.Offset
	}
	if off < 0 || off > int64(len(data)) {
		return err
	}
	line := 1 + strings.Count(string(data[:off]), "\n")
	return fmt.Errorf("line %d: %v", line, err)
}

// Changed reports whether the file was edited since it was last read or
// written.
func (c *Config) Changed() bool {
	st, err := os.Stat(c.path)
	return err == nil && !st.ModTime().Equal(c.modTime)
}

// Touch marks the file as read as it is now, so an unchanged broken file is
// not re-read (and re-reported) on every refresh.
func (c *Config) Touch() {
	if st, err := os.Stat(c.path); err == nil {
		c.modTime = st.ModTime()
	}
}

// Clone returns a deep copy, to change without touching c (which other
// goroutines may be reading).
func (c *Config) Clone() *Config {
	n := &Config{path: c.path, modTime: c.modTime}
	if data, err := json.Marshal(c); err == nil {
		_ = json.Unmarshal(data, n)
	}
	return n
}

// File returns the settings file path.
func (c *Config) File() string { return c.path }

// ClaudeCommand is the command that starts Claude Code for p. yolo adds
// --dangerously-skip-permissions (and leaving it out removes it).
func (c *Config) ClaudeCommand(p Project, yolo bool) []string {
	base := []string{"claude"}
	if len(p.Command) > 0 {
		base = p.Command
	} else if len(c.Command) > 0 {
		base = c.Command
	}
	out := []string{}
	for _, a := range base {
		if a != "--dangerously-skip-permissions" {
			out = append(out, a)
		}
	}
	if yolo {
		out = append(out, "--dangerously-skip-permissions")
	}
	return out
}

// ShellPath is the shell for shell tabs.
func (c *Config) ShellPath() string {
	if c.Shell != "" {
		return c.Shell
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/zsh"
}

// MouseEnabled reports whether mouse support is on (default true).
func (c *Config) MouseEnabled() bool { return c.Mouse == nil || *c.Mouse }

// Save writes the settings file.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// write a copy, then swap it in: a crash mid-write never leaves half a file
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return err
	}
	if st, err := os.Stat(c.path); err == nil {
		c.modTime = st.ModTime()
	}
	return nil
}

// ExpandPath resolves "~" and makes a path absolute.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// SaveProject adds p, or replaces the project at oldPath when oldPath is set.
// A path already in the list (other than oldPath) is refused. Like the other
// changes below, it does not write the file: call Save.
func (c *Config) SaveProject(oldPath string, p Project) error {
	path := ExpandPath(p.Path)
	idx := -1
	for i, q := range c.Projects {
		qp := ExpandPath(q.Path)
		if oldPath != "" && qp == oldPath {
			idx = i
			continue
		}
		if qp == path {
			return fmt.Errorf("%s is already in the list", path)
		}
	}
	if oldPath != "" && idx < 0 {
		return fmt.Errorf("%s is no longer in the list", oldPath)
	}
	if idx >= 0 {
		// The dialog does not edit the command; keep it.
		p.Command = c.Projects[idx].Command
		c.Projects[idx] = p
	} else {
		c.Projects = append(c.Projects, p)
	}
	return nil
}

// RemoveProject drops a project from the list.
func (c *Config) RemoveProject(path string) error {
	for i, p := range c.Projects {
		if ExpandPath(p.Path) == path {
			c.Projects = append(c.Projects[:i], c.Projects[i+1:]...)
			return nil
		}
	}
	return nil
}

// MoveProject moves the project at path next to target (before it, or after
// it when after is set) and puts it in group. An empty target
// moves it to the end of group.
func (c *Config) MoveProject(path, target string, after bool, group string) error {
	from := -1
	for i, p := range c.Projects {
		if ExpandPath(p.Path) == path {
			from = i
		}
	}
	if from < 0 {
		return fmt.Errorf("%s is no longer in the list", path)
	}
	p := c.Projects[from]
	p.Group = group
	rest := append(append([]Project{}, c.Projects[:from]...), c.Projects[from+1:]...)
	at := len(rest)
	if target != "" {
		for i, q := range rest {
			if ExpandPath(q.Path) == target {
				at = i
				if after {
					at++
				}
			}
		}
	} else {
		for i, q := range rest {
			if q.Group == group {
				at = i + 1
			}
		}
	}
	c.Projects = append(rest[:at], append([]Project{p}, rest[at:]...)...)
	return nil
}

// SetAutomationSizes stores the pane sizes of a project's automation.
func (c *Config) SetAutomationSizes(path, name string, cols, rows []float64) error {
	for i, p := range c.Projects {
		if ExpandPath(p.Path) != path {
			continue
		}
		for j, a := range p.Automations {
			if a.Name == name {
				c.Projects[i].Automations[j].Cols, c.Projects[i].Automations[j].Rows = cols, rows
				return nil
			}
		}
	}
	return fmt.Errorf("automation %q not found", name)
}
