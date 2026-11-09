package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/amithgc/quarters/internal/remote"
)

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func pastedHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
}

func TestSavePastedImage(t *testing.T) {
	pastedHome(t)
	a := &App{}
	dir, err := pastedDir()
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.SavePastedImage(base64.StdEncoding.EncodeToString(pngHeader))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != dir || !strings.HasSuffix(p, ".png") {
		t.Errorf("kept as %s, not a .png in %s", p, dir)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("readable by others: %v", st.Mode())
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
		t.Errorf("folder open to others: %v", d.Mode())
	}
}

// What is kept depends on the contents: never a script or a page named an image.
func TestSavePastedImageRefuses(t *testing.T) {
	pastedHome(t)
	a := &App{}
	big := append(slices.Clone(pngHeader), make([]byte, maxPastedImage)...)
	for name, data := range map[string]string{
		"not base64": "%%%",
		"text":       base64.StdEncoding.EncodeToString([]byte("#!/bin/sh\nrm -rf ~\n")),
		"svg":        base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>x</script></svg>`)),
		"empty":      "",
		"too big":    base64.StdEncoding.EncodeToString(big),
	} {
		if p, err := a.SavePastedImage(data); err == nil {
			t.Errorf("%s: kept as %s", name, p)
		}
	}
	dir, _ := pastedDir()
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

func TestPrunePasted(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	add := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, pngHeader, 0o600)
		at := now.Add(-age)
		os.Chtimes(p, at, at)
		return p
	}
	stale := add("image-stale.png", pastedKeepFor+time.Hour)
	other := add("notes.txt", pastedKeepFor+time.Hour)
	oldest := add("image-oldest.png", 3*time.Hour)
	for i := range pastedKeepMax - 1 {
		add(fmt.Sprintf("image-%d.png", i), time.Hour)
	}
	prunePasted(dir, now)
	if _, err := os.Stat(stale); err == nil {
		t.Error("a week-old image stayed")
	}
	if _, err := os.Stat(oldest); err == nil {
		t.Error("the oldest image stayed with the folder full")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("a file that isn't a pasted image was removed")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "image-*")); len(left) != pastedKeepMax-1 {
		t.Errorf("%d images left, want room for one more of %d", len(left), pastedKeepMax)
	}
}

// Pasting is typing: not for a watch-only device.
func TestPasteImageNeedsTyping(t *testing.T) {
	pastedHome(t)
	data, _ := json.Marshal(base64.StdEncoding.EncodeToString(pngHeader))
	args := []json.RawMessage{data}
	view := remoteBackend{a: &App{}, access: func(string) string { return remote.AccessView }}
	if _, err := view.Call("web1", "SavePastedImage", args); err == nil {
		t.Error("a watch-only device pasted an image")
	}
	typ := remoteBackend{a: &App{}, access: func(string) string { return remote.AccessType }}
	if _, err := typ.Call("web1", "SavePastedImage", args); err != nil {
		t.Errorf("a device that may type couldn't paste: %v", err)
	}
}

// Review lists each new file, where the side panel shows a new folder as one row.
func TestChangedFilesListsNewFolders(t *testing.T) {
	a, dir := testApp(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "new", "deep"), 0o755)
	os.WriteFile(filepath.Join(dir, "new", "b.txt"), []byte("b\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new", "deep", "c.txt"), []byte("c\n"), 0o644)

	list, err := a.ChangedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range list {
		paths = append(paths, f.Path)
	}
	slices.Sort(paths)
	if want := []string{"a.txt", "new/b.txt", "new/deep/c.txt"}; !slices.Equal(paths, want) {
		t.Errorf("listed %v, want %v", paths, want)
	}
	if _, err := a.ChangedFiles(t.TempDir()); err == nil {
		t.Error("listed the changes of a folder that isn't a project")
	}
}
