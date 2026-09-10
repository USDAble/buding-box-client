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

// Builtin returns the factory grouping: the three product mode ids, with NO
// model ids.
//
// Modes are product-fixed (需求 §5.2.4「标识符例外」— the ids are data keys, not
// brand strings). The models *inside* them are not: under the P0 design they
// are a projection of the signed catalog downloaded from the central platform
// (P0-04), so any id hardcoded here would list a model that no shipped build
// can actually select.
//
// History — why the four `buding-*` ids were removed instead of kept as a
// "known transitional state": they used to be registered into config.yml by
// ensureLocalEndpoint(), which was deleted on 2026-09-11. Because the packaged
// data/chat-modes.json is `{}`, Load falls back to this function, so every
// packaged build listed four models with an empty compositeId — including
// `default`'s defaultModel. A factory default that cannot be selected is a
// defect, not a state to document. P0-04 only has to ADD the projection now.
func Builtin() Config {
	return Config{
		SchemaVersion: 1,
		Modes: []Mode{
			{ID: ModePrivacy},
			{ID: ModeSmart},
			{ID: ModeDefault},
		},
	}
}

// Load reads the mode config at path.
//
// A MISSING file is not an error and is NOT written: the caller gets the
// factory grouping (Builtin) and nothing touches the disk. This deliberately
// follows the sensitive-dictionary pattern — missing means "use the default",
// not "materialize the default". The old behaviour seeded data/chat-modes.json
// on first load so the user could see and edit it, but that rationale died with
// the hardcoded model ids: Builtin() now contains only the three fixed mode
// ids, so a seeded file would show the user three empty groups and give them
// nothing to edit. Under P0-04 the modes are a projection of the signed
// catalog, so a locally seeded file would also be an override that could
// silently empty the selector. See 开发规范 §3.9.1 (no startup writes of
// user-editable files).
//
// A present-but-unparseable file degrades to the built-in default WITHOUT
// overwriting the user's file, returning ErrUnreadable alongside it so the
// caller can surface a one-time hint. A genuine read error is returned as-is
// with the built-in config.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Builtin(), nil
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
