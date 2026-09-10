package main

import (
	"os"
	"testing"

	"github.com/open-octo/octo-agent/internal/productprofile"
)

func TestDesktopWebviewURL_Default(t *testing.T) {
	t.Setenv("OCTO_DESKTOP_DEV_URL", "")
	if got, want := desktopWebviewURL(), "http://"+hubAddr; got != want {
		t.Fatalf("desktopWebviewURL() = %q, want %q", got, want)
	}
}

func TestDesktopWebviewURL_DevOverride(t *testing.T) {
	t.Setenv("OCTO_DESKTOP_DEV_URL", "http://localhost:5173")
	want := "http://" + hubAddr
	if productprofile.Current().AllowDevWebview {
		want = "http://localhost:5173"
	}
	if got := desktopWebviewURL(); got != want {
		t.Fatalf("desktopWebviewURL() = %q, want %q", got, want)
	}
}

func TestDesktopWebviewURL_TrimsWhitespace(t *testing.T) {
	t.Setenv("OCTO_DESKTOP_DEV_URL", "  http://localhost:5173  ")
	want := "http://" + hubAddr
	if productprofile.Current().AllowDevWebview {
		want = "http://localhost:5173"
	}
	if got := desktopWebviewURL(); got != want {
		t.Fatalf("desktopWebviewURL() = %q, want %q", got, want)
	}
}

func TestApplyDesktopProfileRejectsProductionInputs(t *testing.T) {
	t.Setenv("OCTO_DESKTOP_DEV_URL", "http://localhost:5173")
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())
	t.Setenv("OCTO_PROVIDER", "openai")
	t.Setenv("OPENAI_API_KEY", "test-key")

	p := applyDesktopProfile()
	if !p.IsProduction() {
		return
	}
	for _, name := range []string{"OCTO_DESKTOP_DEV_URL", "OCTO_DATA_ROOT", "OCTO_PROVIDER", "OPENAI_API_KEY"} {
		if got := os.Getenv(name); got != "" {
			t.Errorf("%s = %q after production profile, want empty", name, got)
		}
	}
}
