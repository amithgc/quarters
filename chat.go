package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/stats"
)

// ChatView is a Claude tab's conversation as a chat (the phone's view).
type ChatView struct {
	Version string           `json:"version"`
	Same    bool             `json:"same"` // unchanged since version
	Items   []stats.ChatItem `json:"items"`
}

const chatItems = 300 // newest items sent to a phone

// Chat returns the conversation of Claude tab id, or Same when its log has
// not changed since version.
func (a *App) Chat(id int, version string) (ChatView, error) {
	path, err := a.chatLog(id)
	if err != nil || path == "" {
		return ChatView{Items: []stats.ChatItem{}}, err
	}
	if v, err := stats.ChatVersion(path); err == nil && v == version {
		return ChatView{Version: v, Same: true}, nil
	}
	c, err := stats.ReadChat(path, chatItems)
	if err != nil {
		return ChatView{Items: []stats.ChatItem{}}, nil // not written yet
	}
	return ChatView{Version: c.Version, Items: c.Items}, nil
}

// TabImage returns an image file named in tab id (as a data: URL), for the
// phone's chat and the terminal's image links. Only a path the tab itself
// showed is read: one the agent wrote in its conversation, or one printed
// in the tab's terminal. So a device can't use this to fetch other files
// from the Mac.
func (a *App) TabImage(id int, path string) (string, error) {
	shown := false
	if log, err := a.chatLog(id); err == nil && log != "" {
		if c, err := stats.ReadChat(log, chatItems); err == nil {
			shown = mentioned(c.Items, path)
		}
	}
	if !shown && !a.sessions.Printed(id, path) {
		return "", fmt.Errorf("that image isn't in this tab")
	}
	full := config.ExpandPath(path)
	if !filepath.IsAbs(full) {
		return "", fmt.Errorf("not a full path")
	}
	return stats.FileImage(filepath.Clean(full))
}

// mentioned reports whether the agent wrote path in one of its replies
// (your own messages don't count).
func mentioned(items []stats.ChatItem, path string) bool {
	return path != "" && slices.ContainsFunc(items, func(it stats.ChatItem) bool { return it.Kind == "text" && strings.Contains(it.Text, path) })
}

// chatLog is the conversation log a Claude tab writes: its known
// conversation, or for "continue" the newest log written since it started.
func (a *App) chatLog(id int) (string, error) {
	for _, info := range a.sessions.List() {
		if info.ID != id {
			continue
		}
		m := metaOf(info)
		// only agents with a reader have a chat view (Claude Code's log today)
		if m.Kind == "shell" || m.AutoKey != "" || config.HarnessByID(m.Harness).Reader != config.Claude {
			return "", fmt.Errorf("no chat view for this tab")
		}
		conv := a.conversationOf(id, m, nil)
		if conv == "" || !uuidRe.MatchString(conv) {
			return "", nil
		}
		return filepath.Join(projectDir(orDefault(m.Cwd, m.Path)), conv+".jsonl"), nil
	}
	return "", fmt.Errorf("no such session")
}
