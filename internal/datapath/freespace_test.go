package datapath

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFreeSpaceReportsAPlausibleNumber is deliberately weak about the value and
// strict about the shape. The number depends on the machine running the test,
// so asserting a range would be a flake; what must hold everywhere is that a
// real directory yields a real reading, because the boot pre-check treats a
// failure to measure as "cannot judge" and would silently skip the check if
// this quietly returned 0.
func TestFreeSpaceReportsAPlausibleNumber(t *testing.T) {
	free, err := FreeSpace(t.TempDir())
	if err != nil {
		t.Fatalf("FreeSpace() = %v, want a reading", err)
	}
	if free == 0 {
		t.Fatal("FreeSpace() = 0 for a real directory; the boot pre-check would never fire")
	}
}

// TestFreeSpaceDoesNotInventAMissingVolume pins the failure direction: a path
// that is not there must report an error rather than 0. 0 reads as "the disk is
// full", so the two must not be the same value — otherwise a vanished data root
// would be reported to the user as "no space left", which names the wrong
// problem and sends them to fix the wrong thing (需求20260906 §5.1.2 第 4 条 asks
// for the reason to be named).
func TestFreeSpaceDoesNotInventAMissingVolume(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-here")
	if _, err := FreeSpace(missing); err == nil {
		t.Fatal("FreeSpace() on a missing path = nil error, want a failure")
	}
	// And it is a read-only probe: measuring must not bring the path into being.
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatalf("FreeSpace() created %q", missing)
	}
}

// TestFreeSpaceIsUsableWhileFrozen records the interaction with the L-E3 gate:
// measuring is a read, and the shell may legitimately want to say how much room
// is left on a frozen root (the volume did not go away, only the directory).
// Root/Sub refuse while frozen; this must not.
func TestFreeSpaceIsUsableWhileFrozen(t *testing.T) {
	t.Cleanup(Thaw)
	base := filepath.Join(t.TempDir(), "data")
	t.Setenv(dataRootEnv, base)
	if _, err := Root(); err != nil {
		t.Fatalf("setup: Root() = %v", err)
	}

	Freeze()
	if _, err := FreeSpace(base); err != nil {
		t.Fatalf("FreeSpace() while frozen = %v, want a reading", err)
	}
}
