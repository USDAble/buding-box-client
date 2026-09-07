package brand

import (
	"strings"
	"testing"
)

func TestEmbeddedSchemaVersion(t *testing.T) {
	if got := Load().SchemaVersion; got != SupportedSchemaVersion {
		t.Fatalf("embedded schemaVersion = %d, want %d — regenerate with scripts/sync-branding.mjs", got, SupportedSchemaVersion)
	}
}

func TestNameAndShortName(t *testing.T) {
	cfg := Load()
	for _, tc := range []struct {
		locale, name, short string
	}{
		{"zh-CN", "布丁盒子", "布丁盒子"},
		{"en-US", "Pudding Box", "Pudding"},
	} {
		if got := cfg.Name(tc.locale); got != tc.name {
			t.Errorf("Name(%q) = %q, want %q", tc.locale, got, tc.name)
		}
		if got := cfg.ShortName(tc.locale); got != tc.short {
			t.Errorf("ShortName(%q) = %q, want %q", tc.locale, got, tc.short)
		}
	}
}

// The desktop shell resolves the UI language to a bare "zh"/"en" (see
// cmd/octo-desktop/lang.go resolveLang), so the bare subtag must find zh-CN.
func TestBareLanguageSubtagResolves(t *testing.T) {
	cfg := Load()
	if got := cfg.Name("zh"); got != "布丁盒子" {
		t.Errorf(`Name("zh") = %q, want 布丁盒子`, got)
	}
	if got := cfg.Name("en"); got != "Pudding Box" {
		t.Errorf(`Name("en") = %q, want "Pudding Box"`, got)
	}
	if got := cfg.ShortName("en"); got != "Pudding" {
		t.Errorf(`ShortName("en") = %q, want "Pudding"`, got)
	}
}

// Traditional Chinese falls back to the Simplified value through the shared
// language subtag. This phase ships no zh-TW entry on purpose (the product
// requirement routes Traditional systems to English at the UI layer), so this
// test pins the *mechanism*, not a promise of zh-TW support.
func TestUnlistedLocaleFallsBack(t *testing.T) {
	cfg := Load()
	if got := cfg.Name("zh-TW"); got != "布丁盒子" {
		t.Errorf(`Name("zh-TW") = %q, want the zh-CN value`, got)
	}
	if got := cfg.Name("de-DE"); got != "Pudding Box" {
		t.Errorf(`Name("de-DE") = %q, want the en-US default`, got)
	}
	if got := cfg.Name(""); got == "" {
		t.Error(`Name("") returned empty; an unknown locale must still yield a name`)
	}
}

func TestCopyrightForSubstitutesYear(t *testing.T) {
	cfg := Load()
	if raw := cfg.Copyright("zh-CN"); !strings.Contains(raw, "{year}") {
		t.Fatalf("Copyright(zh-CN) = %q, expected an unsubstituted {year} placeholder", raw)
	}
	for _, locale := range []string{"zh-CN", "en-US"} {
		got := cfg.CopyrightFor(locale, 2026)
		if strings.Contains(got, "{year}") {
			t.Errorf("CopyrightFor(%q) left the placeholder in: %q", locale, got)
		}
		if !strings.Contains(got, "2026") {
			t.Errorf("CopyrightFor(%q) = %q, want it to contain 2026", locale, got)
		}
	}
}

// Class B: one fixed value per field, deliberately not localized. The Chinese
// file description is a fixed display string, not an identifier, so it stays
// Chinese on an English Windows too.
func TestWindowsDisplayValuesAreFixed(t *testing.T) {
	cfg := Load()
	if got := cfg.Display(DisplayWindows, DisplayProductName); got != "Pudding Box" {
		t.Errorf("Display product name = %q, want %q", got, "Pudding Box")
	}
	if got := cfg.Display(DisplayWindows, DisplayWindowsDescription); got != "布丁盒子" {
		t.Errorf("Display file description = %q, want %q", got, "布丁盒子")
	}
}

// Every declared identifier constant must resolve, otherwise a call site gets
// an empty string where a path or a lock name belongs.
func TestDeclaredIdentifiersResolve(t *testing.T) {
	cfg := Load()
	for _, key := range []string{
		IdentifierExeName, IdentifierCLIExeName, IdentifierPortableDirName,
		IdentifierSingleInstanceID, IdentifierWindowsInternalName,
		IdentifierDataRoot, IdentifierWorkspaceDir, IdentifierPort,
		IdentifierCLICommand, IdentifierConfigDir, IdentifierEnvPrefix,
		IdentifierUIProtocol, IdentifierPairingProtocol, IdentifierGoModule,
		IdentifierMacBundleID, IdentifierInnoAppID, IdentifierMobileAppID,
		IdentifierInstallDir, IdentifierLinuxDesktopEntry,
	} {
		if cfg.Identifier(key) == "" {
			t.Errorf("Identifier(%q) is empty", key)
		}
	}
}

// Class C values are machine-read, so they must be printable ASCII without
// spaces on every platform. scripts/brand-schema.mjs enforces this on the
// source; this asserts the embedded copy the Go binary actually ships with.
func TestIdentifiersAreAsciiWithoutSpaces(t *testing.T) {
	cfg := Load()
	groups := map[string]map[string]string{
		"identifiers.current": cfg.Identifiers.Current,
		"identifiers.future":  cfg.Identifiers.Future,
		"visual.logo":         cfg.Visual.Logo,
		// visual.colors is absent until the brand colour is agreed; a nil map
		// here simply contributes nothing rather than failing.
		"visual.colors": cfg.Visual.Colors,
	}
	for group, links := range cfg.Links {
		groups["links."+group] = links
	}
	for group, values := range groups {
		for key, value := range values {
			for _, r := range value {
				if r < '!' || r > '~' {
					t.Errorf("%s.%s = %q contains %q; class C must be printable ASCII without spaces", group, key, value, r)
					break
				}
			}
		}
	}
}

// The single-instance lock must not collide with an Octo install on the same
// machine — that is the whole reason the identifier changes this phase.
func TestSingleInstanceIDIsRebranded(t *testing.T) {
	got := Load().Identifier(IdentifierSingleInstanceID)
	if got != "app.puddingbox.desktop" {
		t.Errorf("single instance id = %q, want app.puddingbox.desktop", got)
	}
	if strings.Contains(got, "octo") {
		t.Errorf("single instance id %q still references the upstream app", got)
	}
}

// Compatibility identifiers stay on the upstream values this phase; changing
// them breaks in-place upgrades for already-installed copies.
func TestCompatibilityIdentifiersUnchanged(t *testing.T) {
	cfg := Load()
	for key, want := range map[string]string{
		IdentifierCLICommand:  "octo",
		IdentifierConfigDir:   "~/.octo",
		IdentifierEnvPrefix:   "OCTO_",
		IdentifierGoModule:    "github.com/open-octo/octo-agent",
		IdentifierMacBundleID: "dev.octo-agent.desktop",
		IdentifierMobileAppID: "dev.octo.mobile",
	} {
		if got := cfg.Identifier(key); got != want {
			t.Errorf("Identifier(%q) = %q, want %q (must stay on the upstream value this phase)", key, got, want)
		}
	}
}

func TestLinksAndAssets(t *testing.T) {
	cfg := Load()
	if got := cfg.Link("external", "license"); !strings.HasPrefix(got, "https://") {
		t.Errorf("license link = %q, want an https URL", got)
	}
	if got := cfg.Link("external", "license"); strings.ContainsAny(got, "<>") {
		t.Errorf("license link = %q still contains a placeholder", got)
	}
	if got := cfg.Link("inApp", "terms"); !strings.HasPrefix(got, "/") {
		t.Errorf("in-app terms route = %q, want an app-relative route", got)
	}
	if got := cfg.Asset("mark"); got == "" {
		t.Error("Asset(\"mark\") is empty")
	}
}

func TestTextResolvesSharedCopy(t *testing.T) {
	cfg := Load()
	for _, locale := range []string{"zh-CN", "en-US"} {
		if got := cfg.Text("termsBody", locale); got == "" {
			t.Errorf("Text(termsBody, %q) is empty", locale)
		}
	}
	if got := cfg.Text("no-such-key", "en-US"); got != "" {
		t.Errorf("Text on an unknown key = %q, want empty", got)
	}
}

func TestLocalizeFallbackOrder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values Localized
		locale string
		want   string
	}{
		{"exact match wins", Localized{"zh-CN": "精确", "en-US": "exact"}, "zh-CN", "精确"},
		{"language subtag match", Localized{"zh-CN": "简体", "en-US": "en"}, "zh-Hant", "简体"},
		{"english default", Localized{"en-US": "en"}, "ja-JP", "en"},
		{"empty value skipped", Localized{"zh-CN": "", "en-US": "en"}, "zh-CN", "en"},
		{"any value as last resort", Localized{"ja-JP": "日本語"}, "ko-KR", "日本語"},
		{"nil map", nil, "en-US", ""},
		{"all empty", Localized{"en-US": ""}, "en-US", ""},
	} {
		if got := localize(tc.values, tc.locale); got != tc.want {
			t.Errorf("%s: localize(%v, %q) = %q, want %q", tc.name, tc.values, tc.locale, got, tc.want)
		}
	}
}
