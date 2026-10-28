package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework Carbon
#import <Cocoa/Cocoa.h>
#import <Carbon/Carbon.h>

extern void hotkeyPressed(void);

static EventHotKeyRef hotkeyRef = NULL;
static EventHandlerRef hotkeyHandler = NULL;

static OSStatus onHotkey(EventHandlerCallRef next, EventRef event, void *data) {
	hotkeyPressed();
	return noErr;
}

// setHotkey registers the global hotkey (code < 0 removes it). It returns
// 0, or the error macOS gave (e.g. the combination is taken).
static int setHotkey(int code, int mods) {
	__block int result = 0;
	void (^work)(void) = ^{
		if (hotkeyRef != NULL) {
			UnregisterEventHotKey(hotkeyRef);
			hotkeyRef = NULL;
		}
		if (code < 0) return;
		if (hotkeyHandler == NULL) {
			EventTypeSpec spec = { kEventClassKeyboard, kEventHotKeyPressed };
			InstallApplicationEventHandler(&onHotkey, 1, &spec, NULL, &hotkeyHandler);
		}
		EventHotKeyID hid = { 'hrns', 1 };
		result = (int)RegisterEventHotKey((UInt32)code, (UInt32)mods, hid, GetApplicationEventTarget(), 0, &hotkeyRef);
	};
	if ([NSThread isMainThread]) work();
	else dispatch_sync(dispatch_get_main_queue(), work);
	return result;
}

// toggleApp hides Quarters when it is in front, else brings it forward.
static void toggleApp(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if ([NSApp isActive] && [[NSApp mainWindow] isVisible]) {
			[NSApp hide:nil];
		} else {
			[NSApp unhide:nil];
			for (NSWindow *w in [NSApp windows]) {
				if ([w canBecomeMainWindow]) { [w makeKeyAndOrderFront:nil]; break; }
			}
			[NSApp activateIgnoringOtherApps:YES];
		}
	});
}
*/
import "C"

import (
	"fmt"
	"strings"
)

// Carbon modifier flags and virtual key codes (ANSI layout).
const (
	modCmd   = 1 << 8
	modShift = 1 << 9
	modAlt   = 1 << 11
	modCtrl  = 1 << 12
)

var keyCodes = map[string]int{
	"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9, "b": 11, "q": 12,
	"w": 13, "e": 14, "r": 15, "y": 16, "t": 17, "1": 18, "2": 19, "3": 20, "4": 21, "6": 22, "5": 23,
	"9": 25, "7": 26, "8": 28, "0": 29, "o": 31, "u": 32, "i": 34, "p": 35, "l": 37, "j": 38,
	"k": 40, "n": 45, "m": 46, "space": 49, "`": 50, "return": 36, "\\": 42, ";": 41, "'": 39, ",": 43, ".": 47, "/": 44,
}

// parseHotkey reads "ctrl+alt+space" into a key code and modifiers.
func parseHotkey(s string) (code, mods int, err error) {
	code = -1
	for _, part := range strings.Split(strings.ToLower(strings.ReplaceAll(s, " ", "")), "+") {
		switch part {
		case "cmd", "command", "⌘":
			mods |= modCmd
		case "shift", "⇧":
			mods |= modShift
		case "alt", "opt", "option", "⌥":
			mods |= modAlt
		case "ctrl", "control", "⌃":
			mods |= modCtrl
		default:
			c, ok := keyCodes[part]
			if !ok || code >= 0 {
				return 0, 0, fmt.Errorf("hotkey %q: unknown key %q", s, part)
			}
			code = c
		}
	}
	if code < 0 {
		return 0, 0, fmt.Errorf("hotkey %q needs a key, e.g. ctrl+alt+space", s)
	}
	if mods == 0 {
		return 0, 0, fmt.Errorf("hotkey %q needs ctrl, alt, shift or cmd", s)
	}
	return code, mods, nil
}

// registerHotkey sets the global hotkey ("" removes it).
func registerHotkey(combo string) error {
	if combo == "" {
		C.setHotkey(-1, 0)
		return nil
	}
	code, mods, err := parseHotkey(combo)
	if err != nil {
		return err
	}
	if r := C.setHotkey(C.int(code), C.int(mods)); r != 0 {
		return fmt.Errorf("%s is taken by another app (error %d); pick another", combo, int(r))
	}
	return nil
}

// toggleApp hides Quarters when it is in front, else brings it forward.
func toggleApp() { C.toggleApp() }
