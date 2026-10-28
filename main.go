// Quarters is a Mac app wrapping Claude Code: projects on the left, Claude
// Code in the middle, session stats on the right.
package main

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/amithgc/quarters/internal/config"
	"github.com/amithgc/quarters/internal/remote"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/term"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	app.assets, _ = fs.Sub(assets, "frontend/dist")
	if len(os.Args) > 1 && os.Args[1] == "--serve" {
		serve(app)
		return
	}
	if len(os.Args) > 2 && os.Args[1] == "--set-password" {
		setPassword(app, os.Args[2])
		return
	}

	var ctx context.Context
	send := func(action string) func(*menu.CallbackData) {
		return func(*menu.CallbackData) { runtime.EventsEmit(ctx, "menu", action) }
	}

	appMenu := menu.NewMenu()
	appMenu.Append(menu.AppMenu())
	appMenu.Append(menu.EditMenu())

	sess := appMenu.AddSubmenu("Session")
	sess.AddText("New Claude Tab", keys.CmdOrCtrl("t"), send("new-claude"))
	sess.AddText("Continue Last Conversation", keys.Combo("t", keys.CmdOrCtrlKey, keys.ShiftKey), send("continue"))
	sess.AddText("New Shell Tab", keys.Combo("t", keys.CmdOrCtrlKey, keys.OptionOrAltKey), send("new-shell"))
	sess.AddSeparator()
	sess.AddText("Close Tab", keys.CmdOrCtrl("w"), send("close-tab"))
	sess.AddText("Next Tab", keys.Combo("]", keys.CmdOrCtrlKey, keys.ShiftKey), send("next-tab"))
	sess.AddText("Previous Tab", keys.Combo("[", keys.CmdOrCtrlKey, keys.ShiftKey), send("prev-tab"))
	sess.AddSeparator()
	sess.AddText("Clear Scrollback", keys.CmdOrCtrl("k"), send("clear"))

	proj := appMenu.AddSubmenu("Projects")
	proj.AddText("Jump to Project…", keys.CmdOrCtrl("p"), send("palette"))
	proj.AddText("Add Project…", keys.CmdOrCtrl("o"), send("add-project"))
	proj.AddText("Settings…", keys.CmdOrCtrl(","), send("settings"))
	proj.AddText("Edit config.json", keys.Combo(",", keys.CmdOrCtrlKey, keys.OptionOrAltKey), send("edit-config"))
	proj.AddText("Show in Finder", keys.Combo("r", keys.CmdOrCtrlKey, keys.ShiftKey), send("reveal"))
	proj.AddText("Search Conversations…", keys.Combo("f", keys.CmdOrCtrlKey, keys.ShiftKey), send("history"))
	proj.AddText("Worktrees…", keys.Combo("g", keys.CmdOrCtrlKey, keys.ShiftKey), send("worktrees"))
	proj.AddSeparator()
	proj.AddText("Remote Access…", nil, send("remote"))
	proj.AddText("Start/Stop Remote Server", keys.Combo("r", keys.CmdOrCtrlKey, keys.OptionOrAltKey), send("remote-toggle"))

	view := appMenu.AddSubmenu("View")
	view.AddText("Toggle Projects Panel", keys.CmdOrCtrl("b"), send("toggle-left"))
	view.AddText("Toggle Stats Panel", keys.CmdOrCtrl("i"), send("toggle-right"))
	view.AddSeparator()
	view.AddText("Bigger Text", keys.CmdOrCtrl("="), send("font-up"))
	view.AddText("Smaller Text", keys.CmdOrCtrl("-"), send("font-down"))
	view.AddText("Actual Size", keys.CmdOrCtrl("0"), send("font-reset"))
	view.AddSeparator()
	for i := 1; i <= 9; i++ {
		key := string(rune('0' + i))
		view.AddText("Tab "+key, keys.CmdOrCtrl(key), send("tab-"+key))
	}
	appMenu.Append(menu.WindowMenu())

	err := wails.Run(&options.App{
		Title:            "Quarters",
		Width:            1440,
		Height:           900,
		MinWidth:         860,
		MinHeight:        520,
		BackgroundColour: &options.RGBA{R: 14, G: 14, B: 19, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		// Files dropped on a terminal are typed in as paths.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},
		Menu:        appMenu,
		OnStartup: func(c context.Context) {
			ctx = c
			app.startup(c)
		},
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		Bind:          []any{app},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			WebviewIsTransparent: false,
			About: &mac.AboutInfo{
				Title:   "Quarters",
				Message: "A home for your Claude Code sessions.",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// setPassword sets password sign-in for remote access without a window:
// Quarters --set-password <username>, then the password on standard input.
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

// serve runs Quarters without a window: only the remote-access web server
// (remote access must be set up in config.json). It prints a pairing link.
func serve(app *App) {
	app.start()
	app.mu.Lock()
	srv, err := app.remote, app.remoteErr
	app.mu.Unlock()
	if srv == nil {
		if err == "" {
			err = `remote access is off; set "remote": {"enabled": true, "url": "https://…"} in config.json`
		}
		log.Fatal(err)
	}
	fmt.Printf("Quarters is serving %s (listening on %s)\n", srv.Origin(), app.config().Remote.Listen)
	if app.config().Remote.LAN {
		_, port, _ := net.SplitHostPort(orDefault(app.config().Remote.Listen, config.DefaultListen))
		fmt.Printf("on the home network: %s\n", strings.Join(remote.LANAddresses(port), "  "))
	}
	if p, err := srv.NewPairing(); err == nil {
		fmt.Printf("pair a device (one use, 10 minutes):\n%s\n", p.URL)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	app.stop()
}
