//go:build !product_production && !product_test

// OCTO-FORK: product_test is a sealed package profile and must not run the
// developer-only WebView override checks in this file.

package main

import "testing"

// OCTO_DESKTOP_DEV_URL is what lets `make desktop-dev` load the Vite dev server
// in the real desktop window — the only way to debug what needs the shell
// (tray, the product gate's window-token path, native dialogs, the freeze
// overlay) with web/ edits hot-reloading instead of a full re-package.
//
// The override is a developer-profile capability, not a general one. This file
// asserts the developer half; the production build compiles the other half
// (webviewurl_production_test.go, `product_production` tag), because the
// profile is a compile-time choice and a single test binary cannot be both.
//
// The test binary is built without either package tag, so Current() is the
// developer profile here — profiles/developer.json sets allowDevWebview true.
func TestDeveloperProfileHonoursTheDevWebviewURL(t *testing.T) {
	const v = "http://localhost:5173"
	t.Setenv("OCTO_DESKTOP_DEV_URL", v)
	if got := desktopWebviewURL(); got != v {
		t.Errorf("desktopWebviewURL() = %q, want %q — `make desktop-dev` would "+
			"open the embedded build instead of the Vite server", got, v)
	}
}

// A blank value falls back to the hub rather than becoming the window's URL.
// `OCTO_DESKTOP_DEV_URL= make desktop-dev` (or an exported-but-empty variable)
// would otherwise hand the webview an empty string: a blank window with nothing
// in the log to say why.
//
// The empty string is also the "not set" case the override has to leave alone —
// `make desktop` and `make desktop-dev` share this one code path, so the default
// must stay the embedded UI.
func TestBlankDevWebviewURLFallsBackToTheHub(t *testing.T) {
	for _, v := range []string{"", "   ", "\t"} {
		t.Setenv("OCTO_DESKTOP_DEV_URL", v)
		if got, want := desktopWebviewURL(), "http://127.0.0.1:8088"; got != want {
			t.Errorf("desktopWebviewURL() with %q = %q, want the hub %q", v, got, want)
		}
	}
}
