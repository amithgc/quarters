package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/stats"
)

func TestPlanAlerts(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local)
	reset := now.Add(2 * time.Hour).UnixMilli()
	watch := map[string]*limitWatch{}
	check := func(pct float64, resetsAt, runsOut int64) []string {
		var out []string
		for _, n := range planAlerts([]stats.Limit{{Label: "Current session", Percent: pct, ResetsAt: resetsAt, RunsOutAt: runsOut}}, watch, 80, now) {
			out = append(out, n.Title)
		}
		return out
	}
	steps := []struct {
		pct     float64
		resets  int64
		runsOut int64
		want    []string
	}{
		{50, reset, 0, nil},
		{60, reset, now.Add(25 * time.Minute).UnixMilli(), []string{"Claude plan runs out in ~25 min"}},
		{65, reset, now.Add(20 * time.Minute).UnixMilli(), nil}, // said once per window
		{81, reset, 0, []string{"Claude usage at 81%"}},
		{85, reset, 0, nil},
		// from below 90 to 100: one alert, the highest level
		{100, reset, 0, []string{"Claude plan limit reached"}},
		{100, reset, 0, nil},
		// the window reset (and was announced, so the reset is too)
		{3, reset + 5*time.Hour.Milliseconds(), 0, []string{"Claude current session reset"}},
		{40, reset + 5*time.Hour.Milliseconds(), 0, nil},
		// a reset nothing was said about isn't announced
		{1, reset + 10*time.Hour.Milliseconds(), 0, nil},
		{95, reset + 10*time.Hour.Milliseconds(), 0, []string{"Claude usage at 95%"}},
	}
	for i, s := range steps {
		got := check(s.pct, s.resets, s.runsOut)
		if strings.Join(got, "|") != strings.Join(s.want, "|") {
			t.Errorf("step %d (%v%%): got %q, want %q", i, s.pct, got, s.want)
		}
	}
	if got := usageLevels(95); len(got) != 2 || got[0] != 95 || got[1] != 100 {
		t.Errorf("levels from 95%% = %v", got)
	}
}

func TestMenuBarContent(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local)
	acct := stats.Account{Plan: "Max", Limits: []stats.Limit{
		{Label: "Current session", Percent: 42, ResetsAt: now.Add(2*time.Hour + 10*time.Minute).UnixMilli(), RunsOutAt: now.Add(45 * time.Minute).UnixMilli()},
		{Label: "Weekly · all models", Percent: 18, ResetsAt: now.Add(72 * time.Hour).UnixMilli()},
	}}
	waiting := []WaitingItem{
		{Key: "k1", Name: "api\tgateway", DoneAt: now.Add(-3 * time.Minute).UnixMilli(), CacheUntil: now.Add(2 * time.Minute).UnixMilli()},
		{Key: "k2", Name: "shop", Asks: true, DoneAt: now.UnixMilli(), CacheUntil: now.Add(-time.Minute).UnixMilli()},
	}
	title, rows, keys := menuBarContent(acct, waiting, now)
	if title != "◐2  42% ~45m" {
		t.Errorf("title = %q", title)
	}
	want := []menuRow{
		{Kind: "plan", Text: "Claude plan", Detail: "Max"},
		{Kind: "limit", Text: "Current session", Detail: "Runs out in ~45 min · resets in 2h 10m", Percent: 42, Level: "warn"},
		{Kind: "limit", Text: "Weekly · all models", Detail: "Resets " + now.Add(72*time.Hour).Format("Mon 15:04"), Percent: 18},
		{Kind: "sep"},
		{Kind: "section", Text: "Waiting for you"},
		{Kind: "tab", Text: "api gateway", Detail: "Your turn · 3 min ago · cache 2 min left", Tag: 1},
		{Kind: "tab", Text: "shop", Detail: "Asks you · cache expired", Tag: 2, Asks: true},
		{Kind: "sep"},
		{Kind: "open", Text: "Open Quarters"},
	}
	if fmt.Sprint(rows) != fmt.Sprint(want) {
		t.Errorf("rows:\n%v\nwant:\n%v", rows, want)
	}
	if len(keys) != 2 || keys[0] != "k1" || keys[1] != "k2" {
		t.Errorf("keys = %v", keys)
	}
	if l := limitLevel(stats.Limit{Percent: 95}); l != "crit" {
		t.Errorf("95%% is %q", l)
	}

	title, rows, _ = menuBarContent(stats.Account{Error: "Not signed in — run claude and /login"}, nil, now)
	if title != "" || rows[0].Kind != "note" || !strings.HasPrefix(rows[0].Detail, "Not signed in") || rows[2].Kind != "empty" {
		t.Errorf("signed out: %q / %v", title, rows)
	}
}

func TestSecretOwner(t *testing.T) {
	cfg := &config.Config{Projects: []config.Project{{Name: "Code", Path: "/Users/me/code"}, {Name: "Shop", Path: "/Users/me/code/shop"}}}
	a := &App{}
	cases := []struct{ cwd, file, project, name string }{
		{"/Users/me/code/shop/web", "/x/-Users-me-code-shop-web/c.jsonl", "/Users/me/code/shop", "Shop"}, // the innermost project
		{"/Users/me/code", "/x/-Users-me-code/c.jsonl", "/Users/me/code", "Code"},
		{"/tmp/wt/shop-fix", "/x/-Users-me-code-shop/c.jsonl", "/Users/me/code/shop", "Shop"}, // by its log folder
		{"/Users/me/other", "/x/-Users-me-other/c.jsonl", "", "other"},
	}
	for _, c := range cases {
		got := a.secretOwner(cfg, stats.SecretHit{Cwd: c.cwd, File: c.file})
		if got.Project != c.project || got.Name != c.name {
			t.Errorf("%s: got %q %q, want %q %q", c.cwd, got.Project, got.Name, c.project, c.name)
		}
	}
}
