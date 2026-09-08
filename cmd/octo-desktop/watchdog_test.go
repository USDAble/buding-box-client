package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/datapath"
)

// startWatchdog is exercised with a tiny interval so the tests don't wait the
// production 2s cadence. The onLost/onRestored callbacks signal buffered
// channels (capacity 1) so the watch loop never blocks a tick on the test.

func TestWatchdog_LostThenRestored(t *testing.T) {
	datapath.Thaw()          // the freeze gate is process-global; start clear
	t.Cleanup(datapath.Thaw) // and never leak a frozen gate into the next test

	base := t.TempDir()
	root := filepath.Join(base, "data")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCTO_DATA_ROOT", root)
	// The thaw's isOwnInstance reads instance.json; record our own pid so the
	// restored directory is recognised as ours.
	if err := writeInstanceJSON(t.Name(), os.Getpid()); err != nil {
		t.Fatal(err)
	}

	lost := make(chan struct{}, 1)
	restored := make(chan struct{}, 1)
	w := startWatchdog(root, 10*time.Millisecond, os.Getpid(),
		func() { lost <- struct{}{} },
		func() { restored <- struct{}{} },
	)
	defer w.Stop()

	// Pull the drive: rename the data root away → two consecutive failed stats
	// → onLost and frozen.
	moved := filepath.Join(base, "data-moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, lost, "onLost")
	if !w.Frozen() {
		t.Errorf("Frozen() = false after root removed, want true")
	}

	// Re-plug the same path → statOK again, and instance.json still records us
	// → thaw + onRestored.
	if err := os.Rename(moved, root); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, restored, "onRestored")
	if w.Frozen() {
		t.Errorf("Frozen() = true after root restored, want false")
	}
}

func TestWatchdog_NewPathDoesNotRestore(t *testing.T) {
	datapath.Thaw()          // the freeze gate is process-global; start clear
	t.Cleanup(datapath.Thaw) // and never leak a frozen gate into the next test

	base := t.TempDir()
	root := filepath.Join(base, "data")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCTO_DATA_ROOT", root)
	if err := writeInstanceJSON(t.Name(), os.Getpid()); err != nil {
		t.Fatal(err)
	}

	lost := make(chan struct{}, 1)
	restored := make(chan struct{}, 1)
	w := startWatchdog(root, 10*time.Millisecond, os.Getpid(),
		func() { lost <- struct{}{} },
		func() { restored <- struct{}{} },
	)
	defer w.Stop()

	// Rename away → onLost.
	moved := filepath.Join(base, "data-moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, lost, "onLost")

	// Reappear at a *different* path (a different drive letter): the original
	// path stays absent, so the watchdog must not thaw (需求 §5.1.2-10 — a
	// changed path cannot auto-resume).
	other := filepath.Join(base, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	select {
	case <-restored:
		t.Errorf("onRestored fired for a different path; want it to stay frozen")
	case <-time.After(150 * time.Millisecond):
		// expected: still frozen
	}
	if !w.Frozen() {
		t.Errorf("Frozen() = false after reappearing at a different path, want true")
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
