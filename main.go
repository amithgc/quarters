//go:build !server

// Quarters is a Mac app wrapping Claude Code: projects on the left, Claude
// Code in the middle, session stats on the right. Built with the server
// tag it is the remote-access server alone, with no window (main_server.go).
package main

import (
	"context"
	"io/fs"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func main() {
	app := NewApp()
	app.assets, _ = fs.Sub(assets, "frontend/dist")
	if runCLI(app, os.Args[1:], false) {
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
	sess.AddText("Next Waiting for You", keys.CmdOrCtrl("j"), send("next-waiting"))
	sess.AddSeparator()
	sess.AddText("Clear Scrollback", keys.CmdOrCtrl("k"), send("clear"))

	proj := appMenu.AddSubmenu("Projects")
	proj.AddText("Jump to Project…", keys.CmdOrCtrl("p"), send("palette"))
	proj.AddText("Command Palette…", keys.Combo("p", keys.CmdOrCtrlKey, keys.ShiftKey), send("commands"))
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
	view.AddText("Focus Mode", keys.Combo("return", keys.CmdOrCtrlKey, keys.ShiftKey), send("focus-mode"))
	view.AddText("Usage by Project…", keys.Combo("u", keys.CmdOrCtrlKey, keys.ShiftKey), send("usage"))
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
	help := appMenu.AddSubmenu("Help")
	help.AddText("Keyboard Shortcuts", keys.CmdOrCtrl("/"), send("shortcuts"))

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
