package main

import (
	"testing"

	winversion "github.com/tc-hib/winres/version"
)

func TestNumericVersion(t *testing.T) {
	if got, want := numericVersion("v1.2.3-beta"), [4]uint16{1, 2, 3, 0}; got != want {
		t.Fatalf("numericVersion = %#v, want %#v", got, want)
	}
}

func TestBuildVersionInfoContainsLocalizedProductMetadata(t *testing.T) {
	vi := buildVersionInfo("1.2.3")
	if got, want := vi.FileVersion, [4]uint16{1, 2, 3, 0}; got != want {
		t.Fatalf("FileVersion = %#v, want %#v", got, want)
	}
	for _, tc := range []struct {
		lang        uint16
		name        string
		description string
	}{
		{winversion.LangDefault, "Pudding Box", "Pudding Box Desktop"},
		{0x0804, "布丁盒子", "布丁盒子桌面端"},
	} {
		table, ok := vi.Table()[tc.lang]
		if !ok || table == nil {
			t.Fatalf("missing VERSIONINFO translation %#x", tc.lang)
		}
		if got := (*table)[winversion.ProductName]; got != tc.name {
			t.Errorf("ProductName[%#x] = %q, want %q", tc.lang, got, tc.name)
		}
		if got := (*table)[winversion.FileDescription]; got != tc.description {
			t.Errorf("FileDescription[%#x] = %q, want %q", tc.lang, got, tc.description)
		}
		if got := (*table)[winversion.InternalName]; got != "pudding-box-desktop" {
			t.Errorf("InternalName[%#x] = %q, want pudding-box-desktop", tc.lang, got)
		}
		if got := (*table)[winversion.OriginalFilename]; got != "octo-desktop.exe" {
			t.Errorf("OriginalFilename[%#x] = %q, want octo-desktop.exe", tc.lang, got)
		}
	}
}
