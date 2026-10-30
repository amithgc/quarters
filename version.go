package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
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

// serverBuild: this is the server alone (go build -tags server), not the Mac app.
var serverBuild = false

// Update is what the version in the status bar needs to know: the newest
// release, whether it's newer than this build, and how this one was installed.
type Update struct {
	Version string `json:"version"` // e.g. "0.6.1" when it's newer than this build, else ""
	Latest  string `json:"latest"`  // the newest release; "" until GitHub has answered
	URL     string `json:"url"`     // its release page
	OS      string `json:"os"`      // where Quarters runs: darwin, linux
	Arch    string `json:"arch"`
	Server  bool   `json:"server"` // the server alone (quarters --serve), not the Mac app
}

const releasesAPI = "https://api.github.com/repos/amithgc/quarters/releases/latest"

var latest struct {
	sync.Mutex
	at           time.Time
	version, url string
}

// CheckUpdate asks GitHub for the newest release, at most once an hour
// (every 15 minutes while it can't be reached). A dev build never asks.
func (a *App) CheckUpdate() Update {
	u := Update{OS: runtime.GOOS, Arch: runtime.GOARCH, Server: serverBuild}
	if version == "dev" {
		return u
	}
	latest.Lock()
	defer latest.Unlock()
	if time.Since(latest.at) > time.Hour {
		latest.at = time.Now()
		if v, url, err := fetchLatest(releasesAPI); err == nil {
			latest.version, latest.url = v, url
		} else {
			latest.at = time.Now().Add(-45 * time.Minute)
		}
	}
	u.Latest, u.URL = latest.version, latest.url
	if newerVersion(latest.version, version) {
		u.Version = latest.version
	}
	return u
}

// fetchLatest is the newest release's version ("0.6.1") and page.
func fetchLatest(api string) (string, string, error) {
	c := http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", api, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Quarters/"+version)
	res, err := c.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("releases: %s", res.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&r); err != nil {
		return "", "", err
	}
	return strings.TrimPrefix(r.Tag, "v"), r.URL, nil
}

// newerVersion: a is a later version than b ("0.10.0" is later than "0.9.2").
func newerVersion(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	if a == "" || b == "" {
		return false
	}
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}
