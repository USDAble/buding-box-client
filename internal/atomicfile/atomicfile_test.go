package atomicfile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/atomicfile"
)

// noDebris fails the test if a temporary file was left next to the target.
func noDebris(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestWriteCreatesParentsAndExactContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "state.json")

	want := []byte("{\n  \"a\": 1\n}\n")
	if err := atomicfile.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("content = %q, want %q", got, want)
	}
	noDebris(t, filepath.Dir(path))

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// 0600 is a POSIX mode and this assertion is only meaningful where POSIX
	// modes exist: on Windows os.Stat reports a synthetic mode whose permission
	// bits are 0666 (plus READONLY), so Chmod cannot express 0600 at all. The
	// skipped half is not a nicety — it is the portable product's real
	// limitation on its primary platform, registered as V-64 rather than
	// encoded as an expectation. What IS asserted on Windows is that the write
	// landed and nothing was left behind, which is what the callers need.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("perm = %o, want 600", perm)
		}
	} else if perm := info.Mode().Perm(); perm != 0o666 {
		// Pin the Windows behaviour we actually get, so a future change that
		// silently makes the file *more* permissive (Chmod removing only the
		// owner-write bit, say) fails here instead of passing unnoticed.
		t.Logf("windows mode = %o (not a POSIX permission; V-64)", perm)
	}
}

// The rename must replace the old file wholesale. This is the property that
// makes a save crash-safe: a reader sees the old file or the new one, never a
// mixture, which is what a truncate-then-write would allow.
func TestWriteReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credential.json")

	long := []byte(strings.Repeat("old-content-", 100))
	if err := atomicfile.WriteFile(path, long, 0o600); err != nil {
		t.Fatalf("first write: %v", err)
	}

	short := []byte(`{"refreshToken":"new"}`)
	if err := atomicfile.WriteFile(path, short, 0o600); err != nil {
		t.Fatalf("second write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	// If the second write had truncated in place, the tail of the old content
	// would still be here.
	if string(got) != string(short) {
		t.Errorf("content = %q, want %q (no leftover bytes)", got, short)
	}
	noDebris(t, dir)
}

// A failure must not leave a temporary file behind: on the portable drive that
// debris would be copied along with the user's data.
//
// This test needs a directory the OS refuses to write into, and only POSIX
// mode bits provide one. Windows does not enforce the read-only bit on a
// directory for the owning process, so the premise cannot be built there and
// the test is skipped with that reason stated — the property it pins is still
// wanted on Windows, it is simply unreachable by this means (V-64).
func TestFailedWriteLeavesNothingBehind(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}
	if runtime.GOOS == "windows" {
		t.Skip("windows ignores mode 0500 on directories, so a failing write cannot be provoked this way (V-64)")
	}
	dir := t.TempDir()
	readOnly := filepath.Join(dir, "ro")
	if err := os.Mkdir(readOnly, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })

	path := filepath.Join(readOnly, "state.json")
	err := atomicfile.WriteFile(path, []byte("{}"), 0o600)
	if err == nil {
		t.Fatal("WriteFile succeeded on a read-only directory, want an error")
	}
	noDebris(t, readOnly)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("target exists after a failed write: %v", statErr)
	}
}

// Writing an empty file is a valid outcome - an empty preference list, say - and
// must not be confused with a failure.
func TestWriteEmptyContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")

	if err := atomicfile.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}
	noDebris(t, dir)
}
