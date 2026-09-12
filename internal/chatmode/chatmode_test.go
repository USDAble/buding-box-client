package chatmode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The mode set is a product constant, not catalog data (中台交付包 §4.3
// 「catalog.modes 的形状」: an unknown id is ignored and never becomes a new
// mode). These tests pin the set and its order, because the picker's group
// order is user-visible and a reordering is a product decision.

func TestTheProductOwnsThreeModesInOrder(t *testing.T) {
	want := []string{ModePrivacy, ModeSmart, ModeDefault}
	got := IDs()

	if len(got) != len(want) {
		t.Fatalf("IDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("IDs() = %v, want %v (order is user-visible)", got, want)
		}
	}
}

func TestOnlyProductModesAreRecognised(t *testing.T) {
	// The positive half is deliberately not just the three constants read back
	// through one helper: an unknown id that a catalog invents must be refused,
	// and so must the near-misses a hand-typed value produces.
	for _, id := range []string{ModePrivacy, ModeSmart, ModeDefault} {
		if !IsProductMode(id) {
			t.Errorf("IsProductMode(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"", "teams", "smart ", "Smart", "defaults", "privacy-mode"} {
		if IsProductMode(id) {
			t.Errorf("IsProductMode(%q) = true, want false — the mode set is a product constant", id)
		}
	}
}

// TestThisPackageNeverReadsAFile defends 需求基线 B1 规则 2 mechanically: the
// packaged data/chat-modes.json was deleted, and "不再落此文件" is a requirement,
// not a preference. A future edit that reintroduces a loader here would undo the
// one rule that stops a local list from shadowing the signed catalog, and it
// would do so silently — the loader would simply answer when the catalog could
// not.
//
// The assertion is over the source rather than the behaviour because a loader
// only misbehaves on the path that calls it, and that path is what a later PR
// would add. The archive branch this package was moved from had exactly these
// three names (Load, Config, ErrUnreadable); they are listed so the refusal is
// specific to the thing being refused rather than to the word "file".
func TestThisPackageNeverReadsAFile(t *testing.T) {
	root := repoRootForChatmode(t)

	forbidden := []string{"os.ReadFile", "os.Open", "chat-modes.json", "ErrUnreadable", "encoding/json"}
	entries, err := os.ReadDir(filepath.Join(root, "internal", "chatmode"))
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, "internal", "chatmode", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		checked++
		for _, bad := range forbidden {
			if strings.Contains(string(raw), bad) {
				t.Errorf("internal/chatmode/%s mentions %q; this package holds the mode ids and nothing else "+
					"(需求基线 B1 规则 2: data/chat-modes.json is gone and must not come back)", name, bad)
			}
		}
	}
	// Without this the loop could pass by having read nothing at all.
	if checked == 0 {
		t.Fatal("no non-test Go file was inspected; this nail is not testing anything")
	}
}

// repoRootForChatmode walks up for go.mod rather than counting parent
// directories, so the nail still points somewhere real if the package moves.
func repoRootForChatmode(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("walked to the filesystem root without finding go.mod")
		}
		dir = parent
	}
}
