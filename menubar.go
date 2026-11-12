package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/stats"
)

// ---- the menu bar: plan use and who's waiting, from any app ----

// WaitingItem is an agent tab waiting for you, as the Mac window sees it.
type WaitingItem struct {
	Key        string `json:"key"` // the tab (see tabKey in app.js)
	Name       string `json:"name"`
	DoneAt     int64  `json:"doneAt"`     // when it finished (unix ms)
	Asks       bool   `json:"asks"`       // it asks you something
	CacheUntil int64  `json:"cacheUntil"` // when its prompt cache runs out (unix ms, 0: unknown)
}

// menuBarHandler handles a click in the menu bar's menu (see
// menubar_export_darwin.go).
var menuBarHandler atomic.Pointer[func(int)]

// SetWaiting tells Quarters which tabs wait for you, for the menu bar. Only
// the Mac window calls it.
func (a *App) SetWaiting(list []WaitingItem) {
	a.fmu.Lock()
	a.f.waiting = list
	a.fmu.Unlock()
	a.updateMenuBar()
}

// cachedAccount is the plan's limits as last fetched, without fetching.
func (a *App) cachedAccount() stats.Account {
	a.account.mu.Lock()
	defer a.account.mu.Unlock()
	return a.account.val
}

// updateMenuBar redraws the menu bar item when what it shows changed.
func (a *App) updateMenuBar() {
	if a.ctx == nil {
		return // no window: nothing to show it in
	}
	on := a.config().On("menu_bar")
	a.fmu.Lock()
	var title, items string
	var keys []string
	if on {
		title, items, keys = menuBarContent(a.cachedAccount(), a.f.waiting, time.Now())
	}
	shown := fmt.Sprint(on, "\x00", title, "\x00", items)
	same := shown == a.f.menuShown
	a.f.menuShown, a.f.menuKeys = shown, keys
	a.fmu.Unlock()
	if same {
		return
	}
	if h := menuBarHandler.Load(); h == nil {
		click := a.menuBarClick
		menuBarHandler.Store(&click)
	}
	setMenuBar(on, title, items)
}

// menuBarClick opens Quarters (tag 0) or the waiting tab with that number.
func (a *App) menuBarClick(tag int) {
	a.fmu.Lock()
	key := ""
	if tag > 0 && tag <= len(a.f.menuKeys) {
		key = a.f.menuKeys[tag-1]
	}
	a.fmu.Unlock()
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	activateApp()
	if key != "" {
		runtime.EventsEmit(a.ctx, "notify:open", key)
	}
}

// menuBarContent is what the menu bar shows: its title ("◐2  42% ~45m":
// tabs waiting, the plan's fullest limit and when it runs out at this pace)
// and its menu, one item per line ("tag\ttext"; see setMenuBar). keys[i] is
// the tab item tag i+1 opens.
func menuBarContent(acct stats.Account, waiting []WaitingItem, now time.Time) (title, items string, keys []string) {
	clean := func(s string) string { return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s) }
	var t, m []string
	if len(waiting) > 0 {
		t = append(t, fmt.Sprintf("◐%d", len(waiting)))
	}
	var top *stats.Limit
	var soon int64
	for i, l := range acct.Limits {
		if top == nil || l.Percent > top.Percent {
			top = &acct.Limits[i]
		}
		if l.RunsOutAt > 0 && (soon == 0 || l.RunsOutAt < soon) {
			soon = l.RunsOutAt
		}
	}
	if top != nil {
		s := fmt.Sprintf("%d%%", int(top.Percent))
		if soon > 0 {
			s += " ~" + fmtShort(time.UnixMilli(soon).Sub(now))
		}
		t = append(t, s)
	}

	switch {
	case len(acct.Limits) > 0:
		head := "Claude plan"
		if acct.Plan != "" {
			head += " · " + acct.Plan
		}
		m = append(m, "-1\t"+clean(head))
		for _, l := range acct.Limits {
			line := fmt.Sprintf("%s — %d%%", l.Label, int(l.Percent))
			if l.RunsOutAt > 0 {
				line += " · runs out in ~" + fmtLeft(time.UnixMilli(l.RunsOutAt).Sub(now)) + " at this pace"
			} else if r := resetsIn(l.ResetsAt, now); r != "" {
				line += " · " + r
			}
			m = append(m, "-1\t"+clean(line))
		}
		m = append(m, "-")
	case acct.Error != "":
		m = append(m, "-1\t"+clean("Claude plan: "+acct.Error), "-")
	}

	if len(waiting) == 0 {
		m = append(m, "-1\tNobody is waiting for you")
	} else {
		m = append(m, "-1\tWaiting for you")
	}
	for _, w := range waiting {
		line := w.Name + " — " + map[bool]string{true: "asks you", false: "your turn"}[w.Asks]
		if ago := now.Sub(time.UnixMilli(w.DoneAt)); w.DoneAt > 0 && ago >= time.Minute {
			line += " · " + fmtLeft(ago)
		}
		if w.CacheUntil > 0 {
			if left := time.UnixMilli(w.CacheUntil).Sub(now); left > 0 {
				line += " · cache " + fmtLeft(left) + " left"
			} else {
				line += " · cache expired"
			}
		}
		keys = append(keys, w.Key)
		m = append(m, fmt.Sprintf("%d\t%s", len(keys), clean(line)))
	}
	m = append(m, "-", "0\tOpen Quarters")
	return strings.Join(t, "  "), strings.Join(m, "\n"), keys
}

// fmtShort is a duration as "25m" or "1h10m", for the menu bar's title.
func fmtShort(d time.Duration) string {
	m := max(1, int(d.Round(time.Minute).Minutes()))
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh%02dm", m/60, m%60)
}

// resetsIn is when a limit resets: "resets in 2h 10m" within a day, else
// "resets Fri 10:00".
func resetsIn(ms int64, now time.Time) string {
	if ms <= 0 {
		return ""
	}
	at := time.UnixMilli(ms)
	if d := at.Sub(now); d < 24*time.Hour {
		if d <= 0 {
			return "resetting"
		}
		return "resets in " + fmtLeft(d)
	}
	return "resets " + at.Format("Mon 15:04")
}

// ---- session health: prompt cache and compactions, per agent tab ----

// TabHealth is what a Claude tab's conversation log says about its health.
type TabHealth struct {
	ID          int   `json:"id"`
	CacheUntil  int64 `json:"cacheUntil"` // when its prompt cache runs out (unix ms, 0: unknown)
	CacheTTL    int   `json:"cacheTTL"`   // the cache's lifetime in seconds (300 or 3600)
	Context     int   `json:"context"`
	Compactions int   `json:"compactions"`
}

// Health reports, for each running Claude tab, when its prompt cache runs
// out and how often it was compacted.
func (a *App) Health() []TabHealth {
	out := []TabHealth{}
	if a.sessions == nil || !a.config().On("cache_timer") {
		return out
	}
	used := map[string]bool{}
	for _, info := range a.sessions.List() {
		m := metaOf(info)
		if info.Exited || m.Kind == "shell" || m.AutoKey != "" || (m.Harness != "" && m.Harness != config.Claude) {
			continue
		}
		conv := a.conversationOf(info.ID, m, used)
		if conv == "" {
			continue
		}
		used[conv] = true
		s, ok := a.reader.SessionFile(filepath.Join(projectDir(orDefault(m.Cwd, m.Path)), conv+".jsonl"))
		if !ok {
			continue
		}
		h := TabHealth{ID: info.ID, Context: s.Context, Compactions: s.Compactions, CacheTTL: int(s.CacheTTL.Seconds())}
		if u := s.CacheUntil(); !u.IsZero() {
			h.CacheUntil = u.UnixMilli()
		}
		out = append(out, h)
	}
	return out
}
