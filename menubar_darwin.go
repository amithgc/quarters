package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

extern void menuBarClicked(int tag);

@interface QuartersMenuTarget : NSObject
- (void)clicked:(NSMenuItem *)item;
@end

@implementation QuartersMenuTarget
- (void)clicked:(NSMenuItem *)item { menuBarClicked((int)item.tag); }
@end

static NSStatusItem *qStatus = nil;
static QuartersMenuTarget *qTarget = nil;

// setMenuBar shows Quarters in the menu bar with title next to its icon,
// and a menu made of items: one per line, "tag\ttext" (a tag below 0 is a
// label, not clickable), or "-" for a separator. show 0 removes it.
static void setMenuBar(int show, const char *title, const char *items) {
	NSString *t = [[NSString alloc] initWithUTF8String:title];
	NSString *lines = [[NSString alloc] initWithUTF8String:items];
	dispatch_async(dispatch_get_main_queue(), ^{
		if (!show) {
			if (qStatus != nil) {
				[[NSStatusBar systemStatusBar] removeStatusItem:qStatus];
				[qStatus release];
				qStatus = nil;
			}
			[t release];
			[lines release];
			return;
		}
		if (qStatus == nil) {
			qStatus = [[[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength] retain];
			if (qTarget == nil) qTarget = [[QuartersMenuTarget alloc] init];
			SEL symbol = @selector(imageWithSystemSymbolName:accessibilityDescription:);
			if ([NSImage respondsToSelector:symbol]) { // macOS 11
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wunguarded-availability-new"
				NSImage *img = [NSImage imageWithSystemSymbolName:@"terminal" accessibilityDescription:@"Quarters"];
#pragma clang diagnostic pop
				[img setTemplate:YES];
				qStatus.button.image = img;
				qStatus.button.imagePosition = NSImageLeft;
			}
			qStatus.button.font = [NSFont monospacedDigitSystemFontOfSize:[NSFont systemFontSize] weight:NSFontWeightRegular];
		}
		qStatus.button.title = t.length ? [@" " stringByAppendingString:t] : @"";
		if (qStatus.button.image == nil && t.length == 0) qStatus.button.title = @"Quarters";
		NSMenu *menu = [[NSMenu alloc] init];
		menu.autoenablesItems = NO;
		for (NSString *line in [lines componentsSeparatedByString:@"\n"]) {
			if ([line isEqualToString:@"-"]) {
				[menu addItem:[NSMenuItem separatorItem]];
				continue;
			}
			NSRange tab = [line rangeOfString:@"\t"];
			if (tab.location == NSNotFound) continue;
			int tag = [[line substringToIndex:tab.location] intValue];
			NSString *text = [line substringFromIndex:tab.location + 1];
			NSMenuItem *mi = [[NSMenuItem alloc] initWithTitle:text action:(tag >= 0 ? @selector(clicked:) : NULL) keyEquivalent:@""];
			mi.tag = tag;
			if (tag >= 0) mi.target = qTarget;
			else mi.enabled = NO;
			[menu addItem:mi];
			[mi release];
		}
		qStatus.menu = menu;
		[menu release];
		[t release];
		[lines release];
	});
}
*/
import "C"

import "unsafe"

// setMenuBar shows (or, with show false, removes) Quarters in the menu bar:
// title beside its icon, and the menu's lines (see menuBarContent).
func setMenuBar(show bool, title, items string) {
	ct, ci := C.CString(title), C.CString(items)
	defer C.free(unsafe.Pointer(ct))
	defer C.free(unsafe.Pointer(ci))
	on := C.int(0)
	if show {
		on = 1
	}
	C.setMenuBar(on, ct, ci)
}
