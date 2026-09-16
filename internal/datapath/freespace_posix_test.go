//go:build !windows

package datapath

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestFreeSpaceMatchesTheFilesystemsAvailableBlocks pins which field the Posix
// reading comes off, by recomputing it from the filesystem independently of the
// implementation.
//
// The comparison carries an allowance (see driftAllowance for the value and why it
// sits where it does) because the two readings cannot be taken at the same instant:
// FreeSpace() stats the volume and then this test stats it again, so any other
// process writing anywhere on that volume in between moves Bavail. A byte-exact
// assertion failed only on a busy volume — V-96 measured 0 failures in 300 isolated
// runs (the two reads are about 0.2 ms apart) against 2 failures in full-suite runs,
// where other packages are writing temp files concurrently.
//
// Honest limit: on a filesystem that reserves no blocks for root (APFS, tmpfs)
// f_bavail and f_bfree are equal, so this cannot tell the two apart there — a
// mutation to Bfree survives on macOS. The distinction only becomes observable
// where a reserve exists (ext4's default 5%), which is where this pin actually
// does work; the reason to prefer Bavail is argued at the implementation. The
// allowance does not weaken it there, and
// TestTheDriftAllowanceToleratesNoiseButNotTheWrongField pins that side of the
// claim with the two numbers each end really produces.
func TestFreeSpaceMatchesTheFilesystemsAvailableBlocks(t *testing.T) {
	dir := t.TempDir()
	got, err := FreeSpace(dir)
	if err != nil {
		t.Fatalf("FreeSpace() = %v", err)
	}

	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		t.Fatalf("Statfs() = %v", err)
	}
	want := uint64(st.Bavail) * uint64(st.Bsize)

	if !withinDriftAllowance(got, want) {
		t.Fatalf("FreeSpace() = %d, the filesystem reports Bavail*Bsize = %d — %.4f%% apart, over the %.2f%% drift allowance",
			got, want, relativeGap(got, want), 100*driftAllowance)
	}
}

// driftAllowance is how far apart the two readings of the same volume may be.
//
// Relative, and deliberately far from both ends. The drift V-96 measured was
// 12288 B on a 258 GB volume — 4.8e-8 relative — while the difference this pin
// exists to catch is far coarser: Bfree counts the blocks reserved for root, 5% of
// the volume by default on ext4. 1% is orders of magnitude above the drift we
// measured and still 5x below the signal.
//
// It is deliberately not tightened to the measured drift. A single large write
// during the window between the two reads moves Bavail by its own size — 1 GB is
// already 0.4% of a 258 GB volume — and a pin that goes red when an unrelated
// process writes a big file is exactly the false red V-96 was.
const driftAllowance = 0.01

// withinDriftAllowance reports whether two readings of the same volume are close
// enough to be the same quantity, given that they were necessarily taken at
// different instants.
func withinDriftAllowance(a, b uint64) bool {
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	// Both zero (a volume with nothing left) compares 0 <= 0 and passes, which is
	// the only reading such a volume allows.
	return float64(hi-lo) <= driftAllowance*float64(hi)
}

func relativeGap(a, b uint64) float64 {
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi == 0 {
		return 0
	}
	return 100 * float64(hi-lo) / float64(hi)
}

// TestTheDriftAllowanceToleratesNoiseButNotTheWrongField pins the allowance's two
// ends, which the statfs comparison above cannot do on this machine: APFS reserves
// no blocks for root, so Bavail and Bfree are equal there and the wrong-field
// signal does not exist to reject. The numbers below are the ones each side really
// produces — the drift V-96 measured, and ext4's default 5% reserve.
func TestTheDriftAllowanceToleratesNoiseButNotTheWrongField(t *testing.T) {
	const volume = uint64(258_009_468_928)

	// Three 4 KiB blocks: the drift V-96 registered, which must be tolerated.
	if !withinDriftAllowance(volume, volume-12288) {
		t.Fatal("a 12288 B drift on a 258 GB volume is the measurement V-96 registered; it must be tolerated")
	}
	// Bfree counts the 5% root reserve, so the wrong field reads larger by that
	// margin — that must NOT be tolerated, or the pin stops pinning.
	if withinDriftAllowance(volume, volume+volume/20) {
		t.Fatal("a 5% gap is the wrong-field signal (ext4's default reserve); the allowance must reject it")
	}
	if !withinDriftAllowance(volume, volume) {
		t.Fatal("identical readings must agree")
	}
	if !withinDriftAllowance(0, 0) {
		t.Fatal("a volume with nothing left reports zero twice; the allowance must not divide by it")
	}
}
