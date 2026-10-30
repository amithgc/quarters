package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/projects"
	"github.com/amithgc/quarters/internal/remote"
	"github.com/amithgc/quarters/internal/session"
	"github.com/amithgc/quarters/internal/stats"
)

// App is the Go side of the window. Its exported methods are callable from
// the frontend (window.go.main.App.*).
type App struct {
	ctx      context.Context
	sessions *session.Manager
	reader   *stats.Reader

	mu     sync.Mutex
	cfg    *config.Config
	cfgErr string // why config.json could not be read ("" = fine)
	notify bool   // macOS allows notifications
	noteID int

	assets    fs.FS // the web page, also served to remote browsers
	remote    *remote.Server
	remoteErr string

	history   *stats.History
	today     todayCache   // today's token use, across projects
	account   accountCache // the Claude plan's limits
	agentAcct accountCache // Antigravity's plan and quotas
	usage     usageCache   // tokens per project and day

	fmu   sync.Mutex // the optional features' state below
	f     featureState
	convs map[int]string // session id → Claude conversation id

	worktrees sync.Map // folders the file browser found to be worktrees: dir → time.Time
}

// macClient is the Mac window's name among the windows sharing sessions
// (browsers are "web1", "web2", …).
const macClient = "mac"

// NewApp loads the config, creating a starter one from recent Claude Code
// projects the first time.
// A broken config.json does not stop the app: it starts with no projects and
// shows the error until the file is fixed.
func NewApp() *App {
	a := &App{reader: stats.NewReader(), history: stats.NewHistory(), convs: map[int]string{}}
	a.f.init()
	if err := config.MigrateOld(); err != nil {
		fmt.Fprintln(os.Stderr, "could not copy the old settings:", err)
	}
	cfg, err := config.Load(projects.Seed(6))
	a.cfg = cfg
	if err != nil {
		a.cfgErr = err.Error()
	}
	return a
}

// startup is called by Wails once the window exists.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.setupNotifications()
	a.applyHotkey()
	a.start()
}

// start starts everything that works without a window (also for --serve).
func (a *App) start() {
	a.sessions = session.NewManager(a.emit, a.watchOutput)
	a.startRemote()
	a.loadRestore()
	go a.background()
}

// emit sends an event to the Mac window and to every signed-in browser.
func (a *App) emit(event string, data any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data)
	}
	if r := a.remoteServer(); r != nil {
		r.Broadcast(event, data)
	}
}

// bg is a context for background work (there is none without a window).
func (a *App) bg() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// setupNotifications asks macOS for permission to post notifications (once;
// macOS remembers the answer) and opens the tab a clicked notification is about.
func (a *App) setupNotifications() {
	if err := runtime.InitializeNotifications(a.ctx); err != nil {
		return
	}
	if ok, _ := runtime.CheckNotificationAuthorization(a.ctx); !ok {
		if ok, _ = runtime.RequestNotificationAuthorization(a.ctx); !ok {
			return
		}
	}
	runtime.OnNotificationResponse(a.ctx, func(r runtime.NotificationResult) {
		if r.Error != nil {
			return
		}
		key, _ := r.Response.UserInfo["tab"].(string)
		runtime.WindowShow(a.ctx)
		activateApp()
		runtime.EventsEmit(a.ctx, "notify:open", key)
	})
	a.mu.Lock()
	a.notify = true
	a.mu.Unlock()
}

// Notify posts a macOS notification; clicking it opens the tab named by key.
// The Dock icon bounces too, which works even without notification permission.
func (a *App) Notify(title, body, key string) {
	if a.ctx == nil {
		return
	}
	bounceDock()
	a.mu.Lock()
	ok := a.notify
	a.noteID++
	id := a.noteID
	a.mu.Unlock()
	if !ok {
		return
	}
	_ = runtime.SendNotification(a.ctx, runtime.NotificationOptions{
		ID: fmt.Sprintf("quarters-%d", id), Title: title, Body: body,
		Data: map[string]any{"tab": key},
	})
}

// SetBadge shows the number of Claude tabs waiting for you on the Dock icon.
func (a *App) SetBadge(n int) {
	if n > 0 {
		setDockBadge(fmt.Sprint(n))
	} else {
		setDockBadge("")
	}
}

func (a *App) beforeClose(ctx context.Context) bool {
	n := a.sessions.Running()
	if n == 0 {
		return false
	}
	answer, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "Quit Quarters?",
		Message:       fmt.Sprintf("%d session(s) are still running. Quitting ends them.", n),
		Buttons:       []string{"Quit", "Cancel"},
		DefaultButton: "Quit",
		CancelButton:  "Cancel",
	})
	return err == nil && answer == "Cancel"
}

// shutdown is called by Wails when the app quits.
func (a *App) shutdown(context.Context) { a.stop() }

// stop saves what is open, then ends every session.
func (a *App) stop() {
	a.saveRestore()
	a.stopAwake()
	a.stopRemote()
	a.sessions.CloseAll()
}

// config returns the settings, re-reading the file if it was edited.
func (a *App) config() *config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Changed() {
		cfg, err := config.Load(nil)
		a.cfgErr = ""
		if err != nil {
			a.cfgErr = err.Error()
			go a.emit("flash", "config.json: "+a.cfgErr)
		}
		// Keep the projects already shown until the file reads cleanly again.
		if err == nil || len(a.cfg.Projects) == 0 {
			a.cfg = cfg
		} else {
			a.cfg.Touch()
		}
	}
	return a.cfg
}

// current returns the settings in use without checking the file for edits:
// for code that runs very often (config re-reads it when it changed).
func (a *App) current() *config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

// editConfig changes the settings and saves them. change gets a copy; once
// it is saved, the copy replaces the settings in use. Code reading the old
// settings keeps a consistent copy, so nothing changes under it. A
// config.json with a mistake in it is never overwritten.
func (a *App) editConfig(change func(*config.Config) error) error {
	a.config() // pick up edits made to the file first
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfgErr != "" {
		return fmt.Errorf("fix config.json first (%s)", a.cfgErr)
	}
	next := a.cfg.Clone()
	if err := change(next); err != nil {
		return err
	}
	if err := next.Save(); err != nil {
		return err
	}
	a.cfg = next
	return nil
}

// ConfigError explains why config.json could not be read ("" when it is fine).
func (a *App) ConfigError() string {
	a.config()
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfgErr
}

// ---- projects ----

// ProjectView is a project as the window shows it.
type ProjectView struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	ShortPath  string `json:"shortPath"`
	Group      string `json:"group"`
	Icon       string `json:"icon"` // full path, "" for none
	Branch     string `json:"branch"`
	LastActive int64  `json:"lastActive"` // unix ms, 0 = never
	Missing    bool   `json:"missing"`
	Yolo       string `json:"yolo"`    // "", "on" or "off"
	Harness    string `json:"harness"` // its own coding agent, "" for the default
	Color      string `json:"color"`   // "#rrggbb" or "accent", "" for one from the name
	Agent      string `json:"agent"`   // the coding agent its tabs start
	Git        bool   `json:"git"`     // git status below is known
	Dirty      int    `json:"dirty"`
	Ahead      int    `json:"ahead"`
	Behind     int    `json:"behind"`

	Automations []AutomationView `json:"automations"`
}

// AutomationView is an automation with its folders resolved.
type AutomationView struct {
	Name   string    `json:"name"`
	Layout string    `json:"layout"` // grid, columns, rows or tabs
	Tabs   []TabView `json:"tabs"`
	Auto   bool      `json:"auto"`
	Cols   []float64 `json:"cols"`
	Rows   []float64 `json:"rows"`

	Schedule *config.Schedule `json:"schedule"`
}

// TabView is one tab of an automation. Dir is resolved; RawName and RawDir
// are as written in config.json, for the edit dialog.
type TabView struct {
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Command string `json:"command"`
	RawName string `json:"rawName"`
	RawDir  string `json:"rawDir"`
}

// Projects lists the projects from the config file.
func (a *App) Projects() []ProjectView {
	cfg := a.config()
	list := projects.Load(cfg)
	out := make([]ProjectView, 0, len(list))
	gitOn := cfg.On("git_status")
	for _, p := range list {
		v := ProjectView{Name: p.Name, Path: p.Path, ShortPath: projects.ShortPath(p.Path),
			Group: p.Group, Icon: p.Icon, Branch: p.Branch, Missing: p.Missing,
			Harness: p.Harness, Color: p.Color, Agent: cfg.HarnessFor(config.Project{Harness: p.Harness}).ID}
		if p.Yolo != nil {
			v.Yolo = map[bool]string{true: "on", false: "off"}[*p.Yolo]
		}
		if gitOn && !p.Missing {
			if g, ok := a.gitOf(p.Path, gitListAge); ok && g.IsRepo {
				v.Git, v.Dirty, v.Ahead, v.Behind = true, len(g.Files), g.Ahead, g.Behind
				if g.Branch != "" {
					v.Branch = g.Branch
				}
			}
		}
		if !p.LastActive.IsZero() {
			v.LastActive = p.LastActive.UnixMilli()
		}
		v.Automations = automationViews(p)
		out = append(out, v)
	}
	return out
}

// automationViews resolves a project's automations: layout defaults, tab
// names and folders.
func automationViews(p projects.Project) []AutomationView {
	var out []AutomationView
	for _, au := range p.Automations {
		av := AutomationView{Name: au.Name, Layout: au.Layout, Auto: au.Auto, Cols: au.Cols, Rows: au.Rows, Schedule: au.Schedule}
		switch av.Layout {
		case "grid", "columns", "rows", "tabs":
		default:
			av.Layout = "grid"
		}
		for _, t := range au.Tabs {
			dir := p.Path
			if d := strings.TrimSpace(t.Dir); d != "" {
				if strings.HasPrefix(d, "/") || strings.HasPrefix(d, "~") {
					dir = config.ExpandPath(d)
				} else {
					dir = filepath.Join(p.Path, d)
				}
			}
			name := t.Name
			if name == "" {
				name = t.Command
			}
			if name == "" {
				name = "shell"
			}
			av.Tabs = append(av.Tabs, TabView{Name: name, Dir: dir, Command: t.Command, RawName: t.Name, RawDir: t.Dir})
		}
		if av.Name == "" {
			av.Name = fmt.Sprintf("automation %d", len(out)+1)
		}
		out = append(out, av)
	}
	return out
}

// ChooseFolder shows the native folder picker ("" if cancelled).
func (a *App) ChooseFolder(start string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Choose the project folder",
		DefaultDirectory:     start,
		CanCreateDirectories: true,
	})
}

// ChooseIcon shows the native file picker for an icon image ("" if cancelled).
func (a *App) ChooseIcon(start string) (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Choose an icon for the project",
		DefaultDirectory: start,
		Filters: []runtime.FileFilter{{
			DisplayName: "Images (png, jpg, svg, ico, webp, gif)",
			Pattern:     "*.png;*.jpg;*.jpeg;*.svg;*.ico;*.webp;*.gif;*.icns",
		}},
	})
}

// FindIcons suggests favicons / app icons found in a project folder.
func (a *App) FindIcons(path string) []string {
	return projects.FindIcons(config.ExpandPath(path), 12)
}

// IconData returns an icon image as a data: URL.
func (a *App) IconData(path string) (string, error) { return projects.IconDataURL(path) }

// SetCollapsed stores which project groups are folded shut.
func (a *App) SetCollapsed(groups []string) error {
	return a.editConfig(func(c *config.Config) error {
		c.Layout.Collapsed = groups
		return nil
	})
}

// SaveProject adds a project (oldPath "") or edits the one at oldPath, and
// returns its full path. icon is an image path ("" for none).
// yolo is "" (the global setting), "on" or "off"; harness is the
// project's coding agent ("" for the default).
func (a *App) SaveProject(oldPath, name, path, group, icon string, autos []config.Automation, yolo, harness, color string) (string, error) {
	if !config.KnownHarness(harness) {
		return "", fmt.Errorf("unknown harness %q", harness)
	}
	color = strings.ToLower(strings.TrimSpace(color))
	if color != "" && color != "accent" && !hexColor.MatchString(color) {
		return "", fmt.Errorf("a colour is written like #e08a6a (or \"accent\")")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("choose the project folder")
	}
	full := config.ExpandPath(path)
	if st, err := os.Stat(full); err != nil || !st.IsDir() {
		return "", fmt.Errorf("folder not found: %s", full)
	}
	name = strings.TrimSpace(name)
	if name == filepath.Base(full) {
		name = "" // the default; keep config.json short
	}
	icon = strings.TrimSpace(icon)
	if icon != "" {
		icon = config.ExpandPath(icon)
		if _, err := projects.IconDataURL(icon); err != nil {
			return "", err
		}
		// Inside the project folder, store it relative so the folder can move.
		if rel, err := filepath.Rel(full, icon); err == nil && !strings.HasPrefix(rel, "..") {
			icon = rel
		} else {
			icon = projects.ShortPath(icon)
		}
	}
	autos, err := cleanAutomations(autos)
	if err != nil {
		return "", err
	}
	var yp *bool
	switch yolo {
	case "on", "off":
		v := yolo == "on"
		yp = &v
	}
	return full, a.editConfig(func(c *config.Config) error {
		return c.SaveProject(oldPath, config.Project{Name: name, Path: projects.ShortPath(full),
			Group: strings.TrimSpace(group), Icon: icon, Automations: autos, Yolo: yp, Harness: harness, Color: color})
	})
}

var hexColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// cleanAutomations trims what the dialog sent and checks it.
func cleanAutomations(in []config.Automation) ([]config.Automation, error) {
	var out []config.Automation
	seen := map[string]bool{}
	for _, au := range in {
		au.Name = strings.TrimSpace(au.Name)
		if au.Name == "" {
			return nil, fmt.Errorf("every automation needs a name")
		}
		if seen[au.Name] {
			return nil, fmt.Errorf("two automations are called %q", au.Name)
		}
		seen[au.Name] = true
		switch au.Layout {
		case "columns", "rows", "tabs":
		default:
			au.Layout = "grid"
		}
		var tabs []config.AutomationTab
		for _, t := range au.Tabs {
			t.Name, t.Dir, t.Command = strings.TrimSpace(t.Name), strings.TrimSpace(t.Dir), strings.TrimSpace(t.Command)
			if t.Name != "" || t.Dir != "" || t.Command != "" {
				tabs = append(tabs, t)
			}
		}
		if len(tabs) == 0 {
			return nil, fmt.Errorf("“%s” needs at least one terminal", au.Name)
		}
		if au.Schedule != nil {
			sc, err := cleanSchedule(*au.Schedule)
			if err != nil {
				return nil, fmt.Errorf("“%s”: %v", au.Name, err)
			}
			au.Schedule = sc
		}
		if len(tabs) != len(au.Tabs) || au.Layout == "tabs" {
			au.Cols, au.Rows = nil, nil // the grid changed: back to equal sizes
		}
		au.Tabs = tabs
		out = append(out, au)
	}
	return out, nil
}

// MoveProject moves a project in the list (dragged next to target, or to
// the end of group when target is "") and into group.
func (a *App) MoveProject(path, target string, after bool, group string) error {
	return a.editConfig(func(c *config.Config) error {
		return c.MoveProject(path, target, after, strings.TrimSpace(group))
	})
}

// SaveAutomationSizes stores the pane sizes of an automation.
func (a *App) SaveAutomationSizes(path, name string, cols, rows []float64) error {
	return a.editConfig(func(c *config.Config) error {
		return c.SetAutomationSizes(path, name, cols, rows)
	})
}

// RemoveProject takes a project out of the config.
func (a *App) RemoveProject(path string) error {
	return a.editConfig(func(c *config.Config) error { return c.RemoveProject(path) })
}

// ConfigPath is where the settings file lives.
func (a *App) ConfigPath() string { return projects.ShortPath(config.Path()) }

// OpenConfig opens the settings file in the default text editor.
func (a *App) OpenConfig() error {
	return openWith(config.Path(), true).Run()
}

// RevealProject shows a project folder in Finder.
func (a *App) RevealProject(path string) error {
	if _, ok := a.project(path); !ok {
		return fmt.Errorf("not one of your projects")
	}
	return openWith(path, false).Run()
}

// Layout returns the saved panel widths.
func (a *App) Layout() config.Layout { return a.config().Layout }

// SaveLayout stores the panel widths.
func (a *App) SaveLayout(left, right int) error {
	return a.editConfig(func(c *config.Config) error {
		c.Layout.LeftWidth, c.Layout.RightWidth = left, right
		return nil
	})
}

// PastedImage returns image #n pasted into a Claude Code conversation in the
// project at path, as a data: URL. since (ms) is when the tab started, so the
// tab's own conversation is searched first.
func (a *App) PastedImage(path string, n int, since int64) (string, error) {
	if _, ok := a.project(path); !ok {
		return "", fmt.Errorf("not one of your projects")
	}
	return stats.FindImage(projectDir(path), n, time.UnixMilli(since).Add(-time.Minute))
}
