package main

import (
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
	title, items, keys := menuBarContent(acct, waiting, now)
	if title != "◐2  42% ~45m" {
		t.Errorf("title = %q", title)
	}
	want := strings.Join([]string{
		"-1\tClaude plan · Max",
		"-1\tCurrent session — 42% · runs out in ~45 min at this pace",
		"-1\tWeekly · all models — 18% · resets " + now.Add(72*time.Hour).Format("Mon 15:04"),
		"-",
		"-1\tWaiting for you",
		"1\tapi gateway — your turn · 3 min · cache 2 min left",
		"2\tshop — asks you · cache expired",
		"-",
		"0\tOpen Quarters",
	}, "\n")
	if items != want {
		t.Errorf("items:\n%s\nwant:\n%s", items, want)
	}
	if len(keys) != 2 || keys[0] != "k1" || keys[1] != "k2" {
		t.Errorf("keys = %v", keys)
	}

	title, items, _ = menuBarContent(stats.Account{Error: "Not signed in — run claude and /login"}, nil, now)
	if title != "" || !strings.HasPrefix(items, "-1\tClaude plan: Not signed in") || !strings.Contains(items, "Nobody is waiting for you") {
		t.Errorf("signed out: %q / %q", title, items)
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
