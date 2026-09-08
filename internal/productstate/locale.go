package productstate

import (
	"os"
	"strings"
	"sync"
)

// SystemLocale returns the language the product should start in when the user
// has not chosen one (需求 §5.3.1 / §10 T10): "zh" for a Simplified-Chinese
// system, "en" for everything else — Traditional Chinese (zh-TW/zh-HK/zh-MO)
// included. The server owns the decision rather than the browser's
// navigator.language, which the webview may not populate accurately.
//
// Prefers the process environment (LANG etc.); on macOS, where a Finder-launched
// app carries no LANG, it falls back to the system AppleLocale.
var (
	systemLocaleOnce sync.Once
	systemLocale     string
)

func SystemLocale() string {
	systemLocaleOnce.Do(func() { systemLocale = detectSystemLocale() })
	return systemLocale
}

func detectSystemLocale() string {
	raw := firstEnv("LC_ALL", "LC_MESSAGES", "LANG")
	if raw == "" {
		raw = darwinAppleLocale()
	}
	raw = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "-", "_"))
	if !strings.HasPrefix(raw, "zh") {
		return "en"
	}
	// Traditional-Chinese variants are deliberately English (T10).
	if strings.Contains(raw, "tw") || strings.Contains(raw, "hk") ||
		strings.Contains(raw, "mo") || strings.Contains(raw, "hant") {
		return "en"
	}
	return "zh"
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
