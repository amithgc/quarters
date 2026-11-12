package main

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/projects"
	"github.com/amithgc/quarters/internal/remote"
	"github.com/amithgc/quarters/internal/stats"
)

// ---- secrets that ended up in a Claude conversation ----

// keepSecrets is how many found secrets Quarters lists at most.
const keepSecrets = 50

// secretHit is a secret found in a log, with the project it belongs to.
type secretHit struct {
	stats.SecretHit
	Project string // the project's path ("" when it isn't one)
	Name    string // the project's name, or the folder's
}

// SecretView is a possible secret in a project's conversations, for the
// side panel.
type SecretView struct {
	Kind   string `json:"kind"`
	Masked string `json:"masked"`
	Where  string `json:"where"`
	At     int64  `json:"at"` // when it was found (unix ms)
}

// scanSecrets reads what Claude Code added to its logs since the last scan
// and warns about each new key or password in it. Turning the feature off
// forgets where the logs were read up to, so turning it on again only
// looks at what is written from then on.
func (a *App) scanSecrets() {
	cfg := a.config()
	a.fmu.Lock()
	if !cfg.On("secret_scan") {
		a.f.secretScan = nil
		a.fmu.Unlock()
		return
	}
	if a.f.scanning {
		a.fmu.Unlock()
		return
	}
	a.f.scanning = true
	if a.f.secretScan == nil {
		a.f.secretScan = stats.NewSecretScanner()
	}
	sc := a.f.secretScan
	a.fmu.Unlock()

	hits := sc.Scan(projects.ClaudeRoot(), time.Now())

	a.fmu.Lock()
	a.f.scanning = false
	var found []secretHit
	for _, h := range hits {
		found = append(found, a.secretOwner(cfg, h))
	}
	a.f.secrets = append(a.f.secrets, found...)
	if n := len(a.f.secrets); n > keepSecrets {
		a.f.secrets = a.f.secrets[n-keepSecrets:]
	}
	a.fmu.Unlock()

	for _, h := range found {
		title := "Possible secret in a Claude conversation"
		body := h.Name + ": " + h.Kind + " " + h.Masked + " in " + h.Where + ". If it's real, rotate it."
		a.Notify(title, body, "")
		if srv := a.remoteServer(); srv != nil && cfg.On("web_push") {
			srv.Push(remote.PushMessage{Title: title, Body: body})
		}
		a.emit("flash", "⚠ "+h.Kind+" "+h.Masked+" in a Claude conversation ("+h.Name+")")
	}
	if len(found) > 0 {
		a.emit("projects", nil) // side panels show it
	}
}

// secretOwner finds the project a found secret's conversation belongs to:
// the one its folder is in, or whose Claude Code log folder it's in.
func (a *App) secretOwner(cfg *config.Config, h stats.SecretHit) secretHit {
	out := secretHit{SecretHit: h, Name: filepath.Base(h.Cwd)}
	logDir := filepath.Base(filepath.Dir(h.File))
	best := ""
	for _, p := range cfg.Projects {
		path := config.ExpandPath(p.Path)
		inside := h.Cwd != "" && (h.Cwd == path || strings.HasPrefix(h.Cwd, path+string(filepath.Separator)))
		if (inside && len(path) > len(best)) || (best == "" && projects.EncodeDir(path) == logDir) {
			best = path
			out.Project = path
			out.Name = p.Name
			if out.Name == "" {
				out.Name = filepath.Base(path)
			}
		}
	}
	if out.Name == "" || out.Name == "." {
		out.Name = "a Claude conversation"
	}
	return out
}

// secretsOf lists the possible secrets found in a project's conversations,
// newest first.
func (a *App) secretsOf(path string) []SecretView {
	a.fmu.Lock()
	defer a.fmu.Unlock()
	var out []SecretView
	for i := len(a.f.secrets) - 1; i >= 0; i-- {
		h := a.f.secrets[i]
		if h.Project == path {
			out = append(out, SecretView{h.Kind, h.Masked, h.Where, h.At.UnixMilli()})
		}
	}
	return out
}

// DismissSecrets forgets the secrets found in a project's conversations
// (they were rotated, or aren't real).
func (a *App) DismissSecrets(path string) {
	a.fmu.Lock()
	kept := a.f.secrets[:0]
	for _, h := range a.f.secrets {
		if h.Project != path {
			kept = append(kept, h)
		}
	}
	a.f.secrets = kept
	a.fmu.Unlock()
	a.emit("projects", nil)
}
