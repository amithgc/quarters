package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/projects"
)

// ---- scheduled automations ----

var days = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func cleanSchedule(s config.Schedule) (*config.Schedule, error) {
	s.At = strings.TrimSpace(s.At)
	if s.At == "" {
		return nil, nil
	}
	t, err := time.Parse("15:04", s.At)
	if err != nil {
		return nil, fmt.Errorf("the time must look like 09:00")
	}
	s.At = t.Format("15:04")
	var ds []string
	for _, d := range days { // keep the week's order
		if slices.Contains(s.Days, d) {
			ds = append(ds, d)
		}
	}
	if len(ds) == 7 {
		ds = nil
	}
	s.Days = ds
	return &s, nil
}

// runSchedules starts the automations whose time has come (within 10
// minutes, once a day) unless they are running already.
func (a *App) runSchedules(now time.Time) {
	cfg := a.config()
	if !cfg.On("schedules") {
		return
	}
	today := now.Format("2006-01-02")
	for _, p := range projects.Load(cfg) {
		if p.Missing {
			continue
		}
		for _, au := range automationViews(p) {
			sc := au.Schedule
			if sc == nil || sc.At == "" || len(sc.Days) > 0 && !slices.Contains(sc.Days, days[now.Weekday()]) {
				continue
			}
			t, err := time.ParseInLocation("15:04", sc.At, now.Location())
			if err != nil {
				continue
			}
			at := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
			if now.Before(at) || now.Sub(at) > 10*time.Minute {
				continue
			}
			key := p.Path + "::" + au.Name
			a.fmu.Lock()
			done := a.f.schedRun[key] == today
			a.f.schedRun[key] = today
			a.fmu.Unlock()
			if done || a.automationRunning(key) {
				continue
			}
			if err := a.runAutomation(p.Path, au); err != nil {
				a.Notify("Quarters: "+au.Name+" didn't start", err.Error(), "")
				continue
			}
			a.Notify(fmt.Sprintf("▶ %s · %s", p.Name, au.Name), "Started on schedule ("+sc.At+")", "")
		}
	}
}

func (a *App) automationRunning(key string) bool {
	for _, info := range a.sessions.List() {
		if !info.Exited && metaOf(info).AutoKey == key {
			return true
		}
	}
	return false
}

// The terminal size of an automation started by a schedule, until a window
// shows it and gives it that window's size.
const autoCols, autoRows = 120, 32

// runAutomation opens an automation's terminals from here; every window
// shows them (as if a window had run it).
func (a *App) runAutomation(path string, au AutomationView) error {
	group := map[string]any{"id": newUUID(), "name": au.Name, "autoName": au.Name, "path": path, "layout": au.Layout, "cols": au.Cols, "rows": au.Rows}
	key := path + "::" + au.Name
	for _, t := range au.Tabs {
		meta := map[string]any{"key": newUUID(), "started": time.Now().UnixMilli(), "path": path, "kind": "shell",
			"title": t.Name, "dir": t.Dir, "command": t.Command, "autoKey": key, "group": group}
		// no window started it, so none owns its size yet: the first one to
		// show it gives it its size
		if _, err := a.startCommandSession("", t.Dir, t.Command, autoCols, autoRows, meta); err != nil {
			return err
		}
	}
	return nil
}
