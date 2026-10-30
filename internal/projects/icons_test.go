package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amithgc/quarters/internal/config"
)

func TestFindIconsAndDataURL(t *testing.T) {
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\nfake")
	for _, f := range []string{"public/favicon.png", "web/favicon.png", "node_modules/x/favicon.png", "notes.txt"} {
		_ = os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755)
		_ = os.WriteFile(filepath.Join(dir, f), png, 0o644)
	}
	got := FindIcons(dir, 10)
	if len(got) != 2 || !strings.HasSuffix(got[0], "public/favicon.png") {
		t.Fatalf("got %v", got)
	}
	url, err := IconDataURL(got[0])
	if err != nil || !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("data url: %v %.40s", err, url)
	}
	if _, err := IconDataURL(filepath.Join(dir, "notes.txt")); err == nil {
		t.Error("a text file is not an icon")
	}
}

func TestEmojiIcon(t *testing.T) {
	for icon, want := range map[string]bool{
		"🚀": true, "🛠️": true, "👩‍💻": true, "🇮🇳": true, "★": true,
		"": false, "logo.png": false, "web/favicon.svg": false, "~/icons/a.png": false, "abc": false, "café.png": false,
	} {
		if got := EmojiIcon(icon); got != want {
			t.Errorf("EmojiIcon(%q) = %v", icon, got)
		}
	}
	if got := IconPath(config.Project{Path: "/x", Icon: "🚀"}); got != "🚀" {
		t.Errorf("IconPath = %q", got)
	}
}
