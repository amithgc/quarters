package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/amithgc/quarters/internal/remote"
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
		if m.Kind == "shell" || m.AutoKey != "" || a.viewed(info.ID) {
			continue
		}
		name := filepath.Base(m.Path)
		if p, ok := a.project(m.Path); ok && p.Name != "" {
			name = p.Name
		}
		body := m.Title
		if body == "" {
			body = "Claude Code is waiting for you"
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

// keepAwake runs caffeinate while Claude is working or a browser is connected.
func (a *App) keepAwake() {
	want := a.config().On("keep_awake") && (a.sessions.Busy(2*time.Minute) || a.browsersOnline())
	a.fmu.Lock()
	defer a.fmu.Unlock()
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

// ---- usage-limit warnings ----

func (a *App) usageAlerts() {
	cfg := a.config()
	if !cfg.On("usage_alerts") {
		return
	}
	acct := a.Account()
	at := float64(cfg.AlertAt())
	for _, l := range acct.Limits {
		key := fmt.Sprintf("%s|%d", l.Label, l.ResetsAt)
		a.fmu.Lock()
		seen := a.f.alerted[key]
		if l.Percent >= at {
			a.f.alerted[key] = true
		}
		a.fmu.Unlock()
		if l.Percent < at || seen {
			continue
		}
		title := fmt.Sprintf("Claude usage at %d%%", int(l.Percent))
		body := l.Label
		if l.ResetsAt > 0 {
			body += " · resets " + time.UnixMilli(l.ResetsAt).Format("Mon 15:04")
		}
		a.Notify(title, body, "")
		if srv := a.remoteServer(); srv != nil && cfg.On("web_push") {
			srv.Push(remote.PushMessage{Title: title, Body: body})
		}
		a.emit("flash", title+" — "+body)
	}
}
