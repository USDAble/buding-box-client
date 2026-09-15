package datapath

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

// TestNoOctoLiteral enforces the rule from 开发规范.md §3.1: no Go source under
// internal/, cmd/, shared/ may contain the ".octo" string literal — it was the
// pre-fork data root and no longer exists in the product. This is the local
// counterpart to scripts/datapath-guard.mjs (CI), so `make test` catches a
// regression without needing Node. It matches the exact double-quoted ".octo",
// so ".octorules" and prose mentioning the path name never trip it.
//
// The marker rule is mirrored from the script deliberately — the two must agree,
// and the script's copy documents why the exception exists at all: a line may
// carry the literal only if that same line carries an `octo-literal-allow:`
// marker with a reason. Today that licenses exactly two sites, both naming a
// *project's* .octo directory inside the user's own repository rather than the
// data root (internal/hooks/trust.go, internal/app/worktree.go). The marker is
// per-line and licences nothing else, so a new unmarked occurrence anywhere —
// including right next to a marked one — still fails.
func TestNoOctoLiteral(t *testing.T) {
	root := repoRoot(t)
	const forbidden = `".octo"`
	marker := regexp.MustCompile(`octo-literal-allow:\s*\S+`)

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
			rel, _ := filepath.Rel(root, p)
			lines := strings.Split(string(b), "\n")
			for i, line := range lines {
				if !strings.Contains(line, forbidden) {
					continue
				}
				// Same line only — see the doc comment.
				if marker.MatchString(line) {
					continue
				}
				violations = append(violations, fmt.Sprintf("%s:%d", rel, i+1))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if len(violations) > 0 {
		t.Errorf("unlicensed `.octo` literal at %d site(s) (each needs an octo-literal-allow marker with a reason):\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
}
