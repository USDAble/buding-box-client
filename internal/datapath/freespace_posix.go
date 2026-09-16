//go:build !windows

package datapath

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// freeSpace reads the volume's available blocks for a non-privileged writer.
//
// Bavail, not Bfree: Bfree counts the blocks the filesystem reserves for root,
// which a product running as an ordinary user cannot have — reporting it would
// overstate the room left by the size of that reserve (5% by default on ext4),
// which is exactly the kind of confidently-wrong number §3.9 rules out.
func freeSpace(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("datapath: stat %s: %w", path, err)
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
