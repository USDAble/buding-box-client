package datapath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tmpDir returns a temp dir with symlinks resolved. On macOS the /var
// returned by t.TempDir() aliases /private/var, while datapath resolves the
// executable path (including ancestor symlinks), so expectations built from
// a raw TempDir would mismatch for the wrong reason.
func tmpDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if r, err := filepath.EvalSymlinks(d); err == nil {
		d = r
	}
	return d
}

// pointExecutableAt makes the package resolve the program directory from a
// fake executable living in dir, so tests never touch the real test binary's
// directory and Root's default branch is fully deterministic.
func pointExecutableAt(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fake program dir: %v", err)
	}
	name := filepath.Join(dir, "octo-desktop")
	f, err := os.Create(name)
	if err != nil {
		t.Fatalf("create fake executable: %v", err)
	}
	f.Close()

	old := exePath
	exePath = func() (string, error) { return name, nil }
	t.Cleanup(func() { exePath = old })
	return name
}

func TestRootDefaultsToProgramDirData(t *testing.T) {
	progDir := filepath.Join(tmpDir(t), "prog")
	pointExecutableAt(t, progDir)
	t.Setenv(dataRootEnv, "") // unset means "use the executable's data/"

	got, err := Root()
	if err != nil {
		t.Fatalf("Root() error: %v", err)
	}
	want := filepath.Join(progDir, "data")
	if got != want {
		t.Fatalf("Root() = %q, want %q", got, want)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Fatalf("data root was not created: stat=%v err=%v", info, err)
	}
}

func TestRootUsesEnvOverride(t *testing.T) {
	// Even with a (different) fake executable around, an explicit
	// OCTO_DATA_ROOT must win — it is an override, not a fallback.
	pointExecutableAt(t, filepath.Join(t.TempDir(), "ignored"))
	custom := filepath.Join(t.TempDir(), "custom-root")
	t.Setenv(dataRootEnv, custom)

	got, err := Root()
	if err != nil {
		t.Fatalf("Root() error: %v", err)
	}
	if got != custom {
		t.Fatalf("Root() = %q, want %q", got, custom)
	}
}

func TestRootBlankEnvMeansUnset(t *testing.T) {
	progDir := filepath.Join(tmpDir(t), "prog")
	pointExecutableAt(t, progDir)
	t.Setenv(dataRootEnv, "   ")

	got, err := Root()
	if err != nil {
		t.Fatalf("Root() error: %v", err)
	}
	if want := filepath.Join(progDir, "data"); got != want {
		t.Fatalf("Root() = %q, want %q", got, want)
	}
}

func TestRootReadOnlyFailsWithoutHomeFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not carry the same meaning on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission checks are bypassed as root")
	}
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := os.Chmod(dataDir, 0o500); err != nil {
		t.Fatalf("chmod data dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0o700) }) // let TempDir remove it
	t.Setenv(dataRootEnv, dataDir)

	if _, err := Root(); err == nil {
		t.Fatal("Root() succeeded on a read-only data root, want an error")
	} else if home, _ := os.UserHomeDir(); home != "" && strings.Contains(err.Error(), home) {
		t.Fatalf("error mentions the home directory, must never fall back there: %v", err)
	}
}

func TestSubCreatesNestedDirectories(t *testing.T) {
	base := filepath.Join(t.TempDir(), "data")
	t.Setenv(dataRootEnv, base)

	got, err := Sub("sessions", "2026", "09")
	if err != nil {
		t.Fatalf("Sub() error: %v", err)
	}
	want := filepath.Join(base, "sessions", "2026", "09")
	if got != want {
		t.Fatalf("Sub() = %q, want %q", got, want)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Fatalf("nested dir not created: stat=%v err=%v", info, err)
	}
}

func TestJoinDoesNotCreateAnything(t *testing.T) {
	base := filepath.Join(t.TempDir(), "data")
	t.Setenv(dataRootEnv, base)

	got, err := Join("sessions", "x.jsonl")
	if err != nil {
		t.Fatalf("Join() error: %v", err)
	}
	want := filepath.Join(base, "sessions", "x.jsonl")
	if got != want {
		t.Fatalf("Join() = %q, want %q", got, want)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("Join() created the data root %q (stat err=%v), want no side effects", base, err)
	}
}

func TestSymlinkedExecutableResolvesToRealDir(t *testing.T) {
	realDir := filepath.Join(tmpDir(t), "real")
	pointExecutableAt(t, realDir)

	linkDir := filepath.Join(tmpDir(t), "link-to-real")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	old := exePath
	link := filepath.Join(linkDir, "octo-desktop")
	exePath = func() (string, error) { return link, nil }
	t.Cleanup(func() { exePath = old })
	t.Setenv(dataRootEnv, "")

	got, err := Root()
	if err != nil {
		t.Fatalf("Root() error: %v", err)
	}
	// The data root must live next to the real program, not inside the link.
	if want := filepath.Join(realDir, "data"); got != want {
		t.Fatalf("Root() = %q, want %q (symlink not resolved)", got, want)
	}
}

func TestProgramDir(t *testing.T) {
	progDir := filepath.Join(tmpDir(t), "prog")
	pointExecutableAt(t, progDir)

	got, err := ProgramDir()
	if err != nil {
		t.Fatalf("ProgramDir() error: %v", err)
	}
	if got != progDir {
		t.Fatalf("ProgramDir() = %q, want %q", got, progDir)
	}
}
