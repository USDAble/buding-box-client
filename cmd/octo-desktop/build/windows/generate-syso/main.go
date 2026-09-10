// generate-syso builds Windows resource objects (rsrc_windows_*.syso) that
// embed the application icon, manifest, and Windows VERSIONINFO into the
// desktop exe. It is invoked by CI before each Windows go build; the .syso
// files are build artifacts and are gitignored.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/tc-hib/winres"
	winversion "github.com/tc-hib/winres/version"
)

// defaultAppVersion is used when generate-syso is run without an explicit
// version (local builds, the CI check workflows). Release builds pass the
// release version so the file-properties version matches the injected ldflags.
const defaultAppVersion = "0.0.0-dev"

func main() {
	if len(os.Args) < 4 || len(os.Args) > 5 {
		fmt.Fprintf(os.Stderr, "usage: generate-syso <arch> <icon.ico> <manifest.xml> [version]\n")
		os.Exit(1)
	}
	arch, iconPath, manifestPath := os.Args[1], os.Args[2], os.Args[3]
	appVersion := defaultAppVersion
	if len(os.Args) == 5 && strings.TrimSpace(os.Args[4]) != "" {
		appVersion = os.Args[4]
	}

	rs := winres.ResourceSet{}

	iconFile, err := os.Open(iconPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open icon: %v\n", err)
		os.Exit(1)
	}
	ico, err := winres.LoadICO(iconFile)
	iconFile.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load icon: %v\n", err)
		os.Exit(1)
	}
	if err := rs.SetIcon(winres.RT_ICON, ico); err != nil {
		fmt.Fprintf(os.Stderr, "set icon: %v\n", err)
		os.Exit(1)
	}

	rs.SetVersionInfo(buildVersionInfo(appVersion))

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read manifest: %v\n", err)
		os.Exit(1)
	}
	xmlData, err := winres.AppManifestFromXML(manifestData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse manifest: %v\n", err)
		os.Exit(1)
	}
	rs.SetManifest(xmlData)

	out, err := os.Create("rsrc_windows_" + arch + ".syso")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create syso: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()

	archMap := map[string]winres.Arch{
		"amd64": winres.ArchAMD64,
		"arm64": winres.ArchARM64,
		"386":   winres.ArchI386,
	}
	a, ok := archMap[arch]
	if !ok {
		fmt.Fprintf(os.Stderr, "unsupported arch: %s\n", arch)
		os.Exit(1)
	}
	if err := rs.WriteObject(out, a); err != nil {
		fmt.Fprintf(os.Stderr, "write syso: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated rsrc_windows_%s.syso (%s)\n", arch, appVersion)
}

// buildVersionInfo is kept separate from the resource writer so the metadata is
// unit-testable without compiling a PE executable. Values come from
// branding/brand.json via internal/brand.
//
// A single language-neutral translation block (LangNeutral), never localized:
// these are class-B fixed display values and class-C identifiers, so a Chinese
// Windows shows the same product name and description as an English one.
// Localizing VERSIONINFO would make the file properties diverge by OS language
// — the exact behaviour the branding plan §2.2.1 rules out. LangNeutral (rather
// than the en-US LangDefault) is the correct LCID here: it is language-agnostic
// by definition, and it is the block winres' SetFileVersion/SetProductVersion
// already write to, so everything lands in one block.
func buildVersionInfo(appVersion string) winversion.Info {
	cfg := brand.Load()
	vi := winversion.Info{
		FileVersion:    numericVersion(appVersion),
		ProductVersion: numericVersion(appVersion),
	}
	vi.SetFileVersion(appVersion)
	vi.SetProductVersion(appVersion)

	setVersionString(&vi, winversion.ProductName, cfg.Display(brand.DisplayWindows, brand.DisplayProductName))
	setVersionString(&vi, winversion.FileDescription, cfg.Display(brand.DisplayWindows, brand.DisplayWindowsDescription))
	setVersionString(&vi, winversion.CompanyName, cfg.TeamName(brand.DefaultLocale))
	setVersionString(&vi, winversion.LegalCopyright, cfg.CopyrightFor(brand.DefaultLocale, time.Now().Year()))
	setVersionString(&vi, winversion.InternalName, cfg.Identifier(brand.IdentifierWindowsInternalName))
	setVersionString(&vi, winversion.OriginalFilename, cfg.Identifier(brand.IdentifierExeName))
	return vi
}

func setVersionString(vi *winversion.Info, key, value string) {
	if err := vi.Set(winversion.LangNeutral, key, value); err != nil {
		panic(fmt.Sprintf("invalid Windows VERSIONINFO %s: %v", key, err))
	}
}

// numericVersion converts a dotted version string (optionally v-prefixed) into
// the four 16-bit fields of VS_FIXEDFILEINFO. Trailing components default to
// zero; an overflow or non-numeric tail stops at the last parsed component.
func numericVersion(raw string) [4]uint16 {
	var result [4]uint16
	part := 0
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "v")
	value := 0
	seenDigit := false
	for i := 0; i < len(raw) && part < len(result); i++ {
		ch := raw[i]
		switch {
		case ch >= '0' && ch <= '9':
			seenDigit = true
			value = value*10 + int(ch-'0')
			if value > 65535 {
				value = 65535
			}
		case ch == '.' && seenDigit:
			result[part] = uint16(value)
			part++
			value = 0
			seenDigit = false
		default:
			if seenDigit {
				result[part] = uint16(value)
			}
			return result
		}
	}
	if part < len(result) && seenDigit {
		result[part] = uint16(value)
	}
	return result
}
