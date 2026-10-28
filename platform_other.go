//go:build !darwin

package main

import (
	"errors"
	"os/exec"
)

// openWith opens a link, a file in its editor (edit) or a folder with the
// desktop's default application.
func openWith(target string, _ bool) *exec.Cmd { return exec.Command("xdg-open", target) }

// awakeCmd keeps the machine from sleeping until it is stopped, where
// systemd can (systemd-inhibit holds a sleep lock while its command runs).
func awakeCmd(int) *exec.Cmd {
	if _, err := exec.LookPath("systemd-inhibit"); err != nil {
		return nil
	}
	return exec.Command("systemd-inhibit", "--what=sleep:idle", "--who=Quarters", "--why=Claude is working or a browser is connected", "--mode=block", "sleep", "infinity")
}

// The Dock and the global hotkey are macOS features.
func setDockBadge(string) {}
func bounceDock()         {}
func activateApp()        {}
func toggleApp()          {}

func registerHotkey(combo string) error {
	if combo == "" || combo == "off" {
		return nil
	}
	return errors.New("the global hotkey is only available on macOS")
}

func parseHotkey(string) (code, mods int, err error) {
	return 0, 0, errors.New("the global hotkey is only available on macOS")
}
