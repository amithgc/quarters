package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A browser must never reach the Mac-only functions: remote access
// settings, pairing, devices, dialogs, Finder and config editing.
func TestRemoteCannotCallMacOnly(t *testing.T) {
	b := remoteBackend{a: &App{}, access: func(string) string { return "" }}
	for _, m := range []string{"SetRemote", "SetRemotePassword", "RemoteStatus", "PairDevice", "CancelPairing", "RemoveDevice",
		"SignOutAllDevices", "OpenConfig", "RevealProject", "Confirm", "ChooseFolder", "ChooseIcon",
		"Notify", "SetBadge", "SaveLayout", "Call", "Alert", "startup", "NoSuchThing",
		"SetDeviceAccess", "SetYolo", "SetHotkey", "SetHarness", "SetProjectHarness", "SetViewing2", "background", "Settings2"} {
		if _, err := b.Call("web1", m, nil); err == nil {
			t.Errorf("%s was callable remotely", m)
		}
	}
}

func TestRemoteCallDecodesArgs(t *testing.T) {
	b := remoteBackend{a: &App{}, access: func(string) string { return "" }}
	if _, err := b.Call("web1", "Write", []json.RawMessage{json.RawMessage(`"not a number"`), json.RawMessage(`"x"`)}); err == nil {
		t.Error("bad arguments were accepted")
	}
	if _, err := b.Call("web1", "ConfigPath", []json.RawMessage{json.RawMessage(`1`)}); err == nil {
		t.Error("extra arguments were accepted")
	}
	r, err := b.Call("web1", "ClientID", nil)
	if err != nil || r != "web1" {
		t.Errorf("ClientID = %v, %v", r, err)
	}
	r, err = b.Call("web1", "ConfigPath", nil)
	if err != nil || r == "" {
		t.Errorf("ConfigPath = %v, %v", r, err)
	}
}

// A watch-only device can read but not type, start or change anything; a
// "type" device can also type into running sessions.
func TestRemoteAccessLevels(t *testing.T) {
	view := remoteBackend{a: &App{}, access: func(string) string { return "view" }}
	typ := remoteBackend{a: &App{}, access: func(string) string { return "type" }}
	for _, m := range []string{"Write", "Resize", "Claim", "StartSession", "StartCommandSession", "CloseSession",
		"SaveProject", "RemoveProject", "SetFeature", "SavePrompts", "CreateWorktree", "RemoveWorktree", "OpenOnMac", "DismissRestore",
		"QueueAdd", "QueueRemove", "QueueMove", "QueueNext", "SaveInstructions", "SaveNotes"} {
		if _, err := view.Call("web1", m, nil); err == nil || !strings.Contains(err.Error(), "only watch") {
			t.Errorf("view device: %s: %v", m, err)
		}
	}
	for _, m := range []string{"StartSession", "CloseSession", "SaveProject", "SetFeature", "CreateWorktree", "SaveInstructions", "SaveNotes"} {
		if _, err := typ.Call("web1", m, nil); err == nil || !strings.Contains(err.Error(), "only type") {
			t.Errorf("type device: %s: %v", m, err)
		}
	}
	if _, err := typ.Call("web1", "Write", []json.RawMessage{json.RawMessage(`"x"`)}); err == nil || strings.Contains(err.Error(), "only") {
		t.Errorf("type device could not type: %v", err)
	}
	if r, err := view.Call("web1", "ConfigPath", nil); err != nil || r == "" {
		t.Errorf("view device could not read: %v", err)
	}
	full := remoteBackend{a: &App{}, access: func(string) string { return "" }}
	if _, err := full.Call("web1", "SetFeature", []json.RawMessage{json.RawMessage(`"remote_ask"`), json.RawMessage(`false`)}); err == nil {
		t.Error("a browser switched off remote_ask")
	}
}

// A browser can only read images that belong to a project.
func TestRemoteIconsOnlyFromProjects(t *testing.T) {
	a, dir := testApp(t)
	b := remoteBackend{a: a, access: func(string) string { return "view" }}
	os.WriteFile(filepath.Join(dir, "icon.png"), []byte("\x89PNG"), 0o644)
	outside := filepath.Join(t.TempDir(), "passport.png")
	os.WriteFile(outside, []byte("\x89PNG"), 0o644)
	os.Symlink(outside, filepath.Join(dir, "link.png"))
	arg := func(s string) []json.RawMessage { j, _ := json.Marshal(s); return []json.RawMessage{j} }
	if _, err := b.Call("web1", "IconData", arg(filepath.Join(dir, "icon.png"))); err != nil {
		t.Errorf("a project's image: %v", err)
	}
	for _, p := range []string{outside, filepath.Join(dir, "link.png"), filepath.Join(dir, "..", "x.png")} {
		if _, err := b.Call("web1", "IconData", arg(p)); err == nil {
			t.Errorf("%s was readable remotely", p)
		}
	}
	if _, err := b.Call("web1", "FindIcons", arg(filepath.Dir(outside))); err == nil {
		t.Error("a folder outside the projects was searched")
	}
}
