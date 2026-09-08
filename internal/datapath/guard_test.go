package datapath

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the repository root, derived from this test file's own
// location so the check is independent of the process cwd.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// This file is internal/datapath/guard_test.go, so the root is two levels
	// up from its directory.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// TestNoOctoLiteral enforces the zero-exception rule from 开发规范.md §3.1:
// no Go source under internal/, cmd/, shared/ may contain the ".octo" string
// literal — it was the pre-fork data root and no longer exists in the product.
// This is the local counterpart to scripts/datapath-guard.mjs (CI), so
// `make test` catches a regression without needing Node. It matches the exact
// double-quoted ".octo", so ".octo-hooks.yml" (the renamed project-level hooks
// file), ".octorules", and prose mentioning the path name never trip it.
func TestNoOctoLiteral(t *testing.T) {
	root := repoRoot(t)
	const forbidden = `".octo"`

	var violations []string
	for _, dir := range []string{"internal", "cmd", "shared"} {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if strings.Contains(string(b), forbidden) {
				rel, _ := filepath.Rel(root, p)
				violations = append(violations, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if len(violations) > 0 {
		t.Errorf("forbidden `.octo` literal in %d file(s):\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
}
