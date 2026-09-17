//go:build product_production

package main

import "testing"

// The shipped package must ignore OCTO_DESKTOP_DEV_URL entirely. This is not
// "the variable happens to be unread" — it is the bypass that would let a
// release build be pointed at a dev server, so the assertion is that setting it
// has no effect at all.
//
// The gate is profiles/production.json's allowDevWebview, which is false, and
// Profile.Validate() rejects true for the production name outright — so a
// production build that honoured the variable would first have to be a build
// that cannot start.
func TestProductionIgnoresTheDevWebviewURL(t *testing.T) {
	t.Setenv("OCTO_DESKTOP_DEV_URL", "http://localhost:5173")
	if got, want := desktopWebviewURL(), "http://127.0.0.1:8088"; got != want {
		t.Errorf("desktopWebviewURL() = %q, want the embedded hub %q — a production "+
			"build must never load a dev server", got, want)
	}
}
