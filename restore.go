package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/projects"
)

// ---- resume sessions after a restart ----

// RestoreItem is a Claude conversation or automation that was open.
type RestoreItem struct {
	Path  string `json:"path"`
	Name  string `json:"name"` // project name
	Kind  string `json:"kind"` // "claude" or "auto"
	Conv  string `json:"conv"` // conversation id ("" = the latest)
	Cwd   string `json:"cwd"`  // a worktree, or ""
	Title string `json:"title"`
	Auto  string `json:"auto"` // automation name
}

func restorePath() string { return filepath.Join(filepath.Dir(config.Path()), "restore.json") }

// openItems lists what is open now, worth reopening after a restart.
func (a *App) openItems() []RestoreItem {
	var out []RestoreItem
	seenAuto := map[string]bool{}
	used := map[string]bool{}
	for _, info := range a.sessions.List() {
		if info.Exited {
			continue
		}
		m := metaOf(info)
		p, ok := a.project(m.Path)
		if !ok {
			continue
		}
		name := p.Name
		if name == "" {
			name = filepath.Base(m.Path)
		}
		switch {
		case m.AutoKey != "" && m.Group != nil:
			if !seenAuto[m.AutoKey] {
				seenAuto[m.AutoKey] = true
				out = append(out, RestoreItem{Path: m.Path, Name: name, Kind: "auto", Auto: m.Group.AutoName, Title: m.Group.AutoName})
			}
		case m.Kind != "shell" && m.AutoKey == "":
			a.fmu.Lock()
			conv := a.convs[info.ID]
			a.fmu.Unlock()
			if conv == "" { // "continue": the log it writes to
				conv = latestConversation(projectDir(orDefault(m.Cwd, m.Path)), m.Started, used)
			}
			used[conv] = true
			out = append(out, RestoreItem{Path: m.Path, Name: name, Kind: "claude", Conv: conv, Cwd: m.Cwd, Title: m.Title})
		}
	}
	return out
}

func projectDir(path string) string {
	return filepath.Join(projects.ClaudeRoot(), projects.EncodeDir(path))
}

// latestConversation is the newest conversation in dir written since
// startedMs and not in used.
func latestConversation(dir string, startedMs int64, used map[string]bool) string {
	entries, _ := os.ReadDir(dir)
	best, bestT := "", time.UnixMilli(startedMs)
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if !strings.HasSuffix(e.Name(), ".jsonl") || used[id] || !uuidRe.MatchString(id) {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(bestT) {
			best, bestT = id, info.ModTime()
		}
	}
	return best
}

// saveRestore writes what is open, so it can be reopened next time.
func (a *App) saveRestore() {
	if a.sessions == nil || !a.config().On("restore") {
		return
	}
	items := a.openItems()
	a.fmu.Lock()
	pending := len(a.f.restore) > 0
	a.fmu.Unlock()
	if len(items) == 0 && pending {
		return // keep last time's list until it is used or dismissed
	}
	if len(items) == 0 {
		_ = os.Remove(restorePath())
		return
	}
	data, _ := json.MarshalIndent(items, "", "  ")
	tmp := restorePath() + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, restorePath())
	}
}

func (a *App) loadRestore() {
	if !a.config().On("restore") {
		return
	}
	data, err := os.ReadFile(restorePath())
	if err != nil {
		return
	}
	var items []RestoreItem
	if json.Unmarshal(data, &items) != nil {
		return
	}
	var ok []RestoreItem
	for _, it := range items {
		if _, known := a.project(it.Path); known && (it.Conv == "" || uuidRe.MatchString(it.Conv)) {
			ok = append(ok, it)
		}
	}
	a.fmu.Lock()
	a.f.restore = ok
	a.fmu.Unlock()
}

// RestoreInfo lists the sessions open when Quarters last quit (until they
// are reopened or dismissed).
func (a *App) RestoreInfo() []RestoreItem {
	a.fmu.Lock()
	defer a.fmu.Unlock()
	return append([]RestoreItem{}, a.f.restore...)
}

// DismissRestore forgets last time's sessions (they were reopened, or not wanted).
func (a *App) DismissRestore() {
	a.fmu.Lock()
	a.f.restore = nil
	a.fmu.Unlock()
	a.emit("restore:done", nil)
	a.saveRestore()
}
