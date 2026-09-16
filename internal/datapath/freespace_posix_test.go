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
// Honest limit: on a filesystem that reserves no blocks for root (APFS, tmpfs)
// f_bavail and f_bfree are equal, so this cannot tell the two apart there — a
// mutation to Bfree survives on macOS. The distinction only becomes observable
// where a reserve exists (ext4's default 5%), which is where this pin actually
// does work; the reason to prefer Bavail is argued at the implementation.
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
	if got != want {
		t.Fatalf("FreeSpace() = %d, the filesystem reports Bavail*Bsize = %d", got, want)
	}
}
