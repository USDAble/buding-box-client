package datapath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestFreezeBlocksWrites pins the portable freeze contract: while frozen, the
// directory-creating entry points (Root, Sub) refuse with ErrFrozen instead of
// creating a directory, and the read-only Join stays usable. This is the
// backend half of "U盘 pulled out → the product must not write" (P2 §3.4).
func TestFreezeBlocksWrites(t *testing.T) {
	t.Cleanup(Thaw) // the gate is process-global; never leak it
	base := filepath.Join(t.TempDir(), "data")
	t.Setenv(dataRootEnv, base)

	Freeze()
	if _, err := Root(); !errors.Is(err, ErrFrozen) {
		t.Fatalf("Root() after Freeze = %v, want ErrFrozen", err)
	}
	if _, err := Sub("sessions"); !errors.Is(err, ErrFrozen) {
		t.Fatalf("Sub() after Freeze = %v, want ErrFrozen", err)
	}
	// Read-only lookups must not be blocked — a directory that never went away
	// is still readable while frozen.
	if _, err := Join("x"); err != nil {
		t.Fatalf("Join() after Freeze = %v, want nil (read paths unaffected)", err)
	}

	Thaw()
	if _, err := Root(); err != nil {
		t.Fatalf("Root() after Thaw = %v, want nil", err)
	}
}

// TestFreezeCreatesNothing verifies the freeze gate's whole point: a frozen
// Root must not leave a data/ directory behind — that would be an empty,
// freshly-created directory indistinguishable from a clean install, silently
// discarding the user's real (moved-away) data.
func TestFreezeCreatesNothing(t *testing.T) {
	t.Cleanup(Thaw)
	base := filepath.Join(t.TempDir(), "data")
	t.Setenv(dataRootEnv, base)

	Freeze()
	if _, err := Root(); !errors.Is(err, ErrFrozen) {
		t.Fatalf("Root() = %v, want ErrFrozen", err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("Root() created %q while frozen", base)
	}
}
