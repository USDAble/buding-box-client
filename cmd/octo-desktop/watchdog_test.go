package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/datapath"
)

// The watchdog's cadence is 2s in production; the tests run it at 5ms so the
// behaviours ("freezes once", "does not repeat", "thaws once") are asserted as
// transitions rather than by sleeping through a real timeout. The hysteresis
// count is passed explicitly for the same reason — see newTestWatchdog.
const testInterval = 5 * time.Millisecond

// newTestWatchdog starts a watchdog on root counting both callbacks, and
// arranges for it to be stopped and the process-global gate to be cleared when
// the test ends. Without the Thaw cleanup, one test's freeze would leak into
// every later test in the package.
func newTestWatchdog(t *testing.T, root string, failures int) (*Watchdog, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var lost, back atomic.Int32
	w := startWatchdog(root, testInterval, failures, func() { lost.Add(1) }, func() { back.Add(1) })
	t.Cleanup(func() {
		w.Stop()
		datapath.Thaw()
	})
	return w, &lost, &back
}

// waitFor polls until cond holds or the deadline passes. Time-based assertions
// are the only option for a poll loop, so the helper keeps the busy-wait in one
// place and hands the test a real failure message rather than a flake.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestWatchdogFreezesOnceWhenTheRootDisappears is the primary case: the data
// directory vanishes and the product must stop writing. onLost firing exactly
// once is half the assertion — the other half is that the gate the writers read
// (datapath.Frozen) is set, because a watchdog that only noticed would leave
// every write path running (§4.2: 冻结后不能只冻结 UI).
func TestWatchdogFreezesOnceWhenTheRootDisappears(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	w, lost, back := newTestWatchdog(t, root, 2)

	// Healthy root: nothing to report, and the gate stays open.
	time.Sleep(20 * testInterval)
	if got := lost.Load(); got != 0 {
		t.Fatalf("onLost fired %d times on a healthy root", got)
	}
	if w.Frozen() {
		t.Fatal("Frozen() = true while the root is present")
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the first freeze", func() bool { return lost.Load() == 1 })
	if !w.Frozen() {
		t.Fatal("onLost fired but the datapath gate is still open")
	}
	// A write path reached during the freeze must be refused, not attempted
	// against a directory that is no longer there.
	if _, err := datapath.Sub("sessions"); err != datapath.ErrFrozen {
		t.Fatalf("datapath.Sub() while frozen = %v, want ErrFrozen", err)
	}
	if back.Load() != 0 {
		t.Fatalf("onBack fired %d times while the root is still gone", back.Load())
	}
}

// TestWatchdogDoesNotRepeatWhileTheRootStaysGone pins the "no nagging" rule
// (§4.2: 持续丢失不重复广播). The root is left out for many ticks, so a
// watchdog that re-notified on every failed stat would fire dozens of times —
// and the user would get a toast per tick.
func TestWatchdogDoesNotRepeatWhileTheRootStaysGone(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	_, lost, _ := newTestWatchdog(t, root, 2)

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first freeze", func() bool { return lost.Load() == 1 })

	// Long enough for tens of ticks; one notice is the whole contract.
	time.Sleep(100 * testInterval)
	if got := lost.Load(); got != 1 {
		t.Fatalf("onLost fired %d times for one continuous loss, want 1", got)
	}
}

// TestWatchdogFreezesOnAnAlreadyUnusableRoot covers the startup case: the
// watchdog is armed against a root that is not usable to begin with, so there
// is no healthy-looking state to transition out of. Waiting for a transition
// would leave the product running with an open write gate over a root that
// never worked — every write then landing wherever the resolution falls
// (§4.2: 启动时目录不可用 = fail-closed).
func TestWatchdogFreezesOnAnAlreadyUnusableRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data") // deliberately never created
	w, lost, back := newTestWatchdog(t, root, 2)

	waitFor(t, "the freeze", func() bool { return lost.Load() == 1 })
	if !w.Frozen() {
		t.Fatal("the write gate is open over a root that never existed")
	}
	if _, err := datapath.Sub("sessions"); err != datapath.ErrFrozen {
		t.Fatalf("datapath.Sub() = %v, want ErrFrozen", err)
	}
	// Freezing must not have created the directory as a side effect: that would
	// be the "recreate an empty data/" failure wearing the watchdog's badge.
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("the freeze created %q", root)
	}
	if back.Load() != 0 {
		t.Fatalf("onBack fired %d times with no usable root", back.Load())
	}
}

// TestWatchdogSurvivesATransientStatFailure covers the hysteresis: a single
// failed check is not a pulled disk. Freezing here would be worse than a missed
// detection — the product would refuse to write over a blip, and the user would
// be interrupted for a disk that never moved.
func TestWatchdogSurvivesATransientStatFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// failures=2 means one failure must not be enough. Recreate the directory
	// faster than the watchdog can see two consecutive failures by alternating
	// on a period longer than one tick but shorter than two.
	_, lost, _ := newTestWatchdog(t, root, 2)

	// One tick's worth of absence: take the directory away and put it straight
	// back. The watchdog may well see zero or one failure, never two in a row.
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * testInterval)
	if got := lost.Load(); got != 0 {
		t.Fatalf("onLost fired %d times for a blip shorter than the hysteresis", got)
	}
	if datapath.Frozen() {
		t.Fatal("a single failed stat froze the product")
	}
}

// TestWatchdogThawsOnceWhenTheSamePathReturns is the recovery half: the disk is
// plugged back into the same place, so the product resumes. Once again the
// callback count is not the whole assertion — writes have to actually work
// again, or the product would be stuck refusing forever.
func TestWatchdogThawsOnceWhenTheSamePathReturns(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	w, lost, back := newTestWatchdog(t, root, 2)

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first freeze", func() bool { return lost.Load() == 1 })

	// The same path, usable again. Re-created rather than remounted because a
	// test cannot present a real disk; the code's only question is whether the
	// path is a directory, so this exercises the same branch.
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the thaw", func() bool { return back.Load() == 1 })
	if w.Frozen() {
		t.Fatal("onBack fired but the datapath gate is still closed")
	}
	if _, err := datapath.Sub("sessions"); err != nil {
		t.Fatalf("datapath.Sub() after thaw = %v, want nil", err)
	}
	// One recovery is one notice: the callback must not re-fire on each healthy
	// tick afterwards.
	time.Sleep(50 * testInterval)
	if got := back.Load(); got != 1 {
		t.Fatalf("onBack fired %d times for one recovery, want 1", got)
	}
}

// TestWatchdogIgnoresARootThatComesBackElsewhere covers "新路径不算恢复"
// (§4.2). The product's data root is baked in at startup, so a disk that
// reappears under a different path is not our data — thawing onto it would
// start writing a fresh data set onto a stranger's disk while the user's real
// directory was still gone.
//
// Modelled by moving the directory rather than renaming a drive: the watchdog
// holds one absolute path, and a root that is no longer at that path is exactly
// the case under test.
func TestWatchdogIgnoresARootThatComesBackElsewhere(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "data")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	w, lost, back := newTestWatchdog(t, root, 2)

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first freeze", func() bool { return lost.Load() == 1 })

	// The data reappears one level over — the same bytes, a different path.
	elsewhere := filepath.Join(parent, "data-elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * testInterval)
	if !w.Frozen() {
		t.Fatalf("a directory at %s was treated as the data root returning at %s", elsewhere, root)
	}
	if back.Load() != 0 {
		t.Fatalf("onBack fired %d times for a root that never came back", back.Load())
	}
}

// TestWatchdogFreezesAgainAfterRecovering pins that the one-shot behaviour is
// per episode, not once per process. A disk pulled out twice is two incidents,
// and the second one must still stop the writers — a watchdog that latched
// after the first would silently stop protecting the product.
func TestWatchdogFreezesAgainAfterRecovering(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	w, lost, back := newTestWatchdog(t, root, 2)

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first freeze", func() bool { return lost.Load() == 1 })
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the thaw", func() bool { return back.Load() == 1 })

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second freeze", func() bool { return lost.Load() == 2 })
	if !w.Frozen() {
		t.Fatal("the product kept writing after the disk was pulled a second time")
	}
}
