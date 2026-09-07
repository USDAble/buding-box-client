// Package datapath is the single place that resolves product data paths.
//
// This fork ships a portable product (buding-box-client): every byte it
// writes must land next to the executable under data/, never in the host
// user's home directory. os.UserHomeDir() and the .octo path are banned
// everywhere else — scripts/datapath-guard.mjs enforces that with zero
// exceptions, and this package is the only legitimate place either appears.
//
// There is deliberately no fallback: if data/ is not usable the call fails
// and the caller decides what to tell the user (P2 turns that into a startup
// error). A degraded home-directory write would silently break the "host
// stays clean" acceptance item while looking fine on a developer machine.
//
// See dev-docs-usdable/需求/2260906/技术方案/P1-便携数据根.md.
package datapath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// dataRootEnv overrides the data root for tests, development, and a CLI
// installed into a read-only directory. It is an explicit override, not a
// fallback location (P1 §3.2).
const dataRootEnv = "OCTO_DATA_ROOT"

// exePath returns the absolute path of the current executable. It is a
// package variable so tests can point it at a fake program directory.
var exePath = os.Executable

// resolveExeDir returns the directory of the executable with symlinks
// resolved. A packaged .app on macOS runs through a symlink; without the
// resolution the data root would drift with wherever the link points.
func resolveExeDir() (string, error) {
	exe, err := exePath()
	if err != nil {
		return "", fmt.Errorf("datapath: locate the executable: %w", err)
	}
	real, err := filepath.EvalSymlinks(exe)
	if err == nil {
		exe = real
	}
	dir, err := filepath.Abs(filepath.Dir(exe))
	if err != nil {
		return "", fmt.Errorf("datapath: resolve the program directory: %w", err)
	}
	return filepath.Clean(dir), nil
}

// resolveRoot returns the data root path without creating or probing it.
// Join uses this so reading a path has no write side effects.
func resolveRoot() (string, error) {
	if env := os.Getenv(dataRootEnv); strings.TrimSpace(env) != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("datapath: resolve $%s: %w", dataRootEnv, err)
		}
		return filepath.Clean(abs), nil
	}
	exeDir, err := resolveExeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(exeDir, "data"), nil
}

// Root returns the absolute data root path, creating it if necessary.
// Resolution order: $OCTO_DATA_ROOT if set, otherwise <program dir>/data.
// An unusable root is an error — never a fallback to a host directory.
func Root() (string, error) {
	root, err := resolveRoot()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("datapath: create the data root %s: %w", root, err)
	}
	// MkdirAll is a no-op on an existing read-only root, so probe actual
	// writability to keep the no-fallback promise honest.
	probe, err := os.CreateTemp(root, ".datapath-write-probe-*")
	if err != nil {
		return "", fmt.Errorf("datapath: the data root %s is not writable: %w", root, err)
	}
	name := probe.Name()
	if err := probe.Close(); err == nil {
		_ = os.Remove(name)
	}
	return root, nil
}

// ProgramDir returns the directory holding the executable (= the parent of
// the default data root). Desktop startup pins the process cwd here (P1 §3.3).
func ProgramDir() (string, error) {
	return resolveExeDir()
}

// Sub returns a path under the data root and ensures it exists, the
// replacement for filepath.Join(home, ".octo", parts...) plus MkdirAll.
func Sub(parts ...string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("datapath: create %s: %w", dir, err)
	}
	return dir, nil
}

// Join returns a path under the data root without creating anything.
// Prefer it for read-only lookups so opening a file cannot create state.
func Join(parts ...string) (string, error) {
	root, err := resolveRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{root}, parts...)...), nil
}
