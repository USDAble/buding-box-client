//go:build !windows

package main

// webview2Available reports whether the WebView2 Evergreen Runtime is
// installed. WebView2 is Windows-only; every other platform ships its own
// webview (WKWebView on macOS, WebKitGTK on Linux) with the OS or the desktop
// environment, so there is nothing to detect and the boot check always passes.
func webview2Available() bool { return true }
