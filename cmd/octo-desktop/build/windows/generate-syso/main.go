// generate-syso builds Windows resource objects (rsrc_windows_*.syso) that
// embed the application icon, manifest, and localized Windows VERSIONINFO into
// the octo-desktop.exe binary. It is invoked before each Windows Go build.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/tc-hib/winres"
	winversion "github.com/tc-hib/winres/version"
)

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
	fmt.Printf("Generated rsrc_windows_%s.syso with Pudding Box VERSIONINFO (%s)\n", arch, appVersion)
}

// buildVersionInfo is kept separate from the resource writer so the localized
// metadata is unit-testable without compiling a PE executable. User-facing
// values come from branding/brand.json via internal/brand; compatibility names
// remain stable where Windows needs the actual executable filename.
func buildVersionInfo(appVersion string) winversion.Info {
	cfg := brand.Load()
	vi := winversion.Info{
		FileVersion:    numericVersion(appVersion),
		ProductVersion: numericVersion(appVersion),
	}
	vi.SetProductVersion(appVersion)
	vi.SetFileVersion(appVersion)

	for _, translation := range []struct {
		langID uint16
		locale string
	}{
		{langID: winversion.LangDefault, locale: "en-US"},
		{langID: 0x0804, locale: "zh-CN"},
	} {
		name := cfg.Name(translation.locale)
		description := cfg.WindowsFileDescription(translation.locale)
		if description == "" {
			description = name
		}
		setVersionString(&vi, translation.langID, winversion.ProductName, name)
		setVersionString(&vi, translation.langID, winversion.FileDescription, description)
		setVersionString(&vi, translation.langID, winversion.CompanyName, cfg.TeamName(translation.locale))
		setVersionString(&vi, translation.langID, winversion.LegalCopyright, cfg.Copyright(translation.locale))
		setVersionString(&vi, translation.langID, winversion.InternalName, cfg.Platform.Windows.InternalName)
		setVersionString(&vi, translation.langID, winversion.OriginalFilename, cfg.Platform.Windows.OriginalFilename)
		setVersionString(&vi, translation.langID, winversion.ProductVersion, appVersion)
		setVersionString(&vi, translation.langID, winversion.FileVersion, appVersion)
	}
	return vi
}

func setVersionString(vi *winversion.Info, langID uint16, key, value string) {
	if err := vi.Set(langID, key, value); err != nil {
		panic(fmt.Sprintf("invalid Windows VERSIONINFO %s: %v", key, err))
	}
}

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
