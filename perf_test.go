package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// git runs again only when something changed: a commit moves gitSig, a
// session's output counts as maybe-changed, and an idle project waits for
// maxAge.
func TestGitRefreshesOnlyOnChange(t *testing.T) {
	a, dir := testApp(t)
	wait := func() {
		t.Helper()
		for i := 0; i < 200; i++ {
			a.fmu.Lock()
			busy := a.f.git[dir].busy
			a.fmu.Unlock()
			if !busy {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("git status never finished")
	}
	g := a.gitNow(dir)
	if !g.IsRepo || len(g.Files) != 0 {
		t.Fatalf("first look = %+v", g)
	}
	ran := func() time.Time {
		a.fmu.Lock()
		defer a.fmu.Unlock()
		return a.f.git[dir].at
	}
	first := ran()

	// nothing changed: no new run, however often it is asked
	for i := 0; i < 5; i++ {
		a.gitOf(dir, gitListAge)
	}
	wait()
	if !ran().Equal(first) {
		t.Fatal("git ran again with nothing changed")
	}

	// a commit changes git's files: it runs again (once it's old enough)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two\n"), 0o644)
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "second"}} {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	sig := gitSig(dir)
	a.fmu.Lock()
	if a.f.git[dir].sig == sig {
		a.fmu.Unlock()
		t.Fatal("a commit didn't change gitSig")
	}
	e := a.f.git[dir]
	e.at = e.at.Add(-gitMinAge - time.Second)
	a.f.git[dir] = e
	old := e.at
	a.fmu.Unlock()
	a.gitOf(dir, gitListAge)
	wait()
	if !ran().After(old) {
		t.Fatal("git didn't run after a commit")
	}

	// an edit from outside, no session: only once maxAge has passed
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644)
	a.fmu.Lock()
	e = a.f.git[dir]
	e.at = e.at.Add(-gitMinAge - time.Second)
	a.f.git[dir] = e
	a.fmu.Unlock()
	if g, _ := a.gitOf(dir, gitListAge); len(g.Files) != 0 {
		t.Fatal("expected the old status")
	}
	wait()
	g, _ = a.gitOf(dir, time.Second)
	wait()
	if g, _ = a.gitOf(dir, time.Second); len(g.Files) != 1 {
		t.Fatalf("after maxAge: %+v", g.Files)
	}
}

// The project list is sent to windows when it changes, not otherwise.
func TestWatchProjects(t *testing.T) {
	a, dir := testApp(t)
	b := a.watchProjects(nil)
	if b == nil {
		t.Fatal("the first look should send the list")
	}
	if c := a.watchProjects(b); &c[0] != &b[0] {
		t.Fatal("an unchanged list was sent again")
	}
	if err := os.RemoveAll(dir); err != nil { // the project's folder is gone
		t.Fatal(err)
	}
	if c := a.watchProjects(b); &c[0] == &b[0] {
		t.Fatal("a changed list wasn't sent")
	}
}
