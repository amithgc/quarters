package main

import (
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
)

// stdoutOf is what f prints.
func stdoutOf(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	f()
	os.Stdout = old
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestVersionCommand(t *testing.T) {
	for _, arg := range []string{"--version", "-v", "version"} {
		var ran bool
		out := stdoutOf(t, func() { ran = runCLI(nil, []string{arg}, true) })
		want := "Quarters " + version + " (" + runtime.GOOS + "/" + runtime.GOARCH + ", server)\n"
		if !ran || out != want {
			t.Errorf("quarters %s = %q (ran %v), want %q", arg, out, ran, want)
		}
	}
	if got := versionLine(false); strings.Contains(got, "server") {
		t.Errorf("the Mac app's version line says server: %q", got)
	}
	if help := stdoutOf(t, func() { runCLI(nil, []string{"--help"}, true) }); !strings.Contains(help, "--version") {
		t.Error("--help doesn't list --version")
	}
}
