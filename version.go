package main

import (
	_ "embed"
	"encoding/json"
)

// wails.json carries the version: the release script sets it before each
// build, and macOS shows the same one in About Quarters.
//
//go:embed wails.json
var wailsJSON []byte

var version = func() string {
	var w struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if json.Unmarshal(wailsJSON, &w) != nil || w.Info.ProductVersion == "" {
		return "dev"
	}
	return w.Info.ProductVersion
}()

// Version is this build's version, e.g. "0.5.0".
func (a *App) Version() string { return version }
