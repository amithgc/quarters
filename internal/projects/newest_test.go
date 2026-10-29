package projects

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Between full looks, a new log (the folder changes) and writes to the
// newest one are still seen.
func TestNewestLogCached(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, at time.Time) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, at, at)
		return p
	}
	base := time.Now().Add(-time.Hour)
	write("a.jsonl", base)
	b := write("b.jsonl", base.Add(time.Minute))
	if p, _ := newestLog(dir); p != b {
		t.Fatalf("newest = %s", p)
	}
	// b written to: its new time, without a full look
	later := base.Add(10 * time.Minute)
	os.Chtimes(b, later, later)
	if p, m := newestLog(dir); p != b || !m.Equal(later) {
		t.Fatalf("after a write: %s %v", p, m)
	}
	// a new log: the folder changed, so it is found
	c := write("c.jsonl", base.Add(20*time.Minute))
	os.Chtimes(dir, time.Now(), time.Now().Add(time.Second)) // some file systems keep whole seconds
	if p, _ := newestLog(dir); p != c {
		t.Fatalf("after a new log: %s", p)
	}
}
