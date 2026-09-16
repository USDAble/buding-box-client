package datapath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestFreezeRefusesWritesToAnExistingRoot pins the freeze contract at its
// hardest point: the data root is present and perfectly writable, and the write
// still has to be refused. Freezing against a *missing* directory would pass
// for the wrong reason — resolution fails anyway — so a test built that way
// cannot tell the gate from an unrelated error. This one can.
func TestFreezeRefusesWritesToAnExistingRoot(t *testing.T) {
	t.Cleanup(Thaw) // the gate is process-global; never leak it into another test
	base := filepath.Join(t.TempDir(), "data")
	t.Setenv(dataRootEnv, base)
	if _, err := Root(); err != nil {
		t.Fatalf("setup: Root() = %v, want nil", err)
	}

	Freeze()
	if _, err := Root(); !errors.Is(err, ErrFrozen) {
		t.Fatalf("Root() while frozen = %v, want ErrFrozen", err)
	}
	if _, err := Sub("sessions"); !errors.Is(err, ErrFrozen) {
		t.Fatalf("Sub() while frozen = %v, want ErrFrozen", err)
	}
	// Sub must refuse before creating: a sessions/ directory appearing beside a
	// frozen root is the write the gate exists to stop, and it would be left
	// behind even though the call reported an error.
	if _, err := os.Stat(filepath.Join(base, "sessions")); !os.IsNotExist(err) {
		t.Fatalf("Sub() created %q while frozen", filepath.Join(base, "sessions"))
	}
	// Reads stay usable: the directory never went away, and the desktop shell
	// still has to serve the frozen overlay out of it.
	if _, err := Join("sessions"); err != nil {
		t.Fatalf("Join() while frozen = %v, want nil (read paths are exempt)", err)
	}
	// ProgramDir is read-only too — it is the data root's parent, not the root.
	if _, err := ProgramDir(); err != nil {
		t.Fatalf("ProgramDir() while frozen = %v, want nil", err)
	}

	Thaw()
	if _, err := Root(); err != nil {
		t.Fatalf("Root() after Thaw = %v, want nil", err)
	}
	if _, err := Sub("sessions"); err != nil {
		t.Fatalf("Sub() after Thaw = %v, want nil", err)
	}
}

// TestFreezeCreatesNothing covers the case the gate is really for: the root is
// gone, and a frozen product must not helpfully recreate it. An empty data/
// next to the executable is indistinguishable from a clean install, so the user
// would be shown onboarding and a fresh data set while their real data sat on
// the disk they just unplugged.
func TestFreezeCreatesNothing(t *testing.T) {
	t.Cleanup(Thaw)
	base := filepath.Join(t.TempDir(), "data")
	t.Setenv(dataRootEnv, base)

	Freeze()
	if _, err := Root(); !errors.Is(err, ErrFrozen) {
		t.Fatalf("Root() while frozen = %v, want ErrFrozen", err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("Root() created %q while frozen", base)
	}
}

// TestFrozenReflectsTheGate keeps the predicate honest: Frozen() is what the
// desktop shell's watchdog reports as the product's write state and what the
// UI would read, so it must be the gate's own value and not a second bit that
// can drift from it.
func TestFrozenReflectsTheGate(t *testing.T) {
	t.Cleanup(Thaw)

	Thaw()
	if Frozen() {
		t.Fatal("Frozen() = true after Thaw")
	}
	Freeze()
	if !Frozen() {
		t.Fatal("Frozen() = false after Freeze")
	}
}
