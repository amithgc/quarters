package main

import "testing"

func TestHotkeyParse(t *testing.T) {
	if c, m, err := parseHotkey("ctrl+alt+space"); err != nil || c != 49 || m != modCtrl|modAlt {
		t.Errorf("= %d %d %v", c, m, err)
	}
	for _, bad := range []string{"space", "ctrl+", "ctrl+f13", "ctrl+a+b"} {
		if _, _, err := parseHotkey(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
