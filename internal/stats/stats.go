// Package stats reads Claude Code's session logs (token usage, model,
// mode, tools) and git status for the stats panel.
package stats

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// BucketSize and Buckets define the activity chart: 24 × 5 min = 2 h.
const (
	BucketSize = 5 * time.Minute
	Buckets    = 24
)

// Usage is a token tally.
type Usage struct {
	Input, Output, CacheRead, CacheWrite int
}

func (u *Usage) add(o Usage, sign int) {
	u.Input += sign * o.Input
	u.Output += sign * o.Output
	u.CacheRead += sign * o.CacheRead
	u.CacheWrite += sign * o.CacheWrite
}

// CacheHit is the share of prompt tokens served from the cache.
func (u Usage) CacheHit() float64 {
	total := u.Input + u.CacheRead + u.CacheWrite
	if total == 0 {
		return 0
	}
	return float64(u.CacheRead) / float64(total)
}

// ToolCount is how often a tool was called.
type ToolCount struct {
	Name  string
	Count int
}

// Session is what one session log says.
type Session struct {
	File       string
	Title      string
	Model      string
	Mode       string
	Start      time.Time
	Last       time.Time
	Context    int // prompt tokens of the latest main-thread request
	Turns      int // prompts typed by the user
	Usage      Usage
	Tools      []ToolCount
	MCPUsed    map[string]int
	Activity   [Buckets]int // tokens per bucket, oldest first, ending now
	MaxContext int
	Edited     []string // files Claude edited or wrote, first edit first
}

// fileAgg is the running tally for one log file, read incrementally.
type fileAgg struct {
	offset    int64
	session   Session
	tools     map[string]int
	lastID    string
	lastUsage Usage
	lastTime  time.Time
	byMinute  map[int64]int // unix minute → tokens
	today     Usage
	todayKey  string
}

// Reader caches parsed logs so each refresh only reads new lines.
type Reader struct {
	mu    sync.Mutex
	files map[string]*fileAgg
}

// NewReader returns an empty Reader.
func NewReader() *Reader { return &Reader{files: map[string]*fileAgg{}} }

type logLine struct {
	Type           string `json:"type"`
	Timestamp      string `json:"timestamp"`
	IsSidechain    bool   `json:"isSidechain"`
	PermissionMode string `json:"permissionMode"`
	AITitle        string `json:"aiTitle"`
	Message        *struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input      int `json:"input_tokens"`
			Output     int `json:"output_tokens"`
			CacheRead  int `json:"cache_read_input_tokens"`
			CacheWrite int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type contentBlock struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Text  string `json:"text"`
	Input *struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	} `json:"input"`
}

// editTools change files; their input names the file.
var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

func todayKey(t time.Time) string { return t.Local().Format("2006-01-02") }

// update reads new lines of path into its tally.
func (r *Reader) update(path string) (*fileAgg, error) {
	agg := r.files[path]
	if agg == nil {
		agg = &fileAgg{tools: map[string]int{}, byMinute: map[int64]int{},
			session: Session{File: path, MCPUsed: map[string]int{}}}
		r.files[path] = agg
	}
	if k := todayKey(time.Now()); agg.todayKey != k {
		// New day: recount today's usage from the start of the file.
		if agg.todayKey != "" {
			*agg = fileAgg{tools: map[string]int{}, byMinute: map[int64]int{},
				session: Session{File: path, MCPUsed: map[string]int{}}}
		}
		agg.todayKey = k
	}

	f, err := os.Open(path)
	if err != nil {
		return agg, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return agg, err
	}
	if st.Size() < agg.offset { // rewritten: start over
		*agg = fileAgg{tools: map[string]int{}, byMinute: map[int64]int{}, todayKey: agg.todayKey,
			session: Session{File: path, MCPUsed: map[string]int{}}}
	}
	if st.Size() == agg.offset {
		return agg, nil
	}
	if _, err := f.Seek(agg.offset, io.SeekStart); err != nil {
		return agg, err
	}
	br := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			break // a partial last line is read again next time
		}
		agg.offset += int64(len(line))
		agg.parse(line)
	}
	return agg, nil
}

func (a *fileAgg) parse(line []byte) {
	var l logLine
	if json.Unmarshal(line, &l) != nil {
		return
	}
	s := &a.session
	ts, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
	if !ts.IsZero() {
		if s.Start.IsZero() || ts.Before(s.Start) {
			s.Start = ts
		}
		if ts.After(s.Last) {
			s.Last = ts
		}
	}
	switch l.Type {
	case "ai-title":
		s.Title = l.AITitle
	case "permission-mode":
		s.Mode = l.PermissionMode
	case "user":
		if l.Message != nil && !l.IsSidechain && isTypedPrompt(l.Message.Content) {
			s.Turns++
		}
	case "assistant":
		if l.Message == nil {
			return
		}
		m := l.Message
		if m.Model != "" && m.Model != "<synthetic>" && !l.IsSidechain {
			s.Model = m.Model
		}
		var blocks []contentBlock
		if json.Unmarshal(m.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type != "tool_use" {
					continue
				}
				a.tools[b.Name]++
				if editTools[b.Name] && b.Input != nil && !l.IsSidechain {
					f := b.Input.FilePath
					if f == "" {
						f = b.Input.NotebookPath
					}
					if f != "" && !slices.Contains(s.Edited, f) {
						s.Edited = append(s.Edited, f)
					}
				}
				if rest, ok := strings.CutPrefix(b.Name, "mcp__"); ok {
					server, _, _ := strings.Cut(rest, "__")
					s.MCPUsed[server]++
				}
			}
		}
		if m.Usage == nil {
			return
		}
		u := Usage{m.Usage.Input, m.Usage.Output, m.Usage.CacheRead, m.Usage.CacheWrite}
		// One API response is logged as several lines (one per content
		// block) repeating its usage; count it once, keeping the latest.
		if m.ID != "" && m.ID == a.lastID {
			s.Usage.add(a.lastUsage, -1)
			a.account(a.lastTime, a.lastUsage, -1)
		}
		a.lastID, a.lastUsage, a.lastTime = m.ID, u, ts
		s.Usage.add(u, 1)
		a.account(ts, u, 1)
		if !l.IsSidechain {
			s.Context = u.Input + u.CacheRead + u.CacheWrite
			s.MaxContext = max(s.MaxContext, s.Context)
		}
	}
}

// account adds a response to today's tally and the activity chart.
func (a *fileAgg) account(ts time.Time, u Usage, sign int) {
	if ts.IsZero() {
		return
	}
	if todayKey(ts) == a.todayKey {
		a.today.add(u, sign)
	}
	if time.Since(ts) < 3*time.Hour {
		a.byMinute[ts.Unix()/60] += sign * (u.Input + u.Output + u.CacheWrite)
	}
}

// isTypedPrompt tells a prompt the user typed from a tool result.
func isTypedPrompt(content json.RawMessage) bool {
	if len(content) > 0 && content[0] == '"' {
		return true
	}
	var blocks []contentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return false
		}
	}
	return len(blocks) > 0
}

// Session returns the tally of the newest log in claudeDir.
func (r *Reader) Session(claudeDir string) (Session, bool) {
	path := newestLog(claudeDir)
	if path == "" {
		return Session{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	agg, err := r.update(path)
	if err != nil {
		return Session{}, false
	}
	s := agg.session
	s.Edited = slices.Clone(agg.session.Edited)
	s.MCPUsed = map[string]int{}
	for k, v := range agg.session.MCPUsed {
		s.MCPUsed[k] = v
	}
	for name, n := range agg.tools {
		if !strings.HasPrefix(name, "mcp__") {
			s.Tools = append(s.Tools, ToolCount{name, n})
		}
	}
	sort.Slice(s.Tools, func(i, j int) bool {
		if s.Tools[i].Count != s.Tools[j].Count {
			return s.Tools[i].Count > s.Tools[j].Count
		}
		return s.Tools[i].Name < s.Tools[j].Name
	})
	agg.addActivity(&s.Activity)
	return s, true
}

// addActivity adds the file's tokens to the activity chart, and forgets
// minutes too old for it.
func (a *fileAgg) addActivity(chart *[Buckets]int) {
	now := time.Now().Unix() / 60
	bucketMin := int64(BucketSize / time.Minute)
	for minute, n := range a.byMinute {
		age := now - minute
		if age < 0 || age >= Buckets*bucketMin {
			if age >= 3*60 {
				delete(a.byMinute, minute)
			}
			continue
		}
		chart[Buckets-1-int(age/bucketMin)] += n
	}
}

// Day is today's use across every project: tokens, sessions, and the
// activity chart of the last two hours.
type Day struct {
	Usage    Usage
	Sessions int
	Activity [Buckets]int
}

// Today adds up today's usage across every project's logs.
func (r *Reader) Today(root string) Day {
	today := todayKey(time.Now())
	recent := time.Now().Add(-Buckets * BucketSize)
	var d Day
	dirs, _ := os.ReadDir(root)
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(root, dir.Name()))
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			info, err := f.Info()
			// written today, or in the chart's two hours (just after midnight)
			if err != nil || (todayKey(info.ModTime()) != today && info.ModTime().Before(recent)) {
				continue
			}
			path := filepath.Join(root, dir.Name(), f.Name())
			r.mu.Lock()
			agg, err := r.update(path)
			if err == nil {
				if agg.today != (Usage{}) {
					d.Usage.add(agg.today, 1)
					d.Sessions++
				}
				agg.addActivity(&d.Activity)
			}
			r.mu.Unlock()
		}
	}
	return d
}

// DayChat is one conversation in a project's logs.
type DayChat struct {
	Title   string
	Prompts int
	Last    time.Time
}

// TodayChats lists the conversations in a project's log folder that were
// active today, the most recent first.
func (r *Reader) TodayChats(dir string) []DayChat {
	today := todayKey(time.Now())
	entries, _ := os.ReadDir(dir)
	var out []DayChat
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil || todayKey(info.ModTime()) != today {
			continue
		}
		r.mu.Lock()
		agg, err := r.update(filepath.Join(dir, e.Name()))
		if err == nil && agg.session.Turns > 0 {
			out = append(out, DayChat{Title: agg.session.Title, Prompts: agg.session.Turns, Last: agg.session.Last})
		}
		r.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out
}

func newestLog(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestT time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(bestT) {
			best, bestT = filepath.Join(dir, e.Name()), info.ModTime()
		}
	}
	return best
}

// ConfiguredMCP lists the MCP servers configured for a project: global and
// per-project entries in ~/.claude.json plus the project's .mcp.json.
func ConfiguredMCP(projectPath string) []string {
	home, _ := os.UserHomeDir()
	files := []string{filepath.Join(home, ".claude.json"), filepath.Join(projectPath, ".mcp.json")}
	return memo("mcp\x00"+projectPath, files, func() []string { return configuredMCP(projectPath) })
}

func configuredMCP(projectPath string) []string {
	seen := map[string]bool{}
	var out []string
	addKeys := func(m map[string]json.RawMessage) {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	home, _ := os.UserHomeDir()
	if data, err := os.ReadFile(filepath.Join(home, ".claude.json")); err == nil {
		var cfg struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
			Projects   map[string]struct {
				MCPServers map[string]json.RawMessage `json:"mcpServers"`
			} `json:"projects"`
		}
		if json.Unmarshal(data, &cfg) == nil {
			addKeys(cfg.MCPServers)
			addKeys(cfg.Projects[projectPath].MCPServers)
		}
	}
	if data, err := os.ReadFile(filepath.Join(projectPath, ".mcp.json")); err == nil {
		var cfg struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if json.Unmarshal(data, &cfg) == nil {
			addKeys(cfg.MCPServers)
		}
	}
	sort.Strings(out)
	return out
}

// ContextWindow guesses the context window from the "model" setting in the
// project's and the user's Claude Code settings: a "[1m]" model has 1M
// tokens. It returns 0 when no setting says so.
func ContextWindow(projectPath string) int {
	home, _ := os.UserHomeDir()
	files := []string{
		filepath.Join(projectPath, ".claude", "settings.local.json"),
		filepath.Join(projectPath, ".claude", "settings.json"),
		filepath.Join(home, ".claude", "settings.json"),
	}
	return memo("window\x00"+projectPath, files, func() int { return contextWindow(files) })
}

func contextWindow(files []string) int {
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var s struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(data, &s) != nil || s.Model == "" {
			continue
		}
		if strings.Contains(strings.ToLower(s.Model), "[1m]") {
			return 1_000_000
		}
		return 200_000
	}
	return 0
}
