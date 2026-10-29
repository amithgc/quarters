package stats

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---- token use per project and day, from every Claude Code log ----

// UsageDays is how far back the usage view looks.
const UsageDays = 30

// DayTokens maps a day ("2006-01-02", local time) to tokens.
type DayTokens map[string]int

// usageFile is the running tally for one log, read incrementally.
type usageFile struct {
	offset  int64
	days    DayTokens
	lastID  string // one API response is logged as several lines
	lastDay string
	lastN   int
}

// UsageScanner tallies tokens per Claude Code project folder and day. Each
// scan only reads what was added to the logs since the last one.
type UsageScanner struct {
	mu    sync.Mutex
	files map[string]*usageFile
}

// NewUsageScanner returns an empty scanner.
func NewUsageScanner() *UsageScanner { return &UsageScanner{files: map[string]*usageFile{}} }

// Tokens counts what a response costs against a plan: what was sent that
// wasn't cached yet, what was written to the cache, and the reply. Cache
// reads are left out, as in the activity chart: they are cheap, and would
// swamp the rest.
func (u Usage) Tokens() int { return u.Input + u.Output + u.CacheWrite }

// Scan returns tokens per day for each folder in root (Claude Code's
// projects folder, one folder per working directory), counting the logs
// written to since `since`, subagents' included.
func (s *UsageScanner) Scan(root string, since time.Time) map[string]DayTokens {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := todayKey(since)
	out := map[string]DayTokens{}
	seen := map[string]bool{}
	dirs, _ := os.ReadDir(root)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(root, d.Name())
		_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if e.IsDir() {
				if path != dir && strings.Count(strings.TrimPrefix(path, dir), string(filepath.Separator)) > 2 {
					return filepath.SkipDir // a session's subagents are two levels down; nothing deeper
				}
				return nil
			}
			if !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			info, err := e.Info()
			if err != nil || info.ModTime().Before(since) {
				return nil
			}
			seen[path] = true
			f := s.update(path, info.Size())
			for day, n := range f.days {
				if day < cutoff || n == 0 {
					continue
				}
				if out[d.Name()] == nil {
					out[d.Name()] = DayTokens{}
				}
				out[d.Name()][day] += n
			}
			return nil
		})
	}
	for path := range s.files {
		if !seen[path] {
			delete(s.files, path) // old or gone
		}
	}
	return out
}

// usageLine is the part of a log line the tally needs.
type usageLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   *struct {
		ID    string `json:"id"`
		Usage *struct {
			Input      int `json:"input_tokens"`
			Output     int `json:"output_tokens"`
			CacheRead  int `json:"cache_read_input_tokens"`
			CacheWrite int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

var usageKey = []byte(`"usage"`)

func (s *UsageScanner) update(path string, size int64) *usageFile {
	f := s.files[path]
	if f == nil || size < f.offset { // new, or rewritten: start over
		f = &usageFile{days: DayTokens{}}
		s.files[path] = f
	}
	if size == f.offset {
		return f
	}
	fh, err := os.Open(path)
	if err != nil {
		return f
	}
	defer fh.Close()
	if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
		return f
	}
	br := bufio.NewReaderSize(fh, 256*1024)
	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// a long line (a big tool result): read the rest of it
			rest, err2 := br.ReadBytes('\n')
			if err2 != nil {
				break
			}
			line = append(append([]byte(nil), line...), rest...)
		} else if err != nil {
			break // a partial last line is read again next time
		}
		f.offset += int64(len(line))
		if bytes.Contains(line, usageKey) {
			f.parse(line)
		}
	}
	return f
}

func (f *usageFile) parse(line []byte) {
	var l usageLine
	if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message == nil || l.Message.Usage == nil {
		return
	}
	ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		return
	}
	m := l.Message
	n := Usage{m.Usage.Input, m.Usage.Output, m.Usage.CacheRead, m.Usage.CacheWrite}.Tokens()
	day := todayKey(ts)
	// one response, several lines with its usage: count it once, the latest
	if m.ID != "" && m.ID == f.lastID {
		f.days[f.lastDay] -= f.lastN
	}
	f.lastID, f.lastDay, f.lastN = m.ID, day, n
	f.days[day] += n
}
