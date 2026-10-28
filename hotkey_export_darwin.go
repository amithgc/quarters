package main

import "C"

// hotkeyPressed is called by macOS when the global hotkey is pressed. (A
// file with //export may only declare C, so this lives on its own.)
//
//export hotkeyPressed
func hotkeyPressed() { toggleApp() }
