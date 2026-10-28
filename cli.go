package main

import (
	"bufio"
	"embed"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/remote"
	"golang.org/x/term"
)

//go:embed all:frontend/dist
var assets embed.FS

const usage = `Quarters: a home for Claude Code.

  quarters                       %s
  quarters --serve               the remote-access server only, no window
  quarters --set-password NAME   set the username and password browsers sign in with
  quarters --pair                a one-time link that pairs a device (passkey)
  quarters --devices             the paired devices
  quarters --remove-device NAME  unpair a device (by name or id); it is signed out
  quarters --help

Settings and projects: %s
`

// runCLI runs a command-line command and reports whether there was one.
// serverOnly is the build without a window, where serving is the default.
func runCLI(app *App, args []string, serverOnly bool) bool {
	if len(args) == 0 {
		if serverOnly {
			serve(app, true)
			return true
		}
		return false
	}
	switch args[0] {
	case "--serve":
		serve(app, serverOnly)
	case "--set-password":
		if len(args) < 2 {
			log.Fatal("usage: quarters --set-password NAME")
		}
		setPassword(app, args[1])
		reloadServer()
	case "--pair":
		pair()
	case "--devices":
		devices()
	case "--remove-device":
		if len(args) < 2 {
			log.Fatal("usage: quarters --remove-device NAME")
		}
		removeDevice(args[1])
	case "--help", "-h", "help":
		first := "the app"
		if serverOnly {
			first = "the remote-access server (this build has no window)"
		}
		fmt.Printf(usage, first, config.Path())
	default:
		if !serverOnly && strings.HasPrefix(args[0], "-psn_") { // macOS process serial number
			return false
		}
		log.Fatalf("unknown command %q (try --help)", args[0])
	}
	return true
}

// setPassword sets password sign-in for remote access without a window:
// quarters --set-password NAME, then the password on standard input.
func setPassword(app *App, user string) {
	fmt.Fprintf(os.Stderr, "password for %s (10+ characters): ", user)
	var line string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd())) // not echoed
		if err != nil {
			log.Fatal(err)
		}
		line = string(b)
	} else {
		var err error
		if line, err = bufio.NewReader(os.Stdin).ReadString('\n'); err != nil && line == "" {
			log.Fatal(err)
		}
	}
	if err := app.SetRemotePassword(user, strings.TrimRight(line, "\r\n")); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintln(os.Stderr, "\npassword set")
}

// A running server (quarters --serve) writes its process id here, so the
// commands above can ask it for a pairing link (SIGUSR1) or to reload its
// devices and password after a change (SIGHUP).
func pidFile() string     { return filepath.Join(remoteDir(), "server.pid") }
func pairingFile() string { return filepath.Join(remoteDir(), "pairing-link") }

func runningServer() *os.Process {
	data, err := os.ReadFile(pidFile())
	if err != nil {
		return nil
	}
	var pid int
	if _, err := fmt.Sscan(string(data), &pid); err != nil || pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil || p.Signal(syscall.Signal(0)) != nil {
		return nil
	}
	return p
}

// reloadServer makes a running server pick up changed devices or password.
func reloadServer() {
	if p := runningServer(); p != nil && p.Signal(syscall.SIGHUP) == nil {
		fmt.Fprintln(os.Stderr, "the running server has picked it up (browsers sign in again)")
	}
}

func pair() {
	p := runningServer()
	if p == nil {
		log.Fatal("no server is running: start it with quarters --serve (it prints a pairing link)")
	}
	_ = os.Remove(pairingFile())
	if err := p.Signal(syscall.SIGUSR1); err != nil {
		log.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if link, err := os.ReadFile(pairingFile()); err == nil && len(link) > 0 {
			_ = os.Remove(pairingFile())
			fmt.Printf("pair a device (one use, 10 minutes):\n%s\n", link)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Fatal("the server did not answer")
}

func devices() {
	list := remote.DevicesIn(remoteDir())
	if len(list) == 0 {
		fmt.Println("no devices paired (quarters --pair, or a password: quarters --set-password NAME)")
		return
	}
	for _, d := range list {
		last := "never used"
		if d.LastUsed > 0 {
			last = "last used " + time.UnixMilli(d.LastUsed).Format("2006-01-02 15:04")
			if d.LastIP != "" {
				last += " from " + d.LastIP
			}
		}
		fmt.Printf("%-28s %-6s %s  (id %s)\n", d.Name, d.Access, last, short(d.ID))
	}
}

func short(id string) string {
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

func removeDevice(which string) {
	var found []remote.DeviceView
	for _, d := range remote.DevicesIn(remoteDir()) {
		if strings.EqualFold(d.Name, which) || len(which) >= 6 && strings.HasPrefix(d.ID, which) {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		log.Fatalf("no device %q (quarters --devices lists them)", which)
	case 1:
	default:
		log.Fatalf("%d devices match %q: use the id from quarters --devices", len(found), which)
	}
	if err := remote.RemoveDeviceFile(remoteDir(), found[0].ID); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("removed %s\n", found[0].Name)
	reloadServer()
}

// serve runs Quarters without a window: only the remote-access web server.
// alwaysOn (the server-only build) turns remote access on if it is off.
func serve(app *App, alwaysOn bool) {
	if alwaysOn && !app.config().Remote.Enabled {
		if err := app.editConfig(func(c *config.Config) error { c.Remote.Enabled = true; return nil }); err != nil {
			log.Fatal(err)
		}
	}
	app.start()
	app.mu.Lock()
	srv, err := app.remote, app.remoteErr
	app.mu.Unlock()
	if srv == nil {
		if err == "" {
			err = `remote access is off; set "remote": {"enabled": true} in ` + config.Path()
		}
		log.Fatal(err)
	}
	listen := orDefault(app.config().Remote.Listen, config.DefaultListen)
	if app.config().Remote.LAN {
		_, port, _ := net.SplitHostPort(listen)
		listen = "port " + port + ", every address: home network on"
	}
	fmt.Printf("Quarters is serving %s (listening on %s)\n", srv.Origin(), listen)
	if app.config().Remote.LAN {
		_, port, _ := net.SplitHostPort(orDefault(app.config().Remote.Listen, config.DefaultListen))
		fmt.Printf("on the home network: %s\n", strings.Join(remote.LANAddresses(port), "  "))
	}
	if remote.PasswordUserIn(remoteDir()) == "" {
		fmt.Println("no password set: quarters --set-password NAME (needed on the home network)")
	}
	printPairing := func(write bool) {
		srv := app.remoteServer()
		if srv == nil {
			return
		}
		p, err := srv.NewPairing()
		if err != nil {
			return
		}
		fmt.Printf("pair a device (one use, 10 minutes):\n%s\n", p.URL)
		if write {
			_ = os.WriteFile(pairingFile(), []byte(p.URL), 0o600)
		}
	}
	printPairing(false)
	_ = os.WriteFile(pidFile(), []byte(fmt.Sprint(os.Getpid())), 0o600)
	defer os.Remove(pidFile())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR1)
	for s := range sig {
		switch s {
		case syscall.SIGUSR1:
			printPairing(true)
		case syscall.SIGHUP: // devices or password changed on disk
			app.stopRemote()
			app.startRemote()
			fmt.Println("reloaded devices and password")
		default:
			app.stop()
			return
		}
	}
}
