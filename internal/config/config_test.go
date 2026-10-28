package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrailingCommasAreAccepted(t *testing.T) {
	in := `{"projects": [ {"name": "a,]", "path": "/x\"},"}, ], "layout": {"left_width": 3,}, }`
	var c Config
	if err := json.Unmarshal(stripTrailingCommas([]byte(in)), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Projects) != 1 || c.Projects[0].Name != "a,]" || c.Projects[0].Path != `/x"},` || c.Layout.LeftWidth != 3 {
		t.Errorf("got %+v", c)
	}
}

func TestBrokenFileStillLoads(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "quarters", "config.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte("{\n  \"projects\": [\n    { \"path\": \"/a\" }\n    { \"path\": \"/b\" }\n  ]\n}\n"), 0o644)
	c, err := Load(nil)
	if err == nil || !strings.HasPrefix(err.Error(), "line 4:") {
		t.Fatalf("want a line-4 error, got %v", err)
	}
	if c == nil || c.Changed() {
		t.Fatal("broken config should still give a usable, up-to-date Config")
	}
}

func TestSaveProjectAddsEditsAndRefusesDuplicates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SaveProject("", Project{Name: "Games site", Path: "/tmp/a", Group: "Web"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveProject("", Project{Path: "/tmp/a"}); err == nil {
		t.Error("adding the same folder twice should fail")
	}
	c.Projects[0].Command = []string{"claude", "--model", "sonnet"}
	if err := c.SaveProject("/tmp/a", Project{Name: "Games", Path: "/tmp/b", Group: "Games"}); err != nil || c.Save() != nil {
		t.Fatal(err)
	}
	got, _ := Load(nil)
	if len(got.Projects) != 1 || got.Projects[0].Name != "Games" || got.Projects[0].Path != "/tmp/b" ||
		got.Projects[0].Group != "Games" || len(got.Projects[0].Command) != 3 {
		t.Errorf("after edit: %+v", got.Projects)
	}
	if err := got.RemoveProject("/tmp/b"); err != nil || len(got.Projects) != 0 {
		t.Errorf("remove: %v %+v", err, got.Projects)
	}
}

func TestMoveProject(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, _ := Load(nil)
	for _, p := range []string{"/tmp/a", "/tmp/b", "/tmp/c"} {
		if err := c.SaveProject("", Project{Path: p, Group: "G"}); err != nil {
			t.Fatal(err)
		}
	}
	order := func() string {
		var s []string
		for _, p := range c.Projects {
			s = append(s, p.Path+":"+p.Group)
		}
		return strings.Join(s, " ")
	}
	if err := c.MoveProject("/tmp/c", "/tmp/a", false, "G"); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "/tmp/c:G /tmp/a:G /tmp/b:G" {
		t.Fatalf("before a: %s", got)
	}
	if err := c.MoveProject("/tmp/c", "/tmp/b", true, "H"); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "/tmp/a:G /tmp/b:G /tmp/c:H" {
		t.Fatalf("after b: %s", got)
	}
	if err := c.MoveProject("/tmp/a", "", false, "H"); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "/tmp/b:G /tmp/c:H /tmp/a:H" {
		t.Fatalf("end of H: %s", got)
	}
}

func TestMigrateOld(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	old := filepath.Join(root, "harness")
	os.MkdirAll(old, 0o700)
	os.WriteFile(filepath.Join(old, "config.json"), []byte(`{"projects":[{"path":"/x"}]}`), 0o644)
	os.WriteFile(filepath.Join(old, "remote.json"), []byte(`{}`), 0o600)
	os.WriteFile(filepath.Join(old, "remote.json.tmp"), []byte(`{`), 0o600)
	if err := MigrateOld(); err != nil {
		t.Fatal(err)
	}
	c, err := Load(nil)
	if err != nil || len(c.Projects) != 1 || c.Projects[0].Path != "/x" {
		t.Fatalf("after migrating: %+v, %v", c, err)
	}
	if st, err := os.Stat(filepath.Join(root, "quarters", "remote.json")); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("remote.json: %v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(root, "quarters", "remote.json.tmp")); err == nil {
		t.Error("copied a temporary file")
	}
	// the new folder exists now: an edited old one is not copied again
	os.WriteFile(filepath.Join(old, "config.json"), []byte(`{"projects":[]}`), 0o644)
	MigrateOld()
	if c, _ := Load(nil); len(c.Projects) != 1 {
		t.Error("migrated twice")
	}
}
