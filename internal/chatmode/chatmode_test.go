package chatmode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinMatchesRequirement(t *testing.T) {
	c := Builtin()
	if c.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", c.SchemaVersion)
	}
	if len(c.Modes) != 3 {
		t.Fatalf("len(modes) = %d, want 3", len(c.Modes))
	}

	privacy, _ := c.ModeByID(ModePrivacy)
	smart, _ := c.ModeByID(ModeSmart)
	def, _ := c.ModeByID(ModeDefault)

	// 出厂断言（需求 §8 明确验收）：隐私列无云端项、智能列无本地项.
	for _, m := range privacy.Models {
		if strings.HasPrefix(m, "buding-cloud") {
			t.Errorf("privacy mode contains cloud model %q, want local-only", m)
		}
	}
	for _, m := range smart.Models {
		if strings.HasPrefix(m, "buding-local") {
			t.Errorf("smart mode contains local model %q, want cloud-only", m)
		}
	}

	if def.DefaultModel != "buding-cloud-plus" {
		t.Errorf("default mode defaultModel = %q, want buding-cloud-plus (云端智能)", def.DefaultModel)
	}
	if privacy.DefaultModel != "buding-local-general" {
		t.Errorf("privacy defaultModel = %q, want buding-local-general", privacy.DefaultModel)
	}
}

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

func TestModesForModelOverlap(t *testing.T) {
	c := Builtin()
	got := c.ModesForModel("buding-cloud-plus")
	// 智能 + 默认 都含云端智能，不含隐私（需求 §5.6 规则 1 允许重叠）.
	want := []string{ModeSmart, ModeDefault}
	if len(got) != len(want) {
		t.Fatalf("ModesForModel(buding-cloud-plus) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ModesForModel = %v, want %v", got, want)
		}
	}

	if got := c.ModesForModel("buding-local-fast"); len(got) != 2 || got[0] != ModePrivacy || got[1] != ModeDefault {
		t.Fatalf("ModesForModel(buding-local-fast) = %v, want [privacy default]", got)
	}
}

func TestModeByIDMissing(t *testing.T) {
	if _, ok := Builtin().ModeByID("nope"); ok {
		t.Fatal("ModeByID(nope) = ok, want missing")
	}
}
