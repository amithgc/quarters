package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/projects"
)

// ---- settings ----

// FeatureView is a feature and whether it is on.
type FeatureView struct {
	config.Feature
	On bool `json:"on"`
}

// SettingsView is what the Settings dialog shows.
type SettingsView struct {
	Features     []FeatureView   `json:"features"`
	Yolo         bool            `json:"yolo"`
	Prompts      []config.Prompt `json:"prompts"`
	UsageAlertAt int             `json:"usageAlertAt"`
	Hotkey       string          `json:"hotkey"`
	HotkeyError  string          `json:"hotkeyError"`
	Awake        bool            `json:"awake"` // keeping the Mac awake right now
	ConfigPath   string          `json:"configPath"`
}

// Settings describes the settings.
func (a *App) Settings() SettingsView {
	cfg := a.config()
	v := SettingsView{Yolo: cfg.Yolo == nil || *cfg.Yolo, Prompts: cfg.Prompts, UsageAlertAt: cfg.AlertAt(),
		Hotkey: cfg.Hotkey, ConfigPath: projects.ShortPath(config.Path())}
	if v.Hotkey == "" {
		v.Hotkey = "ctrl+alt+space"
	}
	if v.Prompts == nil {
		v.Prompts = []config.Prompt{}
	}
	for _, f := range config.Features {
		v.Features = append(v.Features, FeatureView{f, cfg.On(f.Key)})
	}
	a.fmu.Lock()
	v.HotkeyError, v.Awake = a.f.hotkeyErr, a.f.awake != nil
	a.fmu.Unlock()
	return v
}

// editSettings changes a setting and tells every window, so open Settings
// dialogs and feature buttons follow.
func (a *App) editSettings(change func(*config.Config) error) error {
	err := a.editConfig(change)
	if err == nil {
		go a.emit("settings", nil)
	}
	return err
}

// SetFeature switches an optional feature on or off.
func (a *App) SetFeature(key string, on bool) error {
	if !slices.ContainsFunc(config.Features, func(f config.Feature) bool { return f.Key == key }) {
		return fmt.Errorf("unknown setting %q", key)
	}
	err := a.editSettings(func(c *config.Config) error {
		if c.Features == nil {
			c.Features = map[string]bool{}
		}
		c.Features[key] = on
		return nil
	})
	if err == nil {
		switch key {
		case "hotkey":
			a.applyHotkey()
		case "keep_awake":
			go a.keepAwake()
		}
	}
	return err
}

// SetYolo sets whether Claude skips permission prompts by default.
func (a *App) SetYolo(on bool) error {
	return a.editSettings(func(c *config.Config) error {
		c.Yolo = &on
		return nil
	})
}

// SetUsageAlertAt sets the usage warning level (percent).
func (a *App) SetUsageAlertAt(n int) error {
	if n < 10 || n > 100 {
		return fmt.Errorf("pick a level between 10 and 100%%")
	}
	return a.editSettings(func(c *config.Config) error {
		c.UsageAlertAt = n
		return nil
	})
}

// SetHotkey changes the global hotkey, e.g. "ctrl+alt+space".
func (a *App) SetHotkey(combo string) error {
	combo = strings.ToLower(strings.TrimSpace(combo))
	if _, _, err := parseHotkey(combo); err != nil {
		return err
	}
	if err := a.editSettings(func(c *config.Config) error {
		c.Hotkey = combo
		return nil
	}); err != nil {
		return err
	}
	a.applyHotkey()
	a.fmu.Lock()
	defer a.fmu.Unlock()
	if a.f.hotkeyErr != "" {
		return fmt.Errorf("%s", a.f.hotkeyErr)
	}
	return nil
}

func (a *App) applyHotkey() {
	if a.ctx == nil {
		return
	}
	err := registerHotkey(a.config().HotkeyCombo())
	a.fmu.Lock()
	a.f.hotkeyErr = ""
	if err != nil {
		a.f.hotkeyErr = err.Error()
	}
	a.fmu.Unlock()
}

// SavePrompts replaces the saved prompts.
func (a *App) SavePrompts(list []config.Prompt) error {
	var out []config.Prompt
	for _, p := range list {
		p.Name, p.Text = strings.TrimSpace(p.Name), strings.TrimRight(p.Text, " \n\t")
		if p.Name == "" && p.Text == "" {
			continue
		}
		if p.Text == "" {
			return fmt.Errorf("“%s” has no text", p.Name)
		}
		if p.Name == "" {
			r := []rune(strings.Join(strings.Fields(p.Text), " "))
			p.Name = string(r[:min(24, len(r))])
		}
		if len([]rune(p.Name)) > 40 || len(p.Text) > 4000 {
			return fmt.Errorf("“%.20s” is too long", p.Name)
		}
		out = append(out, p)
	}
	if len(out) > 40 {
		return fmt.Errorf("at most 40 prompts")
	}
	return a.editSettings(func(c *config.Config) error {
		c.Prompts = out
		return nil
	})
}
