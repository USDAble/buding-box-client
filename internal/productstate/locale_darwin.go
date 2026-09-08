//go:build darwin

package productstate

import (
	"os/exec"
	"strings"
)

// darwinAppleLocale reads the user's region/language from the global
// preferences, e.g. "zh_CN" or "en_US". A Finder-launched .app has no LANG in
// its environment, so this is the macOS-specific fallback.
func darwinAppleLocale() string {
	out, err := exec.Command("defaults", "read", "-g", "AppleLocale").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
