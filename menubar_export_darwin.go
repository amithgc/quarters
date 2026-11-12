package main

import "C"

// menuBarClicked is called by macOS when an item of Quarters' menu bar menu
// is chosen. (A file with //export may only declare C, so this lives on its
// own.)
//
//export menuBarClicked
func menuBarClicked(tag C.int) {
	if h := menuBarHandler.Load(); h != nil {
		go (*h)(int(tag))
	}
}
