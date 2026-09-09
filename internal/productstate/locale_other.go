//go:build !darwin

package productstate

// darwinAppleLocale is a no-op off macOS; the environment variables already
// carry the language on Linux/Windows.
func darwinAppleLocale() string { return "" }
