package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/projects"
	"github.com/amithgc/quarters/internal/remote"
	"github.com/amithgc/quarters/internal/session"
)

// ---- optional features (switched on and off in Settings) ----

// featureState is what the optional features keep between calls; App.fmu
// guards it.
type featureState struct {
	viewing   map[string]viewing    // client → the session it shows
	links     map[int]*sessionLinks // dev server links found in output
	ports     map[string]portCheck  // "127.0.0.1:3000" → up?
	git       map[string]gitEntry   // project → git status
	gitBusy   int
	alerted   map[string]bool   // usage limits already warned about
	schedRun  map[string]string // automation → day it last ran by itself
	restore   []RestoreItem     // sessions open when Quarters last quit
	awake     *exec.Cmd         // caffeinate, while keeping the Mac awake
	hotkeyErr string
}

func (f *featureState) init() {
	f.viewing = map[string]viewing{}
	f.links = map[int]*sessionLinks{}
	f.ports = map[string]portCheck{}
	f.git = map[string]gitEntry{}
	f.alerted = map[string]bool{}
	f.schedRun = map[string]string{}
}

type viewing struct {
	id int
	at time.Time
}

// How often the background work runs.
const (
	attentionEvery = 500 * time.Millisecond // "Claude finished" checks
	awakeEvery     = 5 * time.Second
	scheduleEvery  = 20 * time.Second
	restoreEvery   = 30 * time.Second // what's open, saved in case of a crash
	usageEvery     = time.Minute
)

// background runs the features that work without any window open.
func (a *App) background() {
	attention := time.NewTicker(attentionEvery)
	awake := time.NewTicker(awakeEvery)
	schedule := time.NewTicker(scheduleEvery)
	restore := time.NewTicker(restoreEvery)
	usage := time.NewTicker(usageEvery)
	firstUsage := time.After(5 * time.Second)
	for {
		select {
		case <-attention.C:
			a.attention()
		case <-awake.C:
			a.keepAwake()
		case <-schedule.C:
			a.runSchedules(time.Now())
		case <-restore.C:
			a.saveRestore()
		case <-firstUsage:
			go a.usageAlerts()
		case <-usage.C:
			go a.usageAlerts() // fetches over the network: never holds up the rest
		}
	}
}

// tabMeta is what a window attaches to a session (see newTab in app.js).
type tabMeta struct {
	Key     string `json:"key"`
	Started int64  `json:"started"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Dir     string `json:"dir"`
	Command string `json:"command"`
	AutoKey string `json:"autoKey"`
	Cwd     string `json:"cwd"`
	Group   *struct {
		AutoName string `json:"autoName"`
	} `json:"group"`
}

func metaOf(info session.Info) tabMeta {
	var m tabMeta
	_ = json.Unmarshal(info.Meta, &m)
	return m
}

// project finds a configured project by its full path.
func (a *App) project(path string) (config.Project, bool) {
	for _, p := range a.config().Projects {
		if config.ExpandPath(p.Path) == path {
			return p, true
		}
	}
	return config.Project{}, false
}

// insideProject reports whether file is inside one of the projects' folders
// (following symlinks), or is a project's chosen icon. Browsers may only
// read files like these.
func (a *App) insideProject(file string) bool {
	file = filepath.Clean(file)
	real, err := filepath.EvalSymlinks(file)
	if err != nil {
		return false
	}
	for _, p := range a.config().Projects {
		if icon := projects.IconPath(p); icon != "" && filepath.Clean(icon) == file {
			return true
		}
		root, err := filepath.EvalSymlinks(config.ExpandPath(p.Path))
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(root, real); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return true
		}
	}
	return false
}

func (a *App) remoteServer() *remote.Server {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.remote
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
