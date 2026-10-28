//go:build server

package main

import (
	"io/fs"
	"os"
)

// The remote-access server alone, with no window, for a machine you use
// from a browser or phone (a Linux box at home, say):
// go build -tags server.
func main() {
	app := NewApp()
	app.assets, _ = fs.Sub(assets, "frontend/dist")
	runCLI(app, os.Args[1:], true)
}
