package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

static void setDockBadge(const char *label) {
	NSString *s = [[NSString alloc] initWithUTF8String:label];
	dispatch_async(dispatch_get_main_queue(), ^{
		[[NSApp dockTile] setBadgeLabel:(s.length ? s : nil)];
		[s release];
	});
}

static void bounceDock(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp requestUserAttention:NSInformationalRequest];
	});
}

static void activateApp(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp activateIgnoringOtherApps:YES];
	});
}
*/
import "C"

import "unsafe"

// setDockBadge shows label on the Dock icon ("" clears it).
func setDockBadge(label string) {
	cs := C.CString(label)
	defer C.free(unsafe.Pointer(cs))
	C.setDockBadge(cs)
}

// bounceDock bounces the Dock icon once.
func bounceDock() { C.bounceDock() }

// activateApp brings Quarters to the front.
func activateApp() { C.activateApp() }
