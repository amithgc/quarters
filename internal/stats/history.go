package stats

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Conversation is one past Claude Code conversation of a project.
type Conversation struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	First   string `json:"first"` // the first prompt
	Start   int64  `json:"start"` // unix ms
	Last    int64  `json:"last"`
	Prompts int    `json:"prompts"`
	Match   string `json:"match"` // text around the search hit
}

// convText is what a log says, kept for searching.
type convText struct {
	size, mod int64
	conv      Conversation
	text      string // prompts and replies, lowercased
	raw       string // the same, as written
}

const (
	maxConvText      = 400 << 10 // text kept and searched per conversation
	maxConversations = 2000      // conversations kept in memory
)

// History reads and caches a projects' conversation logs.
type History struct {
	mu    sync.Mutex
	files map[string]*convText
}

// NewHistory returns an empty cache.
func NewHistory() *History { return &History{files: map[string]*convText{}} }

// Search lists the conversations in claudeDir, newest first; with query,
// only those whose prompts or replies contain it (any case).
func (h *History) Search(claudeDir, query string, limit int) []Conversation {
	entries, _ := os.ReadDir(claudeDir)
	q := strings.ToLower(strings.TrimSpace(query))
	var out []Conversation
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		c := h.read(filepath.Join(claudeDir, name), info)
		if c == nil || c.conv.Prompts == 0 {
			continue
		}
		conv := c.conv
		if q != "" {
			i := strings.Index(c.text, q)
			if i < 0 {
				continue
			}
			conv.Match = snippet(c.raw, i, len(q))
		}
		out = append(out, conv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (h *History) read(path string, info os.FileInfo) *convText {
	h.mu.Lock()
	c := h.files[path]
	h.mu.Unlock()
	if c != nil && c.size == info.Size() && c.mod == info.ModTime().UnixNano() {
		return c
	}
	c = parseConversation(path)
	if c == nil {
		return nil
	}
	c.size, c.mod = info.Size(), info.ModTime().UnixNano()
	h.mu.Lock()
	if len(h.files) >= maxConversations {
		clear(h.files) // rebuilt as needed
	}
	h.files[path] = c
	h.mu.Unlock()
	return c
}

func parseConversation(path string) *convText {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	c := &convText{conv: Conversation{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}}
	var b strings.Builder
	add := func(s string) {
		if b.Len() < maxConvText {
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	br := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			var l logLine
			if json.Unmarshal(line, &l) == nil {
				if ts, _ := time.Parse(time.RFC3339Nano, l.Timestamp); !ts.IsZero() {
					if c.conv.Start == 0 {
						c.conv.Start = ts.UnixMilli()
					}
					c.conv.Last = ts.UnixMilli()
				}
				switch {
				case l.Type == "ai-title":
					c.conv.Title = l.AITitle
				case l.Type == "user" && l.Message != nil && !l.IsSidechain && isTypedPrompt(l.Message.Content):
					t := promptText(l.Message.Content)
					if t == "" || strings.HasPrefix(t, "<") { // commands and hook output
						break
					}
					c.conv.Prompts++
					if c.conv.First == "" {
						c.conv.First = clip(t, 160)
					}
					add(t)
				case l.Type == "assistant" && l.Message != nil && !l.IsSidechain:
					var blocks []contentBlock
					if json.Unmarshal(l.Message.Content, &blocks) == nil {
						for _, bl := range blocks {
							if bl.Type == "text" {
								add(bl.Text)
							}
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	c.raw = b.String()
	c.text = strings.ToLower(c.raw)
	if len(c.text) != len(c.raw) { // lowercasing changed byte offsets: search the raw text's lowercase rune by rune
		c.raw = c.text
	}
	return c
}

// promptText is the typed text of a user message.
func promptText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []contentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, bl := range blocks {
		if bl.Type == "text" {
			parts = append(parts, bl.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// snippet is the text around a hit at byte i.
func snippet(s string, i, n int) string {
	from, to := max(0, i-60), min(len(s), i+n+80)
	for from > 0 && from < len(s) && s[from]&0xC0 == 0x80 {
		from--
	}
	for to < len(s) && s[to]&0xC0 == 0x80 {
		to++
	}
	out := strings.Join(strings.Fields(s[from:to]), " ")
	if from > 0 {
		out = "…" + out
	}
	if to < len(s) {
		out += "…"
	}
	return out
}
