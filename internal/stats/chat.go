package stats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A conversation as a chat, for the phone: what you said, what Claude
// said, and what it did (tool calls with their results), read from Claude
// Code's conversation log.

// ChatTool is one tool call and what came back.
type ChatTool struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`           // the command, file or pattern
	Old    string `json:"old,omitempty"`    // Edit: the text replaced
	New    string `json:"new,omitempty"`    // Edit, Write: the new text
	Result string `json:"result,omitempty"` // what the tool returned (clipped)
	Error  bool   `json:"error,omitempty"`
}

// ChatItem is a message from you, text from Claude, or a run of tool calls.
type ChatItem struct {
	Kind  string     `json:"kind"` // "user", "text" or "tools"
	Text  string     `json:"text,omitempty"`
	Tools []ChatTool `json:"tools,omitempty"`
	Time  int64      `json:"time"` // unix ms
}

// Chat is the newest items of a conversation. Version changes when the log
// does.
type Chat struct {
	Version string     `json:"version"`
	Items   []ChatItem `json:"items"`
}

const (
	chatTail   = 4 << 20 // only the end of a long log is read
	chatClip   = 1500    // characters kept of a result or an edit
	chatDetail = 160
)

// ChatVersion is a cheap fingerprint of the log at path.
func ChatVersion(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%d", st.Size(), st.ModTime().UnixNano()), nil
}

// ReadChat reads the conversation log at path into at most max items.
func ReadChat(path string, max int) (Chat, error) {
	f, err := os.Open(path)
	if err != nil {
		return Chat{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Chat{}, err
	}
	c := Chat{Version: fmt.Sprintf("%d-%d", st.Size(), st.ModTime().UnixNano()), Items: []ChatItem{}}
	r := bufio.NewReaderSize(f, 256<<10)
	if st.Size() > chatTail { // start at the first whole line of the tail
		if _, err := f.Seek(st.Size()-chatTail, io.SeekStart); err != nil {
			return c, err
		}
		r.Reset(f)
		if _, err := r.ReadBytes('\n'); err != nil {
			return c, nil
		}
	}
	tools := map[string][2]int{} // tool call id → item, tool
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			c.add(line, tools)
		}
		if err != nil {
			break
		}
	}
	if len(c.Items) > max {
		c.Items = c.Items[len(c.Items)-max:]
	}
	return c, nil
}

type chatLine struct {
	Type        string `json:"type"`
	Timestamp   string `json:"timestamp"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Message     *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type chatBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func (c *Chat) add(raw []byte, tools map[string][2]int) {
	var l chatLine
	if json.Unmarshal(raw, &l) != nil || l.Message == nil || l.IsSidechain || l.IsMeta {
		return
	}
	ts, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
	at := ts.UnixMilli()
	var s string
	if json.Unmarshal(l.Message.Content, &s) == nil { // a plain typed prompt
		if l.Type == "user" {
			c.say("user", s, at)
		}
		return
	}
	var blocks []chatBlock
	if json.Unmarshal(l.Message.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		switch {
		case l.Type == "user" && b.Type == "text":
			c.say("user", b.Text, at)
		case l.Type == "user" && b.Type == "tool_result":
			if at, ok := tools[b.ToolUseID]; ok {
				t := &c.Items[at[0]].Tools[at[1]]
				t.Result, t.Error = clipText(resultText(b.Content), chatClip), b.IsError
			}
		case l.Type == "assistant" && b.Type == "text":
			c.say("text", b.Text, at)
		case l.Type == "assistant" && b.Type == "tool_use":
			t := toolOf(b)
			n := len(c.Items)
			if n == 0 || c.Items[n-1].Kind != "tools" {
				c.Items = append(c.Items, ChatItem{Kind: "tools", Time: at})
				n++
			}
			c.Items[n-1].Tools = append(c.Items[n-1].Tools, t)
			tools[b.ID] = [2]int{n - 1, len(c.Items[n-1].Tools) - 1}
		}
	}
}

// say adds text from you or Claude; command output and system notes (they
// start with "<") are not part of the conversation.
func (c *Chat) say(kind, text string, at int64) {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "<") || strings.HasPrefix(text, "[Request interrupted") {
		return
	}
	c.Items = append(c.Items, ChatItem{Kind: kind, Text: text, Time: at})
}

func toolOf(b chatBlock) ChatTool {
	t := ChatTool{Name: b.Name}
	var in map[string]any
	_ = json.Unmarshal(b.Input, &in)
	str := func(k string) string { v, _ := in[k].(string); return v }
	switch b.Name {
	case "Bash":
		t.Detail = firstLine(str("command"))
	case "Read", "Write", "Edit", "MultiEdit", "NotebookEdit":
		t.Detail = shortPath(orStr(str("file_path"), str("notebook_path")))
	case "Grep", "Glob":
		t.Detail = str("pattern")
	case "WebFetch":
		t.Detail = str("url")
	case "WebSearch":
		t.Detail = str("query")
	case "Task", "Agent":
		t.Detail = str("description")
	default:
		t.Detail = orStr(str("description"), str("query"), str("path"))
	}
	t.Detail = clipText(t.Detail, chatDetail)
	switch b.Name {
	case "Edit":
		t.Old, t.New = clipText(str("old_string"), chatClip), clipText(str("new_string"), chatClip)
	case "Write":
		t.New = clipText(str("content"), chatClip)
	}
	return t
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []chatBlock
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func shortPath(p string) string {
	if p == "" {
		return ""
	}
	dir, file := filepath.Split(p)
	if parent := filepath.Base(filepath.Clean(dir)); parent != "" && parent != "/" && parent != "." {
		return parent + "/" + file
	}
	return file
}

func orStr(list ...string) string {
	for _, s := range list {
		if s != "" {
			return s
		}
	}
	return ""
}

func clipText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
