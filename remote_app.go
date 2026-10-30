package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/remote"
)

// ---- remote access: Quarters in a browser ----

// remoteMethods are the App functions a signed-in browser may call, beside
// the session ones handled in remoteBackend.Call. Everything about remote
// access itself (pairing, devices, turning it on) is Mac-only on purpose.
var remoteMethods = map[string]bool{
	"Projects": true, "ConfigError": true, "Version": true, "Layout": true, "Stats": true, "Account": true, "AgentAccount": true,
	"IconData": true, "FindIcons": true, "SetCollapsed": true, "SaveProject": true, "MoveProject": true,
	"SaveAutomationSizes": true, "RemoveProject": true, "ConfigPath": true, "PastedImage": true,
	"CloseSession": true, "SetSessionGroup": true, "Sessions": true, "Snapshot": true,
	"Settings": true, "SetFeature": true, "SetClaudeRemote": true, "Chat": true, "TabImage": true, "RemoteControlled": true, "SavePrompts": true, "SetUsageAlertAt": true,
	"Conversations": true, "Diff": true, "DevLinks": true, "OpenOnMac": true, "ProjectTools": true,
	"Worktrees": true, "CreateWorktree": true, "RemoveWorktree": true, "RestoreInfo": true, "DismissRestore": true,
	"Queues": true, "QueueAdd": true, "QueueRemove": true, "QueueMove": true, "QueueNext": true,
	"Usage": true, "Instructions": true, "ReadInstructions": true, "SaveInstructions": true, "Notes": true, "SaveNotes": true,
	"ListDir": true, "ListFiles": true, "FileStatus": true, "ReadFile": true, "FileStamp": true, "SaveFile": true,
}

// What a device with less than full access may call ("view": watch only;
// "type": also type into running sessions).
var (
	viewMethods = map[string]bool{
		"ClientID": true, "Access": true, "Projects": true, "ConfigError": true, "Version": true, "Layout": true, "Stats": true,
		"Account": true, "AgentAccount": true, "IconData": true, "ConfigPath": true, "PastedImage": true, "Sessions": true,
		"Snapshot": true, "Settings": true, "Conversations": true, "Diff": true, "DevLinks": true,
		"ProjectTools": true, "Worktrees": true, "RestoreInfo": true, "SetViewing": true, "Chat": true, "TabImage": true, "RemoteControlled": true,
		"PushKey": true, "PushSubscribe": true, "PushUnsubscribe": true,
		"Queues": true, "Usage": true, "Instructions": true, "ReadInstructions": true, "Notes": true,
		"ListDir": true, "ListFiles": true, "FileStatus": true, "ReadFile": true, "FileStamp": true,
	}
	// queued prompts are typed in later: the same as typing them now
	typeMethods = map[string]bool{"Write": true, "Resize": true, "Claim": true,
		"QueueAdd": true, "QueueRemove": true, "QueueMove": true, "QueueNext": true}
)

func remoteDir() string { return filepath.Dir(config.Path()) }

// startRemote starts the web server when remote access is on.
func (a *App) startRemote() {
	rc := a.config().Remote
	a.mu.Lock()
	a.remoteErr = ""
	a.mu.Unlock()
	if !rc.Enabled {
		return
	}
	listen := rc.Listen
	if listen == "" {
		listen = config.DefaultListen
	}
	opts := remote.Options{URL: rc.URL, Listen: listen, LAN: rc.LAN, Dir: remoteDir(), App: a.assets}
	if rc.TLSCert != "" || rc.TLSKey != "" {
		opts.TLSCert, opts.TLSKey = config.ExpandPath(rc.TLSCert), config.ExpandPath(rc.TLSKey)
	}
	srv, err := remote.New(opts, remoteBackend{a: a})
	if err == nil {
		err = srv.Start()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.remoteErr = err.Error()
		return
	}
	a.remote = srv
}

func (a *App) stopRemote() {
	a.mu.Lock()
	srv := a.remote
	a.remote = nil
	a.mu.Unlock()
	if srv != nil {
		srv.Stop()
	}
}

// RemoteView is what the Remote Access dialog shows.
type RemoteView struct {
	Enabled bool                `json:"enabled"`
	URL     string              `json:"url"`
	Address string              `json:"address"` // where browsers open it while running
	Listen  string              `json:"listen"`
	LAN     bool                `json:"lan"`
	LANURLs []string            `json:"lanURLs"` // home-network addresses, while running
	LANQR   string              `json:"lanQR"`   // QR code of the first
	User    string              `json:"user"`    // password sign-in's username ("" = off)
	Running bool                `json:"running"`
	Error   string              `json:"error"`
	Devices []remote.DeviceView `json:"devices"`
	Log     []string            `json:"log"`
}

// RemoteStatus describes remote access (Mac window only).
func (a *App) RemoteStatus() RemoteView {
	rc := a.config().Remote
	a.mu.Lock()
	srv, rerr := a.remote, a.remoteErr
	a.mu.Unlock()
	v := RemoteView{Enabled: rc.Enabled, URL: rc.URL, Listen: rc.Listen, LAN: rc.LAN, Error: rerr, User: remote.PasswordUserIn(remoteDir())}
	if v.Listen == "" {
		v.Listen = config.DefaultListen
	}
	if srv != nil && rc.LAN {
		_, port, _ := net.SplitHostPort(v.Listen)
		v.LANURLs = remote.LANAddresses(port)
		if len(v.LANURLs) > 0 {
			v.LANQR, _ = remote.QR(v.LANURLs[0] + "/login")
		}
	}
	if srv != nil {
		v.Running = srv.Running()
		v.Address = srv.Origin()
		v.Devices = srv.Devices()
		v.Log = srv.Log(40)
	} else {
		v.Devices = remote.DevicesIn(remoteDir()) // still paired while remote access is off
		v.Log = remote.LogIn(remoteDir(), 40)
	}
	return v
}

// SetRemote turns remote access on or off and sets its addresses.
func (a *App) SetRemote(enabled bool, url, listen string, lan bool) error {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	listen = strings.TrimSpace(listen)
	if listen == config.DefaultListen {
		listen = ""
	}
	if enabled {
		if _, err := remote.New(remote.Options{URL: url, Listen: orDefault(listen, config.DefaultListen), LAN: lan, Dir: remoteDir()}, remoteBackend{a: a}); err != nil {
			return err
		}
	}
	if err := a.editConfig(func(c *config.Config) error {
		c.Remote.Enabled, c.Remote.URL, c.Remote.Listen, c.Remote.LAN = enabled, url, listen, lan
		return nil
	}); err != nil {
		return err
	}
	a.stopRemote()
	a.startRemote()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.remoteErr != "" {
		return fmt.Errorf("%s", a.remoteErr)
	}
	return nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// PairDevice makes a one-time pairing link (and QR code) for a new device.
func (a *App) PairDevice() (remote.Pairing, error) {
	srv := a.remoteServer()
	if srv == nil {
		return remote.Pairing{}, fmt.Errorf("start the remote server first")
	}
	return srv.NewPairing()
}

// CancelPairing makes the current pairing link useless.
func (a *App) CancelPairing() {
	srv := a.remoteServer()
	if srv != nil {
		srv.CancelPairing()
	}
}

// SetDeviceAccess changes what a device may do: "full", "type" or "view".
func (a *App) SetDeviceAccess(id, access string) error {
	if access == "full" {
		access = remote.AccessFull
	}
	if srv := a.remoteServer(); srv != nil {
		return srv.SetAccess(id, access)
	}
	return remote.SetAccessFile(remoteDir(), id, access)
}

// RemoveDevice unpairs a device; it is disconnected at once.
func (a *App) RemoveDevice(id string) error {
	srv := a.remoteServer()
	if srv != nil {
		return srv.RemoveDevice(id)
	}
	return remote.RemoveDeviceFile(remoteDir(), id)
}

// SetRemotePassword sets the username and password browsers can sign in
// with instead of a passkey. Browsers signed in with an earlier one are
// signed out.
func (a *App) SetRemotePassword(user, password string) error {
	user = strings.TrimSpace(user)
	if srv := a.remoteServer(); srv != nil {
		return srv.SetPassword(user, password)
	}
	return remote.SetPasswordFile(remoteDir(), user, password)
}

// SignOutAllDevices ends every browser session (devices stay paired).
func (a *App) SignOutAllDevices() {
	srv := a.remoteServer()
	if srv != nil {
		srv.SignOutAll()
	}
}

// remoteBackend is the App as the web server sees it. It is a separate
// type so these functions are not callable from the Mac window's page.
type remoteBackend struct {
	a *App
	// access overrides the device's access (tests).
	access func(client string) string
}

func (b remoteBackend) Alert(title, body string) {
	if b.a.ctx == nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", title, body)
		return
	}
	b.a.Notify(title, body, "")
}

// Call runs an App function for a browser.
func (b remoteBackend) Call(client, method string, args []json.RawMessage) (any, error) {
	a := b.a
	access := b.accessOf(client)
	switch {
	case access == remote.AccessFull:
	case viewMethods[method], access == remote.AccessType && typeMethods[method]:
	default:
		what := "watch"
		if access == remote.AccessType {
			what = "type into running sessions"
		}
		return nil, fmt.Errorf("this device can only %s (change it on the Mac: Remote Access)", what)
	}
	switch method {
	case "ClientID":
		return client, nil
	case "Access":
		return remoteAccessName(access), nil
	case "SetViewing":
		var id int
		if err := decodeArgs(args, &id); err != nil {
			return nil, err
		}
		a.setViewing(client, id)
		return nil, nil
	case "PushKey":
		return a.pushKey()
	case "PushSubscribe", "PushUnsubscribe":
		srv := a.remoteServer()
		if srv == nil {
			return nil, fmt.Errorf("remote access is off")
		}
		var sub remote.PushSub
		if err := decodeArgs(args, &sub); err != nil {
			return nil, err
		}
		if method == "PushUnsubscribe" {
			return nil, srv.Unsubscribe(client, sub.Endpoint)
		}
		return nil, srv.Subscribe(client, sub)
	case "SetFeature":
		var key string
		var on bool
		if err := decodeArgs(args, &key, &on); err != nil {
			return nil, err
		}
		for _, f := range config.Features {
			if f.Key == key && f.MacOnly {
				return nil, fmt.Errorf("“%s” can only be changed on the Mac", f.Title)
			}
		}
		return nil, a.SetFeature(key, on)
	case "IconData", "FindIcons":
		// only images that belong to a project, never any file on the Mac
		var path string
		if err := decodeArgs(args, &path); err != nil {
			return nil, err
		}
		if !a.insideProject(config.ExpandPath(path)) {
			return nil, fmt.Errorf("not in one of your projects")
		}
		if method == "FindIcons" {
			return a.FindIcons(path), nil
		}
		return a.IconData(path)
	case "SaveProject":
		// whether the agent asks for permission, and which agent it is,
		// are only changed on the Mac
		var oldPath, name, path, group, icon string
		var autos []config.Automation
		if err := decodeArgs(args[:min(len(args), 6)], &oldPath, &name, &path, &group, &icon, &autos); err != nil {
			return nil, err
		}
		yolo, harness, color := "", "", ""
		if p, ok := a.project(oldPath); ok {
			if p.Yolo != nil {
				yolo = map[bool]string{true: "on", false: "off"}[*p.Yolo]
			}
			harness, color = p.Harness, p.Color
		}
		if len(args) > 8 { // its colour is only a look: any device may change it
			if err := decodeArgs(args[8:9], &color); err != nil {
				return nil, err
			}
		}
		return a.SaveProject(oldPath, name, path, group, icon, autos, yolo, harness, color)
	case "Write":
		var id int
		var data string
		if err := decodeArgs(args, &id, &data); err != nil {
			return nil, err
		}
		a.sessions.Write(client, id, data)
		return nil, nil
	case "Resize", "Claim":
		var id, cols, rows int
		if err := decodeArgs(args, &id, &cols, &rows); err != nil {
			return nil, err
		}
		if method == "Resize" {
			a.sessions.Resize(client, id, cols, rows)
		} else {
			a.sessions.Claim(client, id, cols, rows)
		}
		return nil, nil
	case "StartSession":
		var path, kind string
		var cols, rows int
		var meta map[string]any
		if err := decodeArgs(args, &path, &kind, &cols, &rows, &meta); err != nil {
			return nil, err
		}
		return a.startSession(client, path, kind, cols, rows, meta)
	case "StartCommandSession":
		var dir, command string
		var cols, rows int
		var meta map[string]any
		if err := decodeArgs(args, &dir, &command, &cols, &rows, &meta); err != nil {
			return nil, err
		}
		return a.startCommandSession(client, dir, command, cols, rows, meta)
	}
	if !remoteMethods[method] {
		return nil, fmt.Errorf("%s is not available remotely", method)
	}
	m := reflect.ValueOf(a).MethodByName(method)
	if !m.IsValid() {
		return nil, fmt.Errorf("no such function: %s", method)
	}
	t := m.Type()
	if t.NumIn() != len(args) {
		return nil, fmt.Errorf("%s takes %d arguments", method, t.NumIn())
	}
	in := make([]reflect.Value, len(args))
	for i, raw := range args {
		v := reflect.New(t.In(i))
		if err := json.Unmarshal(raw, v.Interface()); err != nil {
			return nil, fmt.Errorf("%s: argument %d: %v", method, i+1, err)
		}
		in[i] = v.Elem()
	}
	out := m.Call(in)
	var res any
	for _, o := range out {
		if o.Type() == reflect.TypeFor[error]() {
			if !o.IsNil() {
				return nil, o.Interface().(error)
			}
			continue
		}
		res = o.Interface()
	}
	return res, nil
}

func (b remoteBackend) accessOf(client string) string {
	if b.access != nil {
		return b.access(client)
	}
	if srv := b.a.remoteServer(); srv != nil {
		return srv.AccessOf(client)
	}
	return remote.AccessView
}

func remoteAccessName(a string) string {
	if a == remote.AccessFull {
		return "full"
	}
	return a
}

func decodeArgs(args []json.RawMessage, dst ...any) error {
	if len(args) != len(dst) {
		return fmt.Errorf("expected %d arguments", len(dst))
	}
	for i, raw := range args {
		if err := json.Unmarshal(raw, dst[i]); err != nil {
			return fmt.Errorf("argument %d: %v", i+1, err)
		}
	}
	return nil
}
