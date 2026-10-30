package projects

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// IconTypes are the image files a project icon can be.
var IconTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".svg": "image/svg+xml", ".ico": "image/x-icon", ".icns": "image/x-icns",
}

const (
	maxIconBytes = 2 << 20
	maxCached    = 256 // icons kept in memory
)

// EmojiIcon reports whether a project's icon is an emoji ("🚀") rather than
// an image: a few characters, not all plain letters, and nothing a path has.
func EmojiIcon(icon string) bool {
	if icon == "" || len(icon) > 40 || utf8.RuneCountInString(icon) > 10 || strings.ContainsAny(icon, "/\\.~ ") {
		return false
	}
	for _, r := range icon {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}

// Where projects usually keep a favicon or app icon, most likely first.
var iconPatterns = []string{
	"favicon.svg", "favicon.png", "favicon.ico",
	"public/favicon.svg", "public/favicon.png", "public/favicon.ico", "public/icon*.png", "public/logo*.svg", "public/logo*.png",
	"static/favicon.*", "src/favicon.*", "src/app/favicon.ico", "src/app/icon.*", "app/favicon.ico", "app/icon.*",
	"assets/icon*.png", "assets/logo*.png", "assets/images/icon*.png", "assets/images/logo*.png",
	"web/favicon.png", "web/icons/Icon-192.png", "build/appicon.png", "icon.png", "logo.png", "logo.svg",
	"ios/Runner/Assets.xcassets/AppIcon.appiconset/*1024*.png",
}

// FindIcons suggests icon images inside a project folder (and its direct
// sub-folders, for monorepos), up to limit.
func FindIcons(dir string, limit int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(matches []string) {
		for _, m := range matches {
			if len(out) >= limit || seen[m] {
				continue
			}
			if _, ok := IconTypes[strings.ToLower(filepath.Ext(m))]; !ok {
				continue
			}
			if st, err := os.Stat(m); err != nil || st.IsDir() || st.Size() == 0 || st.Size() > maxIconBytes {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	for _, prefix := range []string{"", "*/"} {
		for _, pat := range iconPatterns {
			matches, _ := filepath.Glob(filepath.Join(dir, prefix+pat))
			var keep []string
			for _, m := range matches {
				if !strings.Contains(m, "/node_modules/") && !strings.Contains(m, "/.git/") {
					keep = append(keep, m)
				}
			}
			add(keep)
		}
	}
	return out
}

type cachedIcon struct {
	mod  time.Time
	data string
}

var (
	iconMu    sync.Mutex
	iconCache = map[string]cachedIcon{}
)

// IconDataURL reads an icon image as a data: URL the window can show.
func IconDataURL(path string) (string, error) {
	mime, ok := IconTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return "", fmt.Errorf("not an image: %s", filepath.Base(path))
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("icon not found: %s", path)
	}
	if st.Size() > maxIconBytes {
		return "", fmt.Errorf("icon is too big (over 2 MB): %s", filepath.Base(path))
	}
	iconMu.Lock()
	c, hit := iconCache[path]
	iconMu.Unlock()
	if hit && c.mod.Equal(st.ModTime()) {
		return c.data, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	data := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
	iconMu.Lock()
	if len(iconCache) >= maxCached {
		clear(iconCache)
	}
	iconCache[path] = cachedIcon{st.ModTime(), data}
	iconMu.Unlock()
	return data, nil
}
