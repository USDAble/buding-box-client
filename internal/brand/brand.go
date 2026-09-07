// Package brand exposes the product identity that every user-visible Go
// surface reads from: window titles, tray menus, dialogs, notifications and
// Windows resource metadata.
//
// The source of truth is branding/brand.json. internal/brand/brand.json is
// generated from it by scripts/sync-branding.mjs so this package can go:embed
// the same bytes — a packaged binary never reads a runtime path and therefore
// cannot be rebranded by editing a file next to the executable.
//
// Values fall into three classes (see the branding plan under
// dev-docs-usdable/需求/2260906/), and the accessors mirror them so a caller
// cannot ask for the wrong shape:
//
//	A localized copy      Name, ShortName, Tagline, TeamName, Copyright, Text
//	B fixed display value  Display
//	C identifier or path   Identifier, FutureIdentifier, Link, Asset, Color
//
// Only class A takes a locale. Identifiers and paths are single ASCII strings
// by construction — asking for a localized executable name is not expressible.
package brand

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// SupportedSchemaVersion is the brand.json layout this package understands.
// Load panics on a mismatch rather than silently handing out zero values for
// fields that moved; TestEmbeddedSchemaVersion turns that into a CI failure.
const SupportedSchemaVersion = 2

// DefaultLocale is the fallback used when a locale carries no value.
const DefaultLocale = "en-US"

// Identifier keys. Declared as constants so a typo is caught by
// TestDeclaredIdentifiersResolve instead of yielding an empty string at a call
// site that then renders a blank window title.
const (
	IdentifierExeName             = "exeName"
	IdentifierCLIExeName          = "cliExeName"
	IdentifierPortableDirName     = "portableDirName"
	IdentifierSingleInstanceID    = "singleInstanceId"
	IdentifierWindowsInternalName = "windowsInternalName"
	IdentifierDataRoot            = "dataRoot"
	IdentifierWorkspaceDir        = "workspaceDir"
	IdentifierPort                = "port"
	IdentifierCLICommand          = "cliCommand"
	IdentifierConfigDir           = "configDir"
	IdentifierEnvPrefix           = "envPrefix"
	IdentifierUIProtocol          = "uiProtocol"
	IdentifierPairingProtocol     = "pairingProtocol"
	IdentifierGoModule            = "goModule"
	IdentifierMacBundleID         = "macBundleId"
	IdentifierInnoAppID           = "innoAppId"
	IdentifierMobileAppID         = "mobileAppId"
	IdentifierInstallDir          = "installDir"
	IdentifierLinuxDesktopEntry   = "linuxDesktopEntry"
)

// Windows display groups and keys (class B: fixed, never localized).
const (
	DisplayWindows            = "windows"
	DisplayProductName        = "productName"
	DisplayWindowsDescription = "fileDescription"
)

// Localized maps a BCP-47 locale tag to one string.
type Localized map[string]string

// Config is the whole brand configuration.
type Config struct {
	SchemaVersion int                          `json:"schemaVersion"`
	BrandID       string                       `json:"brandId"`
	Product       Product                      `json:"product"`
	About         About                        `json:"about"`
	Copy          map[string]Localized         `json:"copy"`
	DisplayGroups map[string]map[string]string `json:"display"`
	Identifiers   Identifiers                  `json:"identifiers"`
	Links         map[string]map[string]string `json:"links"`
	Visual        Visual                       `json:"visual"`
}

// Product holds the names and marketing copy (class A).
type Product struct {
	Names       Localized `json:"names"`
	ShortName   Localized `json:"shortName"`
	Tagline     Localized `json:"tagline"`
	Description Localized `json:"description"`
}

// About holds attribution and legal titles (class A).
type About struct {
	TeamName     Localized `json:"teamName"`
	Copyright    Localized `json:"copyright"`
	TermsTitle   Localized `json:"termsTitle"`
	PrivacyTitle Localized `json:"privacyTitle"`
}

// Identifiers splits what this release actually uses from what a future
// rebrand would switch to. Current is the only set consumers may read; Future
// exists so "not changed yet" is recorded rather than looking like an omission.
type Identifiers struct {
	Current map[string]string `json:"current"`
	Future  map[string]string `json:"future"`
}

// Visual holds asset paths and colours (class C).
type Visual struct {
	Logo   map[string]string `json:"logo"`
	Colors map[string]string `json:"colors"`
}

//go:embed brand.json
var embedded []byte

var (
	once   sync.Once
	loaded Config
)

// Load returns the embedded configuration. Safe for concurrent use.
func Load() Config {
	once.Do(func() {
		if err := json.Unmarshal(embedded, &loaded); err != nil {
			panic("brand: invalid embedded configuration: " + err.Error())
		}
		if loaded.SchemaVersion != SupportedSchemaVersion {
			panic(fmt.Sprintf(
				"brand: embedded brand.json is schemaVersion %d, this build understands %d — regenerate with scripts/sync-branding.mjs",
				loaded.SchemaVersion, SupportedSchemaVersion,
			))
		}
	})
	return loaded
}

// Name resolves the full product name, e.g. "布丁盒子" or "Pudding Box".
func (c Config) Name(locale string) string { return localize(c.Product.Names, locale) }

// ShortName resolves the short product name, used where a full name would be
// truncated: the tray tooltip, tray menu verbs and notification titles.
func (c Config) ShortName(locale string) string {
	if short := localize(c.Product.ShortName, locale); short != "" {
		return short
	}
	return c.Name(locale)
}

// Tagline resolves the one-line product description.
func (c Config) Tagline(locale string) string { return localize(c.Product.Tagline, locale) }

// Description resolves the long product description.
func (c Config) Description(locale string) string { return localize(c.Product.Description, locale) }

// TeamName resolves the publisher shown in About and in Windows CompanyName.
func (c Config) TeamName(locale string) string { return localize(c.About.TeamName, locale) }

// Copyright resolves the copyright line, which still contains the {year}
// placeholder. Prefer CopyrightFor unless the caller substitutes it itself.
func (c Config) Copyright(locale string) string { return localize(c.About.Copyright, locale) }

// CopyrightFor resolves the copyright line with {year} substituted, so every
// caller does not reimplement the same replacement.
func (c Config) CopyrightFor(locale string, year int) string {
	return strings.ReplaceAll(c.Copyright(locale), "{year}", strconv.Itoa(year))
}

// TermsTitle resolves the user agreement title.
func (c Config) TermsTitle(locale string) string { return localize(c.About.TermsTitle, locale) }

// PrivacyTitle resolves the privacy policy title.
func (c Config) PrivacyTitle(locale string) string { return localize(c.About.PrivacyTitle, locale) }

// Text resolves one entry from the shared copy section. Web UI strings live in
// the web i18n dictionaries; this is for copy reused outside them, such as the
// in-app legal placeholder pages.
func (c Config) Text(key, locale string) string { return localize(c.Copy[key], locale) }

// Display returns a class-B fixed display value. It takes no locale on
// purpose: these surface in OS-rendered metadata that is not re-rendered when
// the UI language changes, so they are one fixed value per field.
func (c Config) Display(group, key string) string { return c.DisplayGroups[group][key] }

// Identifier returns a class-C identifier or path from the current set.
func (c Config) Identifier(key string) string { return c.Identifiers.Current[key] }

// FutureIdentifier returns the value a future rebrand would use. Recorded for
// planning only — shipping code must read Identifier.
func (c Config) FutureIdentifier(key string) string { return c.Identifiers.Future[key] }

// Link returns a URL or in-app route, e.g. Link("external", "license").
func (c Config) Link(group, key string) string { return c.Links[group][key] }

// Asset returns a brand asset path relative to branding/, e.g. Asset("mark").
func (c Config) Asset(key string) string { return c.Visual.Logo[key] }

// Color returns a brand colour, e.g. Color("primary").
func (c Config) Color(key string) string { return c.Visual.Colors[key] }

// localize resolves a locale against a value map: exact tag, then any tag
// sharing the language subtag, then the English default, then any non-empty
// value. The desktop shell passes bare "zh"/"en", so the language-subtag step
// is what makes "zh" find "zh-CN".
func localize(values Localized, locale string) string {
	if len(values) == 0 {
		return ""
	}
	if value := values[locale]; value != "" {
		return value
	}
	language := locale
	if idx := strings.IndexByte(language, '-'); idx >= 0 {
		language = language[:idx]
	}
	if language != "" {
		for tag, value := range values {
			if value == "" {
				continue
			}
			if tag == language || strings.HasPrefix(tag, language+"-") {
				return value
			}
		}
	}
	if value := values[DefaultLocale]; value != "" {
		return value
	}
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
