// Package atomicfile writes a file so that a crash, a power cut or a yanked
// drive cannot leave a half-written one behind: the bytes land in a temporary
// file in the same directory and are then renamed over the target.
//
// Two consumers need exactly this - the product state and the credential file -
// and both live on removable media, which is why the logic is not duplicated in
// each store. A rename is the only operation that is atomic on FAT32/exFAT,
// which is what the portable drive is formatted with; it is also why no file
// locking is attempted (none exists there).
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile writes data to path via temp-file + rename, creating the parent
// directory if needed.
//
// perm is applied to the temporary file before the rename, so the target never
// exists with wider permissions than requested. On the portable drive perms are
// ignored (FAT32/exFAT have none) - that is expected, not a failure.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("atomicfile: create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("atomicfile: temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Any failure past this point must remove the temporary file, or a yanked
	// drive leaves debris next to the user's data.
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("atomicfile: write %s: %w", tmpName, err)
	}
	// Sync before rename: without it the rename can be durable while the
	// contents are not, on a drive that may be unplugged at any moment.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("atomicfile: sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("atomicfile: close %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		cleanup()
		return fmt.Errorf("atomicfile: chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("atomicfile: rename onto %s: %w", path, err)
	}
	return nil
}
