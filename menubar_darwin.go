package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

extern void menuBarClicked(int tag);

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wunguarded-availability-new"
#pragma clang diagnostic ignored "-Wunguarded-availability"

@interface QuartersMenuTarget : NSObject
- (void)clicked:(NSMenuItem *)item;
@end

@implementation QuartersMenuTarget
- (void)clicked:(NSMenuItem *)item { menuBarClicked((int)item.tag); }
@end

// QuartersRow is a menu row's view, laid out from the top.
@interface QuartersRow : NSView
@end

@implementation QuartersRow
- (BOOL)isFlipped { return YES; }
@end

// QuartersBar is a usage bar: a rounded track filled to a fraction.
@interface QuartersBar : NSView {
	CGFloat fraction;
	NSColor *fill;
}
- (instancetype)initWithFrame:(NSRect)frame fraction:(CGFloat)f color:(NSColor *)c;
@end

@implementation QuartersBar
- (instancetype)initWithFrame:(NSRect)frame fraction:(CGFloat)f color:(NSColor *)c {
	if ((self = [super initWithFrame:frame])) {
		fraction = MAX(0, MIN(1, f));
		fill = [c retain];
	}
	return self;
}
- (void)dealloc {
	[fill release];
	[super dealloc];
}
- (void)drawRect:(NSRect)dirty {
	NSRect b = self.bounds;
	CGFloat r = b.size.height / 2;
	[[NSColor quaternaryLabelColor] setFill];
	[[NSBezierPath bezierPathWithRoundedRect:b xRadius:r yRadius:r] fill];
	if (fraction <= 0) return;
	NSRect f = b;
	f.size.width = MAX(b.size.height, round(b.size.width * fraction));
	[fill setFill];
	[[NSBezierPath bezierPathWithRoundedRect:f xRadius:r yRadius:r] fill];
}
@end

// QuartersPill is a small badge: text on a rounded, tinted capsule.
@interface QuartersPill : NSView {
	NSString *text;
}
- (instancetype)initWithText:(NSString *)t;
- (NSAttributedString *)label;
@end

@implementation QuartersPill
- (instancetype)initWithText:(NSString *)t {
	if ((self = [super initWithFrame:NSZeroRect])) {
		text = [t copy];
		NSSize s = [[self label] size];
		[self setFrameSize:NSMakeSize(ceil(s.width) + 14, 18)];
	}
	return self;
}
- (void)dealloc {
	[text release];
	[super dealloc];
}
- (NSAttributedString *)label {
	return [[[NSAttributedString alloc] initWithString:text attributes:@{
		NSFontAttributeName: [NSFont systemFontOfSize:10.5 weight:NSFontWeightSemibold],
		NSForegroundColorAttributeName: [NSColor controlAccentColor],
	}] autorelease];
}
- (void)drawRect:(NSRect)dirty {
	NSRect b = self.bounds;
	[[[NSColor controlAccentColor] colorWithAlphaComponent:0.16] setFill];
	[[NSBezierPath bezierPathWithRoundedRect:b xRadius:b.size.height / 2 yRadius:b.size.height / 2] fill];
	NSAttributedString *l = [self label];
	NSSize s = [l size];
	[l drawAtPoint:NSMakePoint(round((b.size.width - s.width) / 2), round((b.size.height - s.height) / 2))];
}
@end

static const CGFloat qWidth = 300, qPad = 14;

// qColor is a row's colour for its level ("warn", "crit"), else fallback.
static NSColor *qColor(NSString *level, NSColor *fallback) {
	if ([level isEqualToString:@"crit"]) return [NSColor systemRedColor];
	if ([level isEqualToString:@"warn"]) return [NSColor systemOrangeColor];
	return fallback;
}

// qSymbol is an SF Symbol at a point size (0: as the system draws it), in
// colour when one is given (nil before macOS 11).
static NSImage *qSymbol(NSString *name, NSColor *color, CGFloat size) {
	if (![NSImage respondsToSelector:@selector(imageWithSystemSymbolName:accessibilityDescription:)]) return nil;
	NSImage *img = [NSImage imageWithSystemSymbolName:name accessibilityDescription:nil];
	if (img == nil) return nil;
	if (size <= 0) return img; // the system's own size, as a template
	NSImageSymbolConfiguration *cfg = [NSImageSymbolConfiguration configurationWithPointSize:size weight:NSFontWeightMedium];
	BOOL tinted = color != nil && [NSImageSymbolConfiguration respondsToSelector:@selector(configurationWithHierarchicalColor:)]; // macOS 12
	if (tinted) cfg = [cfg configurationByApplyingConfiguration:[NSImageSymbolConfiguration configurationWithHierarchicalColor:color]];
	img = [img imageWithSymbolConfiguration:cfg];
	[img setTemplate:!tinted];
	return img;
}

// qLabel is a one-line text label at frame r.
static NSTextField *qLabel(NSString *s, NSFont *font, NSColor *color, NSRect r) {
	NSTextField *l = [NSTextField labelWithString:s ?: @""];
	l.font = font;
	l.textColor = color;
	l.lineBreakMode = NSLineBreakByTruncatingTail;
	l.frame = r;
	l.autoresizingMask = NSViewWidthSizable;
	return l;
}

// qViewItem is a menu item showing view.
static NSMenuItem *qViewItem(NSView *view) {
	NSMenuItem *mi = [[[NSMenuItem alloc] initWithTitle:@"" action:NULL keyEquivalent:@""] autorelease];
	view.autoresizingMask = NSViewWidthSizable;
	mi.view = view;
	return mi;
}

// qPlanRow is the menu's header: "Claude plan" and a badge with the plan.
static NSMenuItem *qPlanRow(NSString *text, NSString *plan) {
	QuartersRow *v = [[[QuartersRow alloc] initWithFrame:NSMakeRect(0, 0, qWidth, 30)] autorelease];
	[v addSubview:qLabel(text, [NSFont systemFontOfSize:13 weight:NSFontWeightBold], [NSColor labelColor],
		NSMakeRect(qPad, 7, qWidth - 2 * qPad - 70, 17))];
	if (plan.length) {
		QuartersPill *p = [[[QuartersPill alloc] initWithText:plan] autorelease];
		NSRect f = p.frame;
		f.origin = NSMakePoint(qWidth - qPad - f.size.width, 6);
		p.frame = f;
		p.autoresizingMask = NSViewMinXMargin;
		[v addSubview:p];
	}
	return qViewItem(v);
}

// qLimitRow is one limit: its name and percent, a usage bar and when it
// resets.
static NSMenuItem *qLimitRow(NSString *text, NSString *detail, double percent, NSString *level) {
	CGFloat w = qWidth - 2 * qPad;
	QuartersRow *v = [[[QuartersRow alloc] initWithFrame:NSMakeRect(0, 0, qWidth, detail.length ? 54 : 40)] autorelease];
	[v addSubview:qLabel(text, [NSFont systemFontOfSize:13], [NSColor labelColor], NSMakeRect(qPad, 4, w - 54, 17))];
	NSTextField *pct = qLabel([NSString stringWithFormat:@"%d%%", (int)percent],
		[NSFont monospacedDigitSystemFontOfSize:13 weight:NSFontWeightSemibold], qColor(level, [NSColor labelColor]),
		NSMakeRect(qWidth - qPad - 50, 4, 50, 17));
	pct.alignment = NSTextAlignmentRight;
	pct.autoresizingMask = NSViewMinXMargin;
	[v addSubview:pct];
	QuartersBar *bar = [[[QuartersBar alloc] initWithFrame:NSMakeRect(qPad, 25, w, 6) fraction:percent / 100
		color:qColor(level, [NSColor controlAccentColor])] autorelease];
	bar.autoresizingMask = NSViewWidthSizable;
	[v addSubview:bar];
	if (detail.length)
		[v addSubview:qLabel(detail, [NSFont systemFontOfSize:11], [NSColor secondaryLabelColor], NSMakeRect(qPad, 35, w, 14))];
	return qViewItem(v);
}

// qStatusRow is an icon beside a title and a quieter line under it (for
// "all caught up" and warnings).
static NSMenuItem *qStatusRow(NSString *symbol, NSColor *tint, NSString *text, NSString *detail) {
	CGFloat x = qPad + 26, w = qWidth - x - qPad;
	NSTextField *d = [NSTextField wrappingLabelWithString:detail ?: @""];
	d.font = [NSFont systemFontOfSize:11];
	d.textColor = [NSColor secondaryLabelColor];
	d.maximumNumberOfLines = 3;
	CGFloat dh = detail.length ? ceil([d.cell cellSizeForBounds:NSMakeRect(0, 0, w, 1000)].height) : 0;
	QuartersRow *v = [[[QuartersRow alloc] initWithFrame:NSMakeRect(0, 0, qWidth, 30 + dh)] autorelease];
	NSImage *img = qSymbol(symbol, tint, 15);
	if (img != nil) {
		NSImageView *iv = [NSImageView imageViewWithImage:img];
		iv.frame = NSMakeRect(qPad, 6, 20, 20);
		[v addSubview:iv];
	}
	[v addSubview:qLabel(text, [NSFont systemFontOfSize:13 weight:NSFontWeightMedium], [NSColor labelColor], NSMakeRect(x, 6, w, 17))];
	if (dh > 0) {
		d.frame = NSMakeRect(x, 24, w, dh);
		d.autoresizingMask = NSViewWidthSizable;
		[v addSubview:d];
	}
	return qViewItem(v);
}

// qSection is a section header ("Waiting for you").
static NSMenuItem *qSection(NSString *text) {
	if ([NSMenuItem respondsToSelector:@selector(sectionHeaderWithTitle:)]) return [NSMenuItem sectionHeaderWithTitle:text]; // macOS 14
	NSMenuItem *mi = [[[NSMenuItem alloc] initWithTitle:text action:NULL keyEquivalent:@""] autorelease];
	mi.attributedTitle = [[[NSAttributedString alloc] initWithString:text attributes:@{
		NSFontAttributeName: [NSFont systemFontOfSize:11 weight:NSFontWeightSemibold],
		NSForegroundColorAttributeName: [NSColor secondaryLabelColor],
	}] autorelease];
	mi.enabled = NO;
	return mi;
}

// qAction is a clickable item that calls menuBarClicked with tag.
static NSMenuItem *qAction(NSString *text, NSString *detail, NSImage *img, int tag, QuartersMenuTarget *target) {
	NSMenuItem *mi = [[[NSMenuItem alloc] initWithTitle:text action:@selector(clicked:) keyEquivalent:@""] autorelease];
	mi.tag = tag;
	mi.target = target;
	mi.image = img;
	if (detail.length) {
		if ([mi respondsToSelector:@selector(setSubtitle:)]) mi.subtitle = detail; // macOS 14.4
		else mi.title = [NSString stringWithFormat:@"%@ — %@", text, detail];
	}
	return mi;
}

static NSStatusItem *qStatus = nil;
static QuartersMenuTarget *qTarget = nil;

// qMenu builds the menu from its rows (see menuRow in menubar.go).
static NSMenu *qMenu(NSArray *rows) {
	NSMenu *menu = [[[NSMenu alloc] init] autorelease];
	menu.autoenablesItems = NO;
	menu.minimumWidth = qWidth;
	for (NSDictionary *r in rows) {
		if (![r isKindOfClass:[NSDictionary class]]) continue;
		NSString *kind = r[@"kind"], *text = r[@"text"] ?: @"", *detail = r[@"detail"] ?: @"", *level = r[@"level"] ?: @"";
		NSMenuItem *mi = nil;
		if ([kind isEqualToString:@"sep"]) mi = [NSMenuItem separatorItem];
		else if ([kind isEqualToString:@"plan"]) mi = qPlanRow(text, detail);
		else if ([kind isEqualToString:@"limit"]) mi = qLimitRow(text, detail, [r[@"percent"] doubleValue], level);
		else if ([kind isEqualToString:@"note"]) mi = qStatusRow(@"exclamationmark.triangle.fill", qColor(level, [NSColor systemOrangeColor]), text, detail);
		else if ([kind isEqualToString:@"empty"]) mi = qStatusRow(@"checkmark.circle.fill", [NSColor systemGreenColor], text, detail);
		else if ([kind isEqualToString:@"section"]) mi = qSection(text);
		else if ([kind isEqualToString:@"tab"]) {
			BOOL asks = [r[@"asks"] boolValue];
			NSImage *img = asks ? qSymbol(@"questionmark.bubble.fill", [NSColor systemOrangeColor], 14)
			                    : qSymbol(@"checkmark.bubble.fill", [NSColor controlAccentColor], 14);
			mi = qAction(text, detail, img, [r[@"tag"] intValue], qTarget);
		} else if ([kind isEqualToString:@"open"]) {
			mi = qAction(text, nil, qSymbol(@"macwindow", nil, 13), 0, qTarget);
			mi.keyEquivalent = @"o";
		}
		if (mi != nil) [menu addItem:mi];
	}
	return menu;
}

// setMenuBar shows Quarters in the menu bar with title next to its icon,
// and a menu made of rows (a JSON array of menuRow). show 0 removes it.
static void setMenuBar(int show, const char *title, const char *rows) {
	NSString *t = [[NSString alloc] initWithUTF8String:title];
	NSData *data = [[NSData alloc] initWithBytes:rows length:strlen(rows)];
	dispatch_async(dispatch_get_main_queue(), ^{
		if (!show) {
			if (qStatus != nil) {
				[[NSStatusBar systemStatusBar] removeStatusItem:qStatus];
				[qStatus release];
				qStatus = nil;
			}
			[t release];
			[data release];
			return;
		}
		if (qStatus == nil) {
			qStatus = [[[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength] retain];
			if (qTarget == nil) qTarget = [[QuartersMenuTarget alloc] init];
			NSImage *img = qSymbol(@"terminal", nil, 0);
			if (img != nil) {
				img.accessibilityDescription = @"Quarters";
				qStatus.button.image = img;
				qStatus.button.imagePosition = NSImageLeft;
			}
			qStatus.button.font = [NSFont monospacedDigitSystemFontOfSize:[NSFont systemFontSize] weight:NSFontWeightRegular];
		}
		qStatus.button.title = t.length ? [@" " stringByAppendingString:t] : @"";
		if (qStatus.button.image == nil && t.length == 0) qStatus.button.title = @"Quarters";
		NSArray *list = data.length ? [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] : nil;
		qStatus.menu = qMenu([list isKindOfClass:[NSArray class]] ? list : @[]);
		[t release];
		[data release];
	});
}

#pragma clang diagnostic pop
*/
import "C"

import "unsafe"

// setMenuBar shows (or, with show false, removes) Quarters in the menu bar:
// title beside its icon, and the menu's rows as JSON (see menuBarContent).
func setMenuBar(show bool, title, rows string) {
	ct, cr := C.CString(title), C.CString(rows)
	defer C.free(unsafe.Pointer(ct))
	defer C.free(unsafe.Pointer(cr))
	on := C.int(0)
	if show {
		on = 1
	}
	C.setMenuBar(on, ct, cr)
}
