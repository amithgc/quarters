package main

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/projects"
	"github.com/amithgc/quarters/internal/stats"
)

// ---- usage by project: which projects use your tokens ----

// usageCache keeps the last tally; the logs are read again at most once
// a minute (only what was added since).
type usageCache struct {
	mu   sync.Mutex
	scan *stats.UsageScanner
	at   time.Time
	val  UsageView
}

// ProjectUsage is one project's tokens.
type ProjectUsage struct {
	Path  string `json:"path"` // "" for the folders that aren't projects
	Name  string `json:"name"`
	Today int    `json:"today"`
	Week  int    `json:"week"`  // the last 7 days, today included
	Month int    `json:"month"` // the last 30
	Daily []int  `json:"daily"` // per day, oldest first, ending today
}

// UsageView is the usage of every project, busiest this week first.
type UsageView struct {
	Days     []string       `json:"days"` // the days Daily counts, oldest first
	Projects []ProjectUsage `json:"projects"`
	Other    ProjectUsage   `json:"other"` // Claude Code used in folders that aren't projects
	Total    ProjectUsage   `json:"total"`
	At       int64          `json:"at"`
}

// Usage tallies Claude Code's token use per project over the last 30
// days. A project's worktrees count as the project.
func (a *App) Usage() UsageView {
	c := &a.usage
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) < time.Minute {
		return c.val
	}
	if c.scan == nil {
		c.scan = stats.NewUsageScanner()
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day()-(stats.UsageDays-1), 0, 0, 0, 0, now.Location())
	byDir := c.scan.Scan(projects.ClaudeRoot(), start)
	c.val = usageView(a.config(), byDir, now)
	c.at = now
	return c.val
}

// usageView adds up the folders' tallies per project.
func usageView(cfg *config.Config, byDir map[string]stats.DayTokens, now time.Time) UsageView {
	v := UsageView{At: now.UnixMilli(), Projects: []ProjectUsage{}}
	for i := stats.UsageDays - 1; i >= 0; i-- {
		v.Days = append(v.Days, time.Date(now.Year(), now.Month(), now.Day()-i, 0, 0, 0, 0, now.Location()).Format("2006-01-02"))
	}
	blank := func(path, name string) ProjectUsage {
		return ProjectUsage{Path: path, Name: name, Daily: make([]int, len(v.Days))}
	}
	type owner struct {
		key string
		i   int
	}
	var keys []owner
	for _, p := range cfg.Projects {
		path := config.ExpandPath(p.Path)
		if strings.TrimSpace(p.Path) == "" {
			continue
		}
		name := p.Name
		if name == "" {
			name = filepath.Base(path) // as the project list names it
		}
		keys = append(keys, owner{projects.EncodeDir(path), len(v.Projects)})
		v.Projects = append(v.Projects, blank(path, name))
	}
	v.Other, v.Total = blank("", "Other folders"), blank("", "All")
	for dir, days := range byDir {
		into := &v.Other
		best := -1
		for _, k := range keys {
			if (dir == k.key || strings.HasPrefix(dir, k.key+"-worktrees-")) && len(k.key) > best {
				into, best = &v.Projects[k.i], len(k.key)
			}
		}
		for i, day := range v.Days {
			n := days[day]
			into.Daily[i] += n
			v.Total.Daily[i] += n
		}
	}
	sum := func(u *ProjectUsage) {
		n := len(u.Daily)
		u.Today = u.Daily[n-1]
		for i, t := range u.Daily {
			u.Month += t
			if i >= n-7 {
				u.Week += t
			}
		}
	}
	for i := range v.Projects {
		sum(&v.Projects[i])
	}
	sum(&v.Other)
	sum(&v.Total)
	slices.SortStableFunc(v.Projects, func(x, y ProjectUsage) int {
		if x.Week != y.Week {
			return y.Week - x.Week
		}
		return y.Month - x.Month
	})
	return v
}
