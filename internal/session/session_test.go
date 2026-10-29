package session

import (
	"encoding/base64"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// A start-up command is typed into a real login shell once its prompt is up,
// and the tab stays a working shell afterwards.
func TestStartTypesCommandIntoShell(t *testing.T) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		t.Skip("no $SHELL")
	}
	var mu sync.Mutex
	var out strings.Builder
	m := NewManager(func(event string, data any) {
		if o, ok := data.(Output); ok {
			b, _ := base64.StdEncoding.DecodeString(o.Data)
			mu.Lock()
			out.Write(b)
			mu.Unlock()
		}
	}, nil)
	id, err := m.Start(t.TempDir(), []string{shell, "-l"}, 100, 30, "echo QUARTERS_$((40+2))", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer m.CloseAll()
	saw := func(s string) bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(out.String(), s)
	}
	deadline := time.Now().Add(15 * time.Second)
	for !saw("QUARTERS_42") && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !saw("QUARTERS_42") {
		t.Fatalf("command output never appeared; got %q", out.String())
	}
	m.Write("test", id, "echo STILL_$((1+1))\r")
	for !saw("STILL_2") && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !saw("STILL_2") {
		t.Fatal("shell did not stay usable after the command")
	}
}

// A window joining late gets the output so far, and the offsets of later
// chunks continue from the snapshot; the window that types owns the size.
func TestSnapshotAndOwner(t *testing.T) {
	var mu sync.Mutex
	var events []string
	var chunks []Output
	m := NewManager(func(event string, data any) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
		if o, ok := data.(Output); ok {
			chunks = append(chunks, o)
		}
	}, nil)
	id, err := m.Start(t.TempDir(), []string{"/bin/cat"}, 80, 24, "", []byte(`{"key":"k1"}`), "mac")
	if err != nil {
		t.Fatal(err)
	}
	defer m.CloseAll()
	m.Write("mac", id, "hello\r")
	waitFor := func(cond func() bool) {
		deadline := time.Now().Add(5 * time.Second)
		for !cond() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
	}
	snapHas := func(s string) func() bool {
		return func() bool {
			sn, _ := m.Snapshot(id)
			b, _ := base64.StdEncoding.DecodeString(sn.Data)
			return strings.Contains(string(b), s)
		}
	}
	waitFor(snapHas("hello"))
	sn, err := m.Snapshot(id)
	if err != nil || !snapHas("hello")() {
		t.Fatalf("snapshot missing output: %+v %v", sn, err)
	}
	m.Write("web1", id, "world\r")
	waitFor(snapHas("world"))
	mu.Lock()
	var after []Output
	for _, c := range chunks {
		if c.Off >= sn.Off {
			after = append(after, c)
		}
	}
	evs := strings.Join(events, ",")
	mu.Unlock()
	if len(after) == 0 || after[0].Off != sn.Off {
		t.Errorf("chunks after the snapshot should start at %d: %+v", sn.Off, after)
	}
	if !strings.Contains(evs, "session:open") || !strings.Contains(evs, "pty:owner") {
		t.Errorf("events: %s", evs)
	}
	list := m.List()
	if len(list) != 1 || list[0].Owner != "web1" || string(list[0].Meta) != `{"key":"k1"}` {
		t.Errorf("list: %+v", list)
	}
	m.Close(id)
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(events, ","), "session:close") {
		t.Error("no session:close event")
	}
}

// Taking over a session makes its program redraw, even at the same size
// (the new window cleared its screen for the redraw).
func TestClaimRedraws(t *testing.T) {
	var mu sync.Mutex
	var out strings.Builder
	m := NewManager(func(event string, data any) {
		if o, ok := data.(Output); ok {
			b, _ := base64.StdEncoding.DecodeString(o.Data)
			mu.Lock()
			out.Write(b)
			mu.Unlock()
		}
	}, nil)
	defer m.CloseAll()
	script := `trap 'echo "WINCH $(stty size)"' WINCH; echo READY; while :; do sleep 0.05; done`
	id, err := m.Start(t.TempDir(), []string{"/bin/bash", "-c", script}, 80, 24, "", nil, "mac")
	if err != nil {
		t.Fatal(err)
	}
	wait := func(s string) bool {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			mu.Lock()
			ok := strings.Contains(out.String(), s)
			mu.Unlock()
			if ok {
				return true
			}
		}
		return false
	}
	if !wait("READY") {
		t.Fatal("script never started")
	}
	m.Claim("phone", id, 80, 24) // same size
	if !wait("WINCH 24 80") {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("no redraw signal at the same size; got %q", out.String())
	}
}

// Printed finds text in what a program wrote, past colours and line wraps.
func TestPrinted(t *testing.T) {
	m := NewManager(func(string, any) {}, nil)
	defer m.CloseAll()
	// a coloured path, wrapped across two lines with an indent, as agents do
	script := `printf '\033[1mFile:\033[0m \033[36m/tmp/q-test/scratch\r\n    pad/sample.png\033[0m (650 KB)\r\n'; sleep 5`
	id, err := m.Start(t.TempDir(), []string{"/bin/sh", "-c", script}, 80, 24, "", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !m.Printed(id, "/tmp/q-test/scratchpad/sample.png") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !m.Printed(id, "/tmp/q-test/scratchpad/sample.png") {
		t.Fatal("the wrapped, coloured path was not found")
	}
	for _, not := range []string{"/etc/hosts.png", "/tmp/q-test/other.png", "", "File: /tmp"} {
		if m.Printed(id, not) {
			t.Errorf("Printed(%q) = true", not)
		}
	}
	if m.Printed(999, "/tmp/q-test/scratchpad/sample.png") {
		t.Error("an unknown session printed it")
	}
}
