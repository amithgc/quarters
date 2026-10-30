package main

import (
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/amithgc/quarters/internal/projects"
	"github.com/amithgc/quarters/internal/stats"
)

// ---- stats ----

// todayCache keeps today's totals, which scan every project's logs.
type todayCache struct {
	mu   sync.Mutex
	at   time.Time
	day  stats.Day
	busy bool
}

// accountCache keeps the plan's limits, fetched from Anthropic.
type accountCache struct {
	mu  sync.Mutex
	at  time.Time
	val stats.Account
}

// StatsView is everything the stats panel shows for one project.
type StatsView struct {
	Has        bool              `json:"has"`
	Title      string            `json:"title"`
	Model      string            `json:"model"`
	Mode       string            `json:"mode"`
	Start      int64             `json:"start"`
	Last       int64             `json:"last"`
	Turns      int               `json:"turns"`
	Context    int               `json:"context"`
	Window     int               `json:"window"`
	Usage      stats.Usage       `json:"usage"`
	Activity   []int             `json:"activity"`
	Tools      []stats.ToolCount `json:"tools"`
	MCP        []MCPView         `json:"mcp"`
	Git        stats.Git         `json:"git"`
	Today      stats.Usage       `json:"today"`
	TodayCount int               `json:"todaySessions"`
	Edited     []string          `json:"edited"` // files Claude edited this session
}

// MCPView is one MCP server.
type MCPView struct {
	Name  string `json:"name"`
	Calls int    `json:"calls"`
}

// Stats reads the newest Claude Code session log and git status of a project.
func (a *App) Stats(path string) StatsView {
	cfg := a.config()
	v := StatsView{Activity: make([]int, stats.Buckets)}
	if _, ok := a.project(path); !ok {
		return v
	}
	dir := projectDir(path)
	if s, ok := a.reader.Session(dir); ok {
		v.Has = true
		v.Title, v.Model, v.Mode, v.Turns, v.Context = s.Title, s.Model, s.Mode, s.Turns, s.Context
		v.Start, v.Last = s.Start.UnixMilli(), s.Last.UnixMilli()
		v.Usage, v.Activity = s.Usage, s.Activity[:]
		v.Tools = s.Tools
		if cfg.On("changes") {
			v.Edited = s.Edited
		}
		if len(v.Tools) > 5 {
			v.Tools = v.Tools[:5]
		}
		v.Window = cfg.ContextWindow
		if v.Window == 0 {
			v.Window = stats.ContextWindow(path)
		}
		if v.Window == 0 || s.MaxContext > v.Window {
			v.Window = 200_000
			if s.MaxContext > 200_000 {
				v.Window = 1_000_000
			}
		}
		seen := map[string]bool{}
		for _, name := range stats.ConfiguredMCP(path) {
			seen[name] = true
			v.MCP = append(v.MCP, MCPView{name, s.MCPUsed[name]})
		}
		for name, n := range s.MCPUsed {
			if !seen[name] {
				v.MCP = append(v.MCP, MCPView{name, n})
			}
		}
	} else {
		for _, name := range stats.ConfiguredMCP(path) {
			v.MCP = append(v.MCP, MCPView{Name: name})
		}
	}
	v.Git = a.gitNow(path)

	d := a.day()
	v.Today, v.TodayCount = d.Usage, d.Sessions
	return v
}

// day is today's use across every project. It scans every project's logs,
// so it's refreshed at most every 30 s, in the background: the first call
// (and one after a quiet spell) gets the last figures.
func (a *App) day() stats.Day {
	a.today.mu.Lock()
	defer a.today.mu.Unlock()
	if time.Since(a.today.at) > 30*time.Second && !a.today.busy {
		a.today.busy = true
		go func() {
			d := a.reader.Today(projects.ClaudeRoot())
			a.today.mu.Lock()
			a.today.day, a.today.at, a.today.busy = d, time.Now(), false
			a.today.mu.Unlock()
		}()
	}
	return a.today.day
}

// TodayView is today's Claude Code use across every project, for the home
// page.
type TodayView struct {
	Usage    stats.Usage `json:"usage"`
	Sessions int         `json:"sessions"`
	Activity []int       `json:"activity"` // tokens per 5 min, the last 2 h, oldest first
}

// Today is today's use across every project.
func (a *App) Today() TodayView {
	d := a.day()
	return TodayView{Usage: d.Usage, Sessions: d.Sessions, Activity: d.Activity[:]}
}

// Confirm asks a yes/no question in a native dialog.
func (a *App) Confirm(title, message, yes string) bool {
	if a.ctx == nil {
		return false
	}
	answer, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         title,
		Message:       message,
		Buttons:       []string{yes, "Cancel"},
		DefaultButton: yes,
		CancelButton:  "Cancel",
	})
	return err == nil && answer == yes
}

// Account returns the Claude plan usage, fetched at most once a minute.
// AgentAccount is another coding agent's plan and quotas, for the agents
// Quarters can read them for (only Antigravity, from its running IDE).
func (a *App) AgentAccount(id string) stats.Account {
	if id != "antigravity" {
		return stats.Account{Limits: []stats.Limit{}}
	}
	c := &a.agentAcct
	c.mu.Lock()
	defer c.mu.Unlock()
	// a closed IDE is checked for again soon, so opening it shows up quickly
	if time.Since(c.at) > time.Minute || c.val.Error != "" && time.Since(c.at) > 10*time.Second {
		c.val = stats.FetchAntigravity(a.bg())
		c.at = time.Now()
	}
	return c.val
}

func (a *App) Account() stats.Account {
	a.account.mu.Lock()
	defer a.account.mu.Unlock()
	if time.Since(a.account.at) > time.Minute || a.account.val.Error != "" && time.Since(a.account.at) > 15*time.Second {
		next := stats.FetchAccount(a.bg())
		// Keep the last good numbers through a brief network hiccup.
		if next.Error == "" || len(a.account.val.Limits) == 0 || !strings.HasPrefix(next.Error, "Usage unavailable") {
			a.account.val = next
		}
		a.account.at = time.Now()
	}
	return a.account.val
}
