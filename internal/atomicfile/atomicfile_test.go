package atomicfile_test

import (
	"os"
	"path/filepath"
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
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
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
func TestFailedWriteLeavesNothingBehind(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
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
