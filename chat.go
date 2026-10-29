package main

import (
	"fmt"
	"path/filepath"

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

// chatLog is the conversation log a Claude tab writes: its known
// conversation, or for "continue" the newest log written since it started.
func (a *App) chatLog(id int) (string, error) {
	for _, info := range a.sessions.List() {
		if info.ID != id {
			continue
		}
		m := metaOf(info)
		if m.Kind == "shell" || m.AutoKey != "" || (m.Harness != "" && m.Harness != config.Claude) {
			return "", fmt.Errorf("not a Claude tab")
		}
		conv := a.conversationOf(id, m, nil)
		if conv == "" || !uuidRe.MatchString(conv) {
			return "", nil
		}
		return filepath.Join(projectDir(orDefault(m.Cwd, m.Path)), conv+".jsonl"), nil
	}
	return "", fmt.Errorf("no such session")
}
