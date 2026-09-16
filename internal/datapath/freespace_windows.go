//go:build windows

package datapath

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// freeSpace asks the volume for the bytes available to the calling user.
//
// GetDiskFreeSpaceEx's first result is the "available to caller" figure, which
// is the one that matters here — the other two describe the volume, and on a
// volume with quotas the difference is the whole answer. (Unlike the Posix
// side there is no reserve to subtract: Windows has no root-only block
// reserve, which is why the two implementations do not look alike.)
func freeSpace(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("datapath: encode %s: %w", path, err)
	}
	var available, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &available, &total, &totalFree); err != nil {
		return 0, fmt.Errorf("datapath: get free space for %s: %w", path, err)
	}
	return available, nil
}
