package stats

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileImage(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "a.png")
	os.WriteFile(good, buf.Bytes(), 0o644)
	if url, err := FileImage(good); err != nil || !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("a real PNG: %q, %v", url[:min(len(url), 30)], err)
	}
	fake := filepath.Join(dir, "secret.png") // named like an image, but text
	os.WriteFile(fake, []byte("API_KEY=sk-123\n"), 0o644)
	svg := filepath.Join(dir, "a.svg")
	os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o644)
	folder := filepath.Join(dir, "x.png")
	os.Mkdir(folder, 0o755)
	for name, p := range map[string]string{"text named .png": fake, "svg": svg, "a folder": folder, "missing": filepath.Join(dir, "gone.png"), "no extension": filepath.Join(dir, "a")} {
		if _, err := FileImage(p); err == nil {
			t.Errorf("%s was read as an image", name)
		}
	}
}
