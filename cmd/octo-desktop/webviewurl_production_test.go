//go:build product_production || product_test

// OCTO-FORK: test packages share the product restriction that prevents a
// user-controlled development WebView from replacing their embedded UI.

package main

import "testing"

// Product and test packages must ignore OCTO_DESKTOP_DEV_URL entirely. This is not
// "the variable happens to be unread" — it is the bypass that would let a
// release build be pointed at a dev server, so the assertion is that setting it
// has no effect at all.
//
// The gate is the selected package profile's allowDevWebview, which is false;
// Profile.Validate() rejects true for either package name outright.
func TestProductionIgnoresTheDevWebviewURL(t *testing.T) {
	t.Setenv("OCTO_DESKTOP_DEV_URL", "http://localhost:5173")
	if got, want := desktopWebviewURL(), "http://127.0.0.1:8088"; got != want {
		t.Errorf("desktopWebviewURL() = %q, want the embedded hub %q — a product "+
			"or test build must never load a dev server", got, want)
	}
}
