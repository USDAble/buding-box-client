package main

import (
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/brand"
	winversion "github.com/tc-hib/winres/version"
)

// TestBuildVersionInfoSingleBlock is the load-bearing assertion: a localized
// VERSIONINFO would make a Chinese Windows show a different product name than
// an English one, which the branding plan §2.2.1 rules out. Exactly one
// language-neutral translation block.
func TestBuildVersionInfoSingleBlock(t *testing.T) {
	vi := buildVersionInfo("1.2.3")
	table := (&vi).Table()
	if len(table) != 1 {
		t.Fatalf("VERSIONINFO has %d translation blocks, want 1 (LangNeutral only)", len(table))
	}
	if table[winversion.LangNeutral] == nil {
		t.Fatal("VERSIONINFO has no LangNeutral block")
	}
}

func TestBuildVersionInfoUsesBrandValues(t *testing.T) {
	cfg := brand.Load()
	vi := buildVersionInfo("1.2.3")

	st := (&vi).Table()[winversion.LangNeutral]
	got := func(key string) string { return (*st)[key] }

	checks := []struct {
		key  string
		want string
	}{
		{winversion.ProductName, cfg.Display(brand.DisplayWindows, brand.DisplayProductName)},
		{winversion.FileDescription, cfg.Display(brand.DisplayWindows, brand.DisplayWindowsDescription)},
		{winversion.CompanyName, cfg.TeamName(brand.DefaultLocale)},
		{winversion.InternalName, cfg.Identifier(brand.IdentifierWindowsInternalName)},
		{winversion.OriginalFilename, cfg.Identifier(brand.IdentifierExeName)},
	}
	for _, c := range checks {
		if got(c.key) != c.want {
			t.Errorf("%s = %q, want %q", c.key, got(c.key), c.want)
		}
	}

	// Guards the guard: the whole point is that none of the pre-rename values
	// survive, and that OriginalFilename matches the shipped exe name.
	if got(winversion.ProductName) == "Octo" || got(winversion.ProductName) == "" {
		t.Errorf("ProductName = %q, want the configured product name", got(winversion.ProductName))
	}
	if got(winversion.OriginalFilename) != "PuddingBox.exe" {
		t.Errorf("OriginalFilename = %q, want PuddingBox.exe", got(winversion.OriginalFilename))
	}
	if got(winversion.InternalName) != "pudding-box-desktop" {
		t.Errorf("InternalName = %q, want pudding-box-desktop", got(winversion.InternalName))
	}

	// LegalCopyright is "© {year} <team>" with the current year filled in.
	if !strings.Contains(got(winversion.LegalCopyright), cfg.TeamName(brand.DefaultLocale)) {
		t.Errorf("LegalCopyright = %q, want it to contain the team name %q",
			got(winversion.LegalCopyright), cfg.TeamName(brand.DefaultLocale))
	}
}

func TestNumericVersion(t *testing.T) {
	cases := []struct {
		in   string
		want [4]uint16
	}{
		{"1.2.3", [4]uint16{1, 2, 3, 0}},
		{"v0.20.0", [4]uint16{0, 20, 0, 0}},
		{"1.2.3.4", [4]uint16{1, 2, 3, 4}},
		{"0.0.0-dev", [4]uint16{0, 0, 0, 0}},
		{"", [4]uint16{0, 0, 0, 0}},
		{"1", [4]uint16{1, 0, 0, 0}},
	}
	for _, c := range cases {
		if got := numericVersion(c.in); got != c.want {
			t.Errorf("numericVersion(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
