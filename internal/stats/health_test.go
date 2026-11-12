package stats

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPriceOf(t *testing.T) {
	cases := []struct {
		model string
		want  Price
		ok    bool
	}{
		{"claude-opus-5-5", Price{4, 5, 8, 0.20, 20}, true},
		{"claude-opus-5-5[1m]", Price{4, 5, 8, 0.20, 20}, true},
		{"claude-opus-5", Price{5, 6.25, 10, 0.50, 25}, true},
		{"claude-opus-4-1-20250805", Price{15, 18.75, 30, 1.50, 75}, true},
		{"claude-opus-4-20250514", Price{15, 18.75, 30, 1.50, 75}, true},
		{"claude-opus-4-6", Price{5, 6.25, 10, 0.50, 25}, true},
		{"claude-sonnet-4-20250514", Price{3, 3.75, 6, 0.30, 15}, true},
		{"claude-sonnet-5", Price{2, 2.5, 4, 0.20, 10}, true},
		{"claude-sonnet-5-5", Price{2, 2.5, 4, 0.10, 10}, true},
		{"us.anthropic.claude-haiku-4-5-20251001-v1:0", Price{1, 1.25, 2, 0.10, 5}, true},
		{"claude-fable-5", Price{10, 12.5, 20, 1, 50}, true},
		{"claude-fable-5-2", Price{8, 10, 16, 0.20, 40}, true},
		// a model newer than the table: its family's newest price
		{"claude-opus-6", Price{4, 5, 8, 0.20, 20}, true},
		{"<synthetic>", Price{}, false},
		{"", Price{}, false},
	}
	for _, c := range cases {
		got, ok := PriceOf(c.model)
		if got != c.want || ok != c.ok {
			t.Errorf("PriceOf(%q) = %+v %v, want %+v %v", c.model, got, ok, c.want, c.ok)
		}
	}
}

func TestResponseCost(t *testing.T) {
	// 1M of each kind on Opus 5.5, half the cache writes for an hour
	r := Response{Model: "claude-opus-5-5", Usage: Usage{Input: 1e6, Output: 1e6, CacheRead: 1e6, CacheWrite: 2e6}, Write1h: 1e6}
	if got, want := r.Cost(), 4+20+0.20+5+8.0; !near(got, want) {
		t.Errorf("cost = %v, want %v", got, want)
	}
	r.Speed = "fast"
	if got, want := r.Cost(), 2*(4+20+0.20+5+8.0); !near(got, want) {
		t.Errorf("fast cost = %v, want %v", got, want)
	}
	r.Speed, r.Geo = "", "us"
	if got, want := r.Cost(), 1.1*(4+20+0.20+5+8.0); !near(got, want) {
		t.Errorf("US-only cost = %v, want %v", got, want)
	}
	// Haiku 5.5 costs more past a 100k-token prompt
	short := Response{Model: "claude-haiku-5-5", Usage: Usage{Input: 100_000}}
	long := Response{Model: "claude-haiku-5-5", Usage: Usage{Input: 100_001}}
	if !near(short.Cost(), 0.01) || !near(long.Cost(), 0.0500005) {
		t.Errorf("haiku costs = %v, %v", short.Cost(), long.Cost())
	}
	if (Response{Model: "<synthetic>", Usage: Usage{Output: 100}}).Cost() != 0 {
		t.Error("a synthetic reply costs nothing")
	}
}

func TestSessionHealth(t *testing.T) {
	dir := t.TempDir()
	at := time.Now().UTC().Add(-2 * time.Minute)
	ts := at.Format(time.RFC3339Nano)
	log := strings.Join([]string{
		`{"type":"user","timestamp":"` + ts + `","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"` + ts + `","message":{"id":"m1","model":"claude-opus-5-5","content":[],"usage":{"input_tokens":1000000,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":1000000,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":1000000}}}}`,
		// the same response again (one line per content block): counted once
		`{"type":"assistant","timestamp":"` + ts + `","message":{"id":"m1","model":"claude-opus-5-5","content":[],"usage":{"input_tokens":1000000,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":1000000,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":1000000}}}}`,
		`{"type":"system","subtype":"compact_boundary","timestamp":"` + ts + `","content":"Conversation compacted"}`,
		`{"type":"system","subtype":"compact_boundary","isSidechain":true,"timestamp":"` + ts + `"}`,
		`{"type":"system","subtype":"turn_duration","timestamp":"` + ts + `"}`,
		// a subagent's request doesn't move the main thread's cache
		`{"type":"assistant","isSidechain":true,"timestamp":"` + time.Now().UTC().Format(time.RFC3339Nano) + `","message":{"id":"m2","model":"claude-haiku-5-5","content":[],"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":10}}}`,
	}, "\n") + "\n"
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	s, ok := NewReader().SessionFile(path)
	if !ok {
		t.Fatal("no session")
	}
	if s.Compactions != 1 {
		t.Errorf("compactions = %d, want 1", s.Compactions)
	}
	if s.CacheTTL != time.Hour {
		t.Errorf("cache TTL = %v, want 1h", s.CacheTTL)
	}
	if got := s.CacheUntil(); !got.Equal(at.Add(time.Hour)) {
		t.Errorf("cache until = %v, want %v", got, at.Add(time.Hour))
	}
	// 1M input at $4 and 1M 1-hour cache writes at $8, plus the subagent's
	// 10 five-minute writes on Haiku 5.5
	if want := 4 + 8 + 10*0.125/1e6; !near(s.Cost, want) {
		t.Errorf("cost = %v, want %v", s.Cost, want)
	}
	if _, ok := NewReader().SessionFile(filepath.Join(dir, "missing.jsonl")); ok {
		t.Error("a missing log has no session")
	}
}

func TestPace(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour).UnixMilli()
	p := NewPace()
	read := func(at time.Time, pct float64, resetsAt int64) Limit {
		acct := Account{Limits: []Limit{{Label: "Current session", Percent: pct, ResetsAt: resetsAt}}}
		p.Add(&acct, at)
		return acct.Limits[0]
	}
	// 40% → 50% in 10 minutes: 1% a minute, so the other 50% last 50 minutes
	read(now, 40, reset)
	read(now.Add(5*time.Minute), 45, reset)
	l := read(now.Add(10*time.Minute), 50, reset)
	if want := now.Add(60 * time.Minute).UnixMilli(); l.RunsOutAt != want {
		t.Errorf("runs out at %v, want %v", time.UnixMilli(l.RunsOutAt).UTC(), time.UnixMilli(want).UTC())
	}
	// a reset time that moves by a second is the same window
	if l := read(now.Add(11*time.Minute), 51, reset+1000); l.RunsOutAt == 0 {
		t.Error("lost the pace over a second's jitter in the reset time")
	}
	// idle for the whole window: no pace
	if l := read(now.Add(40*time.Minute), 51, reset); l.RunsOutAt != 0 {
		t.Errorf("idle, yet runs out at %v", time.UnixMilli(l.RunsOutAt))
	}
	// too slow to run out before the reset
	p = NewPace()
	read(now, 10, reset)
	if l := read(now.Add(10*time.Minute), 11, reset); l.RunsOutAt != 0 {
		t.Errorf("runs out after the reset, yet RunsOutAt = %v", time.UnixMilli(l.RunsOutAt))
	}
	// the window reset: earlier readings don't count
	p = NewPace()
	read(now, 90, reset)
	if l := read(now.Add(10*time.Minute), 2, now.Add(5*time.Hour).UnixMilli()); l.RunsOutAt != 0 {
		t.Errorf("after a reset, RunsOutAt = %v", time.UnixMilli(l.RunsOutAt))
	}
}
