package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ---- today's work: a summary from commits and agent conversations ----

// DayProject is what happened in one project today.
type DayProject struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Commits     []string `json:"commits"` // your commits today, newest first
	Chats       []string `json:"chats"`   // agent conversations today: their titles
	Prompts     int      `json:"prompts"` // prompts in them
	Uncommitted int      `json:"uncommitted"`
}

// DaySummary is today's work across every project, and the same as
// Markdown, to paste into a standup or a timesheet.
type DaySummary struct {
	Projects []DayProject `json:"projects"`
	Markdown string       `json:"markdown"`
}

var daySummary = struct {
	sync.Mutex
	at time.Time
	v  DaySummary
}{}

// DailySummary is today's work: your commits, the agent conversations and
// what isn't committed yet, per project touched today. Worked out at most
// every 30 s.
func (a *App) DailySummary() DaySummary {
	daySummary.Lock()
	defer daySummary.Unlock()
	if time.Since(daySummary.at) < 30*time.Second {
		return daySummary.v
	}
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var list []DayProject
	for _, p := range a.Projects() {
		if st, err := os.Stat(p.Path); err != nil || !st.IsDir() {
			continue
		}
		d := DayProject{Name: p.Name, Path: p.Path, Commits: []string{}, Chats: []string{}}
		if email, err := gitRun(ctx, p.Path, "config", "user.email"); err == nil && email != "" {
			out, err := gitRun(ctx, p.Path, "log", "--no-merges", "--since="+midnight.Format(time.RFC3339), "--author="+email, "--format=%s")
			if err == nil && out != "" {
				d.Commits = strings.Split(out, "\n")
			}
		}
		for _, c := range a.reader.TodayChats(projectDir(p.Path)) {
			d.Prompts += c.Prompts
			if c.Title != "" && len(d.Chats) < 8 {
				d.Chats = append(d.Chats, c.Title)
			}
		}
		if len(d.Commits) > 0 || d.Prompts > 0 {
			if g, ok := a.gitOf(p.Path, gitViewAge); ok {
				d.Uncommitted = len(g.Files)
			}
			list = append(list, d)
		}
	}
	v := DaySummary{Projects: list, Markdown: dayMarkdown(now, list)}
	daySummary.at, daySummary.v = time.Now(), v
	return v
}

func dayMarkdown(now time.Time, list []DayProject) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", now.Format("Monday 2 January"))
	if len(list) == 0 {
		b.WriteString("\nNothing yet.\n")
	}
	for _, d := range list {
		fmt.Fprintf(&b, "\n**%s**\n", d.Name)
		for _, c := range d.Commits {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		if d.Prompts > 0 {
			chats := ""
			if len(d.Chats) > 0 {
				chats = ": " + strings.Join(d.Chats, "; ")
			}
			fmt.Fprintf(&b, "- Worked with the agent (%d prompt%s)%s\n", d.Prompts, plural(d.Prompts), chats)
		}
		if d.Uncommitted > 0 {
			fmt.Fprintf(&b, "- In progress: %d file%s not committed yet\n", d.Uncommitted, plural(d.Uncommitted))
		}
	}
	return b.String()
}
