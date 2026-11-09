package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ---- images pasted in a browser ----
//
// Agents read pasted images from the clipboard of the machine they run on,
// which is not the clipboard of a browser elsewhere. So the browser sends the
// image here and types its path into the tab, the way a dropped file is typed
// in; Claude Code and other agents take an image's path as the image.

const (
	maxPastedImage = 3 << 20 // base64 of it still fits the browser's 4 MB message
	pastedKeepFor  = 7 * 24 * time.Hour
	pastedKeepMax  = 100 // with maxPastedImage, what a device can leave on the disk
)

// pastedExt is the extension of each image type accepted. It comes from the
// file's contents, so a browser can't choose what kind of file is written.
var pastedExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// pastedDir is where pasted images are kept: private, outside every project
// (they shouldn't show up in git), and readable by the agents you run.
func pastedDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no folder to keep the image in: %w", err)
	}
	dir := filepath.Join(base, "quarters", "pasted")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("no folder to keep the image in: %w", err)
	}
	return dir, nil
}

// SavePastedImage stores an image (base64) pasted in a browser and returns
// its path, to type into a tab.
func (a *App) SavePastedImage(data string) (string, error) {
	if base64.StdEncoding.DecodedLen(len(data)) > maxPastedImage+2 {
		return "", errors.New("the image is too big to paste")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", errors.New("the pasted image couldn't be read")
	}
	if len(raw) > maxPastedImage {
		return "", errors.New("the image is too big to paste")
	}
	ext, ok := pastedExt[http.DetectContentType(raw)]
	if !ok {
		return "", errors.New("only PNG, JPEG, GIF and WebP images can be pasted")
	}
	dir, err := pastedDir()
	if err != nil {
		return "", err
	}
	prunePasted(dir, time.Now())
	f, err := os.CreateTemp(dir, "image-*"+ext) // private to you, under a name nobody chose
	if err != nil {
		return "", fmt.Errorf("couldn't keep the image: %w", err)
	}
	_, err = f.Write(raw)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("couldn't keep the image: %w", err)
	}
	return f.Name(), nil
}

// prunePasted removes images older than pastedKeepFor, and the oldest beyond
// pastedKeepMax-1 (one is about to be added).
func prunePasted(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type image struct {
		path string
		at   time.Time
	}
	var kept []image
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || !strings.HasPrefix(e.Name(), "image-") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if now.Sub(info.ModTime()) > pastedKeepFor {
			os.Remove(p)
			continue
		}
		kept = append(kept, image{p, info.ModTime()})
	}
	if len(kept) < pastedKeepMax {
		return
	}
	slices.SortFunc(kept, func(a, b image) int { return a.at.Compare(b.at) })
	for _, im := range kept[:len(kept)-pastedKeepMax+1] {
		os.Remove(im.path)
	}
}
