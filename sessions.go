package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/amithgc/quarters/internal/session"
)

// ---- sessions ----

// StartSession starts Claude Code ("claude"), Claude Code continuing the
// last conversation ("continue") or a shell ("shell") in a project. meta is
// what other windows need to show the tab (title, automation, …).
func (a *App) StartSession(path, kind string, cols, rows int, meta map[string]any) (int, error) {
	return a.startSession(macClient, path, kind, cols, rows, meta)
}

func (a *App) startSession(client, path, kind string, cols, rows int, meta map[string]any) (int, error) {
	cfg := a.config()
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
		argv = append([]string{}, cfg.ClaudeCommand(cp, yolo)...)
		if cfg.ClaudeRemoteControl {
			name := cp.Name
			if name == "" {
				name = filepath.Base(path)
			}
			argv = append(argv, "--remote-control", name)
		}
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
func (a *App) Write(id int, data string) { a.sessions.Write(macClient, id, data) }

// Resize changes a session's terminal size (if the Mac window drives it).
func (a *App) Resize(id, cols, rows int) { a.sessions.Resize(macClient, id, cols, rows) }

// Claim gives a session the Mac window's terminal size.
func (a *App) Claim(id, cols, rows int) { a.sessions.Claim(macClient, id, cols, rows) }

// ClientID is this window's name among the windows sharing sessions.
func (a *App) ClientID() string { return macClient }

// CloseSession ends a session.
func (a *App) CloseSession(id int) { a.sessions.Close(id) }
