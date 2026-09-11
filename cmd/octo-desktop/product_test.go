package main

import (
	"encoding/hex"
	"strings"
	"testing"
)

// The window token reaches the window through the URL, and shellURL is the only
// place that URL is built (bridge.go:171). Upstream's TestShellURL asserts the
// exact string shellURL produces, so the token fragment has to be empty until a
// token actually exists — that is what keeps the upstream contract intact, and
// it is pinned here too rather than left implicit.

// TestWindowTokenFragmentIsEmptyUntilATokenExists pins the empty case, which is
// both the CLI path (no window, no token) and the state upstream's own test
// observes.
func TestWindowTokenFragmentIsEmptyUntilATokenExists(t *testing.T) {
	saved := windowTokenVal
	windowTokenVal = ""
	defer func() { windowTokenVal = saved }()

	if got := windowTokenFragment(); got != "" {
		t.Fatalf("windowTokenFragment() = %q, want \"\" before a token exists", got)
	}

	const base = "http://127.0.0.1:8088"
	if got, want := shellURL(base, ""), base+"/?shell=octo-desktop"; got != want {
		t.Fatalf("shellURL without a token = %q, want the upstream shape %q", got, want)
	}
}

// TestShellURLCarriesTheWindowToken pins the injection itself: the token rides
// the query, the shell marker survives next to it, and the hash stays last (the
// frontend routes on location.hash, so a fragment after the hash would be part
// of the route).
func TestShellURLCarriesTheWindowToken(t *testing.T) {
	saved := windowTokenVal
	windowTokenVal = "abc123"
	defer func() { windowTokenVal = saved }()

	const base = "http://127.0.0.1:8088"
	if got, want := shellURL(base, ""), base+"/?shell=octo-desktop&window_token=abc123"; got != want {
		t.Fatalf("shellURL = %q, want %q", got, want)
	}

	got := shellURL(base, "settings")
	if !strings.Contains(got, "shell=octo-desktop") {
		t.Fatalf("shellURL dropped the desktop marker: %q", got)
	}
	if !strings.Contains(got, "window_token=abc123") {
		t.Fatalf("shellURL dropped the window token: %q", got)
	}
	if !strings.HasSuffix(got, "#settings") {
		t.Fatalf("shellURL must keep the hash last: %q", got)
	}
}

// TestNewWindowTokenIsFreshHex pins the generator's two properties: 32 bytes of
// entropy (64 hex chars) and a fresh value per call. A constant token would
// make the gate's identity meaningless across launches.
func TestNewWindowTokenIsFreshHex(t *testing.T) {
	first, err := newWindowToken()
	if err != nil {
		t.Fatalf("newWindowToken: %v", err)
	}
	second, err := newWindowToken()
	if err != nil {
		t.Fatalf("newWindowToken: %v", err)
	}

	if len(first) != 64 {
		t.Fatalf("token length = %d, want 64 hex chars (32 bytes): %q", len(first), first)
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("token is not hex (%v): %q", err, first)
	}
	if first == second {
		t.Fatal("two tokens are identical; the generator is not random")
	}
}
