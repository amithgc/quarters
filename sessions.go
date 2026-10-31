package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/session"
)

// ---- sessions ----

// StartSession starts the project's coding agent ("claude"; Claude Code
// unless the project chose another harness), the agent continuing the last
// conversation ("continue"), a past Claude Code conversation ("resume") or
// a shell ("shell") in a project. meta is what other windows need to show
// the tab (title, automation, …).
func (a *App) StartSession(path, kind string, cols, rows int, meta map[string]any) (int, error) {
	return a.startSession(macClient, path, kind, cols, rows, meta)
}

func (a *App) startSession(client, path, kind string, cols, rows int, meta map[string]any) (int, error) {
	cfg := a.config()
	if meta == nil {
		meta = map[string]any{}
	}
	cp, ok := a.project(path)
	if !ok {
		return 0, fmt.Errorf("%s is not one of your projects", path)
	}
	dir := path
	if cwd, _ := meta["cwd"].(string); cwd != "" && cwd != path {
		// a worktree of the project
		if !a.isWorktree(path, cwd) {
			return 0, fmt.Errorf("%s is not a worktree of %s", cwd, path)
		}
		dir = cwd
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return 0, fmt.Errorf("folder not found: %s", dir)
	}
	var argv []string
	conv := ""
	switch kind {
	case "shell":
		argv = []string{cfg.ShellPath(), "-l"}
	default:
		yolo := cfg.YoloFor(cp) && !(client != macClient && cfg.On("remote_ask"))
		h := cfg.HarnessFor(cp)
		if kind == "resume" {
			h = config.HarnessByID(config.Claude) // past conversations are Claude Code's
		}
		meta["harness"] = h.ID
		argv = append([]string{}, cfg.AgentCommand(cp, h, yolo)...)
		if bin := argv[0]; !strings.Contains(bin, "/") && !a.sessions.Installed(bin) {
			return 0, fmt.Errorf("%s isn't installed here (%s is not on your PATH) · get it at %s", h.Name, bin, h.Site)
		}
		if h.ID != config.Claude {
			if kind == "continue" {
				argv = slices.Insert(argv, 1, h.Continue...)
			}
			break
		}
		if cfg.ClaudeRemoteControl {
			name := cp.Name
			if name == "" {
				name = filepath.Base(path)
			}
			argv = append(argv, "--remote-control", name)
		}
		argv = inProcessTeams(argv)
		switch kind {
		case "continue":
			argv = append(argv, "--continue")
		case "resume":
			conv, _ = meta["resume"].(string)
			if !uuidRe.MatchString(conv) {
				return 0, fmt.Errorf("no such conversation")
			}
			argv = append(argv, "--resume", conv)
		default:
			// a known conversation id, so the tab can be resumed after a restart
			conv = newUUID()
			argv = append(argv, "--session-id", conv)
		}
	}
	m, err := metaJSON(meta)
	if err != nil {
		return 0, err
	}
	id, err := a.sessions.Start(dir, argv, cols, rows, "", m, client)
	if err == nil && conv != "" {
		a.fmu.Lock()
		a.convs[id] = conv
		a.fmu.Unlock()
	}
	return id, err
}

// StartCommandSession opens a login shell in dir and types command into it
// (one tab of an automation).
func (a *App) StartCommandSession(dir, command string, cols, rows int, meta map[string]any) (int, error) {
	return a.startCommandSession(macClient, dir, command, cols, rows, meta)
}

func (a *App) startCommandSession(client, dir, command string, cols, rows int, meta map[string]any) (int, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return 0, fmt.Errorf("folder not found: %s", dir)
	}
	m, err := metaJSON(meta)
	if err != nil {
		return 0, err
	}
	return a.sessions.Start(dir, []string{a.config().ShellPath(), "-l"}, cols, rows, command, m, client)
}

// metaJSON encodes a tab's description for the other windows.
func metaJSON(meta map[string]any) (json.RawMessage, error) {
	m, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	if len(m) > 8<<10 {
		return nil, fmt.Errorf("tab description too long")
	}
	return m, nil
}

// Sessions lists every running session, for a window joining late.
func (a *App) Sessions() []session.Info { return a.sessions.List() }

// Snapshot is a session's recent output.
func (a *App) Snapshot(id int) (session.Snap, error) { return a.sessions.Snapshot(id) }

// Write sends typed text to a session.
func (a *App) Write(id int, data string) {
	a.beforeInput(id, data)
	a.sessions.Write(macClient, id, data)
}

// Resize changes a session's terminal size (if the Mac window drives it).
func (a *App) Resize(id, cols, rows int) { a.sessions.Resize(macClient, id, cols, rows) }

// Claim gives a session the Mac window's terminal size.
func (a *App) Claim(id, cols, rows int) { a.sessions.Claim(macClient, id, cols, rows) }

// ClientID is this window's name among the windows sharing sessions.
func (a *App) ClientID() string { return macClient }

// SetSessionGroup records the split a tab is shown in (nil: none), so a
// window that joins late, or reloads, shows its panes together again.
func (a *App) SetSessionGroup(id int, group map[string]any) error {
	info, ok := a.sessions.Info(id)
	if !ok {
		return nil
	}
	meta := map[string]any{}
	_ = json.Unmarshal(info.Meta, &meta)
	if group == nil {
		delete(meta, "group")
	} else {
		meta["group"] = group
	}
	m, err := metaJSON(meta)
	if err != nil {
		return err
	}
	a.sessions.SetMeta(id, m)
	return nil
}

// CloseSession ends a session.
func (a *App) CloseSession(id int) { a.sessions.Close(id) }

// teamsInProcess keeps Claude Code's agent teams in the tab: with
// teammateMode "tmux" (or "auto" under a tmux Quarters was started from)
// teammates open in tmux panes no Quarters window shows, or fail when tmux
// isn't installed. A --settings of the user's own is left alone.
const teamsInProcess = `{"teammateMode":"in-process"}`

func inProcessTeams(argv []string) []string {
	if slices.ContainsFunc(argv, func(a string) bool { return a == "--settings" || strings.HasPrefix(a, "--settings=") }) {
		return argv
	}
	return append(argv, "--settings", teamsInProcess)
}
