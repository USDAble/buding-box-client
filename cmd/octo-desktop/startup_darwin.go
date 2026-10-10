//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#import <Cocoa/Cocoa.h>
#include <libproc.h>
#include <stdlib.h>
#include <unistd.h>

static long long processStartedAtMicros(void) {
	struct proc_bsdinfo info;
	if (proc_pidinfo(getpid(), PROC_PIDTBSDINFO, 0, &info, sizeof(info)) != sizeof(info)) {
		return 0;
	}
	return info.pbi_start_tvsec * 1000000 + info.pbi_start_tvusec;
}

// OCTO-FORK: AppKit can paint startup feedback before WebKit creates its process.
static void *startupWindow(const char *title, const char *message) {
	@autoreleasepool {
	[NSApplication sharedApplication];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	NSPanel *panel = [[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, 360, 180)
		styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO];
	[panel setReleasedWhenClosed:NO];
	[panel setTitle:[NSString stringWithUTF8String:title]];
	[panel setHidesOnDeactivate:NO];
	[panel setLevel:NSNormalWindowLevel];
	[panel center];
	NSView *content = [panel contentView];
	NSProgressIndicator *spinner = [[NSProgressIndicator alloc] initWithFrame:NSMakeRect(164, 100, 32, 32)];
	[spinner setStyle:NSProgressIndicatorStyleSpinning];
	[spinner setIndeterminate:YES];
	[spinner setUsesThreadedAnimation:YES];
	[content addSubview:spinner];
	[spinner startAnimation:nil];
	[spinner release];
	NSTextField *label = [NSTextField labelWithString:[NSString stringWithUTF8String:message]];
	[label setFrame:NSMakeRect(20, 58, 320, 24)];
	[label setAlignment:NSTextAlignmentCenter];
	[content addSubview:label];
	[panel makeKeyAndOrderFront:nil];
	[panel orderFrontRegardless];
	[NSApp activateIgnoringOtherApps:YES];
	[panel displayIfNeeded];
	return panel;
	}
}

static void startupWindowMessage(void *window, const char *message) {
	@autoreleasepool {
	NSPanel *panel = (NSPanel *)window;
	for (NSView *view in [[panel contentView] subviews]) {
		if ([view isKindOfClass:[NSTextField class]]) {
			[(NSTextField *)view setStringValue:[NSString stringWithUTF8String:message]];
		}
	}
	[panel displayIfNeeded];
	}
}

static void startupWindowClose(void *window) {
	NSPanel *panel = (NSPanel *)window;
	for (NSView *view in [[panel contentView] subviews]) {
		if ([view isKindOfClass:[NSProgressIndicator class]]) {
			[(NSProgressIndicator *)view stopAnimation:nil];
		}
	}
	[panel close];
	[panel release];
}

static bool startupOnMainThread(void) { return [NSThread isMainThread]; }
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func platformProcessStartedAt() (time.Time, error) {
	micros := int64(C.processStartedAtMicros())
	if micros == 0 {
		return time.Time{}, fmt.Errorf("read process creation time")
	}
	return time.UnixMicro(micros), nil
}

// Created on main before Wails; cleanup may also come from a shutdown goroutine.
func platformStartupWindow(title, message string, _ func()) (func(), func(string), error) {
	cTitle, cMessage := C.CString(title), C.CString(message)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cMessage))
	window := C.startupWindow(cTitle, cMessage)
	if window == nil {
		return nil, nil, fmt.Errorf("create startup window")
	}
	setMessage := func(message string) {
		cMessage := C.CString(message)
		defer C.free(unsafe.Pointer(cMessage))
		C.startupWindowMessage(window, cMessage)
	}
	closeWindow := func() {
		close := func() { C.startupWindowClose(window) }
		if bool(C.startupOnMainThread()) {
			close()
		} else {
			application.InvokeSync(close)
		}
	}
	return closeWindow, setMessage, nil
}
