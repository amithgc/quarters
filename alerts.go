package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/remote"
	"github.com/amithgc/quarters/internal/stats"
)

// ---- "Claude is waiting": phone alerts ----

// SetViewing tells Quarters which session the Mac window shows (0: none, or
// the window is not in front), so phones are not buzzed about it.
func (a *App) SetViewing(id int) { a.setViewing(macClient, id) }

func (a *App) setViewing(client string, id int) {
	a.fmu.Lock()
	defer a.fmu.Unlock()
	if id == 0 {
		delete(a.f.viewing, client)
	} else {
		a.f.viewing[client] = viewing{id, time.Now()}
	}
}

// viewed reports whether a window or browser is showing session id now.
func (a *App) viewed(id int) bool {
	a.fmu.Lock()
	defer a.fmu.Unlock()
	for c, v := range a.f.viewing {
		if time.Since(v.at) > 70*time.Second { // windows say it again every 30 s
			delete(a.f.viewing, c)
			continue
		}
		if v.id == id {
			return true
		}
	}
	return false
}

// attention sends a phone alert for each Claude session that just finished
// a stretch of work, unless someone is looking at it.
func (a *App) attention() {
	done := a.sessions.Finished(4 * time.Second)
	if len(done) == 0 {
		return
	}
	cfg := a.config()
	srv := a.remoteServer()
	if srv == nil || !cfg.On("web_push") {
		return
	}
	for _, info := range done {
		m := metaOf(info)
		// with prompts queued it isn't your turn: the next one goes in
		if m.Kind == "shell" || m.AutoKey != "" || a.viewed(info.ID) || a.queued(info.ID) {
			continue
		}
		name := filepath.Base(m.Path)
		if p, ok := a.project(m.Path); ok && p.Name != "" {
			name = p.Name
		}
		body := m.Title
		if body == "" {
			body = config.HarnessByID(m.Harness).Name + " is waiting for you"
		}
		srv.Push(remote.PushMessage{Title: name + " · your turn", Body: body, Key: m.Key})
	}
}

// pushKey is the public key a browser subscribes to phone alerts with.
func (a *App) pushKey() (string, error) {
	srv := a.remoteServer()
	if srv == nil {
		return "", fmt.Errorf("remote access is off")
	}
	return srv.PushKey()
}

// ---- keep the Mac awake ----

// keepAwake runs caffeinate while Claude is working or a browser is
// connected, and while a hold from the top bar lasts.
func (a *App) keepAwake() {
	want := a.config().On("keep_awake") && (a.sessions.Busy(2*time.Minute) || a.browsersOnline())
	a.fmu.Lock()
	defer a.fmu.Unlock()
	if a.f.awakeHold > 0 && time.Now().UnixMilli() >= a.f.awakeHold {
		a.f.awakeHold = 0
	}
	want = want || a.f.awakeHold != 0
	switch {
	case want && a.f.awake == nil:
		cmd := awakeCmd(os.Getpid())
		if cmd != nil && cmd.Start() == nil {
			a.f.awake = cmd
			go cmd.Wait()
		}
	case !want && a.f.awake != nil:
		_ = a.f.awake.Process.Kill()
		a.f.awake = nil
	}
}

// AwakeView is the top bar's keep-awake button.
type AwakeView struct {
	On    bool  `json:"on"`    // keeping the machine awake right now, for whatever reason
	Until int64 `json:"until"` // the hold: when it ends (unix ms), -1 until stopped, 0 none
	Auto  bool  `json:"auto"`  // the keep_awake setting: awake while agents work or a browser is connected
	Can   bool  `json:"can"`   // this machine can be kept awake (Linux needs systemd)
}

// Awake says whether the machine is kept awake, and why.
func (a *App) Awake() AwakeView {
	v := AwakeView{Auto: a.config().On("keep_awake"), Can: awakeCmd(os.Getpid()) != nil}
	a.fmu.Lock()
	defer a.fmu.Unlock()
	v.On, v.Until = a.f.awake != nil, a.f.awakeHold
	return v
}

// SetAwake keeps the machine awake for this many minutes whatever it's
// doing, like Amphetamine: -1 until it's stopped, 0 ends the hold.
func (a *App) SetAwake(minutes int) AwakeView {
	a.fmu.Lock()
	switch {
	case minutes < 0:
		a.f.awakeHold = -1
	case minutes == 0:
		a.f.awakeHold = 0
	default:
		a.f.awakeHold = time.Now().Add(time.Duration(minutes) * time.Minute).UnixMilli()
	}
	a.fmu.Unlock()
	a.keepAwake()
	return a.Awake()
}

func (a *App) stopAwake() {
	a.fmu.Lock()
	defer a.fmu.Unlock()
	if a.f.awake != nil {
		_ = a.f.awake.Process.Kill()
		a.f.awake = nil
	}
}

func (a *App) browsersOnline() bool {
	srv := a.remoteServer()
	if srv == nil {
		return false
	}
	for _, d := range srv.Devices() {
		if d.Online > 0 {
			return true
		}
	}
	return false
}

// ---- plan limits: warnings, "runs out soon" and resets ----

// limitWatch is what was last seen of a plan limit, and what was said
// about its current window.
type limitWatch struct {
	resetsAt int64
	percent  float64
	levels   map[int]bool // warning levels already announced
	pace     bool         // "runs out soon" already announced
}

// planNote is one notification about the plan.
type planNote struct{ Title, Body string }

// soonEnough is how close running out must be for a "runs out soon" alert.
const soonEnough = 30 * time.Minute

// usageLevels are the warning levels: the one set in Settings, then 90% and
// 100%.
func usageLevels(at int) []int {
	out := []int{at}
	for _, l := range []int{90, 100} {
		if l > at {
			out = append(out, l)
		}
	}
	return out
}

// planAlerts updates watch with a new reading of the limits and returns
// what to announce: a warning level passed (the highest, once per window),
// running out within half an hour at your pace, and a limit that reset
// after it was announced.
func planAlerts(limits []stats.Limit, watch map[string]*limitWatch, at int, now time.Time) []planNote {
	var out []planNote
	resets := func(l stats.Limit) string {
		if l.ResetsAt <= 0 {
			return ""
		}
		return " · resets " + time.UnixMilli(l.ResetsAt).Format("Mon 15:04")
	}
	for _, l := range limits {
		w := watch[l.Label]
		moved := w != nil && (l.ResetsAt-w.resetsAt > time.Minute.Milliseconds() || w.resetsAt-l.ResetsAt > time.Minute.Milliseconds())
		if w != nil && moved && l.Percent < w.percent {
			if len(w.levels) > 0 || w.pace {
				out = append(out, planNote{"Claude " + strings.ToLower(l.Label) + " reset",
					fmt.Sprintf("Back to %d%%%s", int(l.Percent), resets(l))})
			}
			w = nil
		}
		if w == nil {
			w = &limitWatch{levels: map[int]bool{}}
			watch[l.Label] = w
		}
		w.resetsAt, w.percent = l.ResetsAt, l.Percent
		top := 0
		for _, lv := range usageLevels(at) {
			if l.Percent >= float64(lv) && !w.levels[lv] {
				top = lv
			}
		}
		if top > 0 {
			for _, lv := range usageLevels(at) {
				if lv <= top {
					w.levels[lv] = true
				}
			}
			title := fmt.Sprintf("Claude usage at %d%%", int(l.Percent))
			if top >= 100 {
				title = "Claude plan limit reached"
			}
			out = append(out, planNote{title, l.Label + resets(l)})
			continue
		}
		if !w.pace && l.RunsOutAt > 0 && l.Percent < 100 && time.UnixMilli(l.RunsOutAt).Sub(now) <= soonEnough {
			w.pace = true
			out = append(out, planNote{"Claude plan runs out in ~" + fmtLeft(time.UnixMilli(l.RunsOutAt).Sub(now)),
				fmt.Sprintf("%s at %d%% at your current pace%s", l.Label, int(l.Percent), resets(l))})
		}
	}
	return out
}

// fmtLeft is a duration as "25 min" or "1h 10m".
func fmtLeft(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	if m < 1 {
		m = 1
	}
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%dh %dm", m/60, m%60)
}

// planCheck reads the plan's limits (which also keeps the pace and the menu
// bar up to date) and sends the alerts planAlerts finds.
func (a *App) planCheck() {
	cfg := a.config()
	if !cfg.On("usage_alerts") && !cfg.On("menu_bar") {
		return
	}
	acct := a.Account()
	a.updateMenuBar()
	a.fmu.Lock()
	notes := planAlerts(acct.Limits, a.f.limits, cfg.AlertAt(), time.Now())
	a.fmu.Unlock()
	if !cfg.On("usage_alerts") {
		return
	}
	for _, n := range notes {
		a.Notify(n.Title, n.Body, "")
		if srv := a.remoteServer(); srv != nil && cfg.On("web_push") {
			srv.Push(remote.PushMessage{Title: n.Title, Body: n.Body})
		}
		a.emit("flash", n.Title+" — "+n.Body)
	}
}
