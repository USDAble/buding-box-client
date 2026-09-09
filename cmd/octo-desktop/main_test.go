package main

import "testing"

func TestDesktopWebviewURL_Default(t *testing.T) {
	t.Setenv("OCTO_DESKTOP_DEV_URL", "")
	if got, want := desktopWebviewURL(), "http://"+hubAddr; got != want {
		t.Fatalf("desktopWebviewURL() = %q, want %q", got, want)
	}
}

func TestDesktopWebviewURL_DevOverride(t *testing.T) {
	t.Setenv("OCTO_DESKTOP_DEV_URL", "http://localhost:5173")
	if got, want := desktopWebviewURL(), "http://localhost:5173"; got != want {
		t.Fatalf("desktopWebviewURL() = %q, want %q", got, want)
	}
}

func TestDesktopWebviewURL_TrimsWhitespace(t *testing.T) {
	t.Setenv("OCTO_DESKTOP_DEV_URL", "  http://localhost:5173  ")
	if got, want := desktopWebviewURL(), "http://localhost:5173"; got != want {
		t.Fatalf("desktopWebviewURL() = %q, want %q", got, want)
	}
}
