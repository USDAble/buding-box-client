package chatmode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The factory default must not invent model ids: the models offered under each
// mode come from the signed catalog (P0-04), so a hardcoded id here would ship
// a model that cannot be selected. Modes themselves are product-fixed.
func TestBuiltinHasNoHardcodedModels(t *testing.T) {
	c := Builtin()
	if c.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", c.SchemaVersion)
	}
	if len(c.Modes) != 3 {
		t.Fatalf("len(modes) = %d, want 3", len(c.Modes))
	}

	wantIDs := []string{ModePrivacy, ModeSmart, ModeDefault}
	for i, id := range wantIDs {
		m, ok := c.ModeByID(id)
		if !ok {
			t.Fatalf("mode %q missing from builtin", id)
		}
		if c.Modes[i].ID != id {
			t.Errorf("modes[%d].id = %q, want %q (order is part of the selector)", i, c.Modes[i].ID, id)
		}
		if len(m.Models) != 0 {
			t.Errorf("mode %q carries hardcoded models %v — the catalog projects them (P0-04)", id, m.Models)
		}
		if m.DefaultModel != "" {
			t.Errorf("mode %q has hardcoded defaultModel %q — the catalog projects it (P0-04)", id, m.DefaultModel)
		}
	}
}

// The factory grouping used to assert "privacy has no cloud model, smart has no
// local model" (需求 §8). That invariant moved with the projection: with no ids
// in Builtin() it is vacuous here, so it is asserted where ids now come from —
// the catalog projection in P0-04 — rather than left as a test that always
// passes. Keep the ids out; see TestBuiltinHasNoHardcodedModels.

func TestLoadMissingFileSeedsBuiltin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat-modes.json")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(missing) = %v, want nil", err)
	}
	if len(cfg.Modes) != 3 {
		t.Fatalf("len(modes) = %d, want 3 (builtin)", len(cfg.Modes))
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("builtin default not written: %v", statErr)
	}
}

func TestLoadCorruptFileKeepsUserFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat-modes.json")
	original := []byte("{ not valid json ")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err == nil {
		t.Fatal("Load(corrupt) = nil error, want ErrUnreadable")
	}
	if !strings.Contains(err.Error(), ErrUnreadable.Error()) {
		t.Fatalf("error = %v, want wrapped ErrUnreadable", err)
	}
	if len(cfg.Modes) != 3 {
		t.Fatalf("len(modes) = %d, want 3 (builtin fallback)", len(cfg.Modes))
	}
	// The user's hand edit must survive untouched (需求 §9: 不覆盖).
	after, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(after) != string(original) {
		t.Fatalf("file was overwritten: got %q, want %q", after, original)
	}
}

func TestLoadParsesUserConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat-modes.json")
	content := `{"schemaVersion":1,"modes":[{"id":"privacy","models":["a"],"defaultModel":"a"}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if len(cfg.Modes) != 1 || cfg.Modes[0].ID != ModePrivacy {
		t.Fatalf("parsed config = %+v, want the single privacy mode", cfg.Modes)
	}
}

// ModesForModel is pure grouping logic over whatever ids the config carries —
// the ids themselves come from the catalog at runtime (P0-04), so this drives
// it with an explicit config rather than Builtin().
func TestModesForModelOverlap(t *testing.T) {
	c := Config{
		SchemaVersion: 1,
		Modes: []Mode{
			{ID: ModePrivacy, Models: []string{"m-local"}},
			{ID: ModeSmart, Models: []string{"m-cloud"}},
			// Overlap is allowed: one model may belong to several modes
			// (需求 §5.6 规则 1).
			{ID: ModeDefault, Models: []string{"m-local", "m-cloud"}},
		},
	}

	if got := c.ModesForModel("m-cloud"); len(got) != 2 || got[0] != ModeSmart || got[1] != ModeDefault {
		t.Fatalf("ModesForModel(m-cloud) = %v, want [smart default]", got)
	}
	if got := c.ModesForModel("m-local"); len(got) != 2 || got[0] != ModePrivacy || got[1] != ModeDefault {
		t.Fatalf("ModesForModel(m-local) = %v, want [privacy default]", got)
	}
	if got := c.ModesForModel("not-listed"); len(got) != 0 {
		t.Fatalf("ModesForModel(not-listed) = %v, want empty", got)
	}
}

func TestModeByIDMissing(t *testing.T) {
	if _, ok := Builtin().ModeByID("nope"); ok {
		t.Fatal("ModeByID(nope) = ok, want missing")
	}
}
