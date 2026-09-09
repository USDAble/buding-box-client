// Package chatmode loads the mode→model grouping that drives the P9 model
// selector. It is pure data: the modes ("privacy" / "smart" / "default") group
// model ids, and the models themselves keep living in upstream config.yml's
// endpoint/model system — this file never re-defines a model.
//
// OCTO-FORK: P9 模式与模型选择器 — 纯新增包，上游无同名路径（零合并冲突）。
// 见 dev-docs-usdable/需求/2260906/技术方案/P9-模式与模型.md。
package chatmode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Mode ids. These are fixed ASCII identifiers (需求 §5.2.4「标识符例外」) —
// data keys that must NOT follow the English brand name when the product is
// renamed. The display names live in the i18n layer (mode.privacy /
// mode.smart / mode.default).
const (
	ModePrivacy = "privacy"
	ModeSmart   = "smart"
	// ModeDefault is the group named "默认" — a grouping label, not the
	// generic notion of "the system default". 需求 §3 calls this ambiguity out
	// explicitly; keep the two meanings apart in prose and code.
	ModeDefault = "default"
)

// Mode is one selector group: an id plus the model ids offered under it and
// the group's default model (需求 §5.6 规则 4).
type Mode struct {
	ID           string   `json:"id"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"defaultModel"`
}

// Config is the parsed chat-modes.json.
type Config struct {
	SchemaVersion int    `json:"schemaVersion"`
	Modes         []Mode `json:"modes"`
}

// ErrUnreadable wraps a chat-modes.json that exists but cannot be parsed. Load
// still returns the built-in config alongside this error so callers can serve
// a working selector; the error is how they learn the file needs attention
// (需求 §9: 损坏时用内置默认并保留原文件，界面提示一次).
var ErrUnreadable = errors.New("chatmode: config is unreadable, using built-in")

// Builtin returns the factory grouping (需求 §5.6.3): privacy is local-only,
// smart is cloud-only, default spans both. The "default" group's default model
// is the cloud-plus model (需求 §8: 出厂默认模型为云端智能).
func Builtin() Config {
	return Config{
		SchemaVersion: 1,
		Modes: []Mode{
			{ID: ModePrivacy, Models: []string{"buding-local-general", "buding-local-fast"}, DefaultModel: "buding-local-general"},
			{ID: ModeSmart, Models: []string{"buding-cloud-plus", "buding-cloud-pro"}, DefaultModel: "buding-cloud-plus"},
			{ID: ModeDefault, Models: []string{"buding-local-general", "buding-local-fast", "buding-cloud-plus", "buding-cloud-pro"}, DefaultModel: "buding-cloud-plus"},
		},
	}
}

// Load reads the mode config at path. A missing file is seeded with the
// built-in default and written out (需求 §5.6 规则 1: 用户可改配置，改完按配置走);
// a present-but-unparseable file degrades to the built-in default WITHOUT
// overwriting the user's file, returning ErrUnreadable alongside the built-in
// so the caller can surface a one-time hint. A genuine read error is returned
// as-is with the built-in config.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			builtin := Builtin()
			// Seed the factory default so the user can see and edit it. A
			// failed write must not fail startup — the in-memory builtin is
			// still served (same degrade-to-builtin policy as the sensitive
			// dictionary).
			if werr := writeBuiltin(path, builtin); werr != nil {
				return builtin, nil
			}
			return builtin, nil
		}
		return Builtin(), err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Corrupt file: keep it (it may hold a hand edit the user is mid-way
		// through) and serve the builtin — never silently clobber it.
		return Builtin(), fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if len(cfg.Modes) == 0 {
		// Empty modes array is still well-formed JSON, but a selector with no
		// groups is useless; treat it like a missing file's semantics.
		return Builtin(), nil
	}
	return cfg, nil
}

// writeBuiltin persists the built-in config atomically-enough for a first-run
// seed: create the parent dir, then write. Best-effort — the caller already
// holds the in-memory builtin to serve regardless.
func writeBuiltin(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// ModeByID returns the mode with the given id.
func (c Config) ModeByID(id string) (Mode, bool) {
	for _, m := range c.Modes {
		if m.ID == id {
			return m, true
		}
	}
	return Mode{}, false
}

// ModesForModel returns every mode id whose list contains modelID. The
// selector uses it to know which group to expand/light up for the current
// model (需求 §5.6 规则 1: 名单允许重叠，一个模型可属多个模式).
func (c Config) ModesForModel(modelID string) []string {
	var out []string
	for _, m := range c.Modes {
		for _, id := range m.Models {
			if id == modelID {
				out = append(out, m.ID)
				break
			}
		}
	}
	return out
}
