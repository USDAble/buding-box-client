// Package chatmode owns the product's three model-picker groups.
//
// It holds the mode ids and their presentation order and nothing else: the models
// inside each group are a projection of the signed catalog, and the group's
// display name is interface copy that lives in the frontend's i18n
// (需求基线 B5 规则 1 — "展示名是界面文案、分组归属来自目录，两者不得互相推导").
//
// The three ids are fixed ASCII data keys, not brand strings: they are persisted
// on sessions and compared by the platform and by the frontend, so they must not
// move when the product is renamed in English (开发规范 §3.1 规则 2, the
// annotated `buding-*` exception).
//
// Why this is a package rather than three constants next to their first consumer:
// the set is a product constant that two sides of the contract already depend on
// independently — the catalog may not add a mode (中台交付包 §4.3) and the picker
// must not offer one — and E2E闭环清单.md §4 names this package as the single
// owner of the grouping fact. A copy elsewhere would be the second definition
// 开发规范 §3.8 forbids.
//
// OCTO-FORK: 模式分组（需求基线 B5）— see dev-docs-usdable/需求/20260911/开发计划.md (PR-4d).
package chatmode

// Mode ids, in the order the picker shows them.
const (
	// ModePrivacy is the privacy group.
	ModePrivacy = "privacy"
	// ModeSmart is the group named "智能" — a grouping label, not the generic
	// notion of "a smart model". The two meanings are kept apart deliberately.
	ModeSmart = "smart"
	// ModeDefault is the group named "默认" — likewise a grouping label, not
	// "the system default". 需求 §3 calls this ambiguity out explicitly.
	ModeDefault = "default"
)

// IDs returns the product's mode ids in presentation order.
//
// The returned slice is a copy: callers range over it, and a caller that sorted
// or truncated it in place would silently reorder the picker for everyone else.
func IDs() []string {
	return []string{ModePrivacy, ModeSmart, ModeDefault}
}

// IsProductMode reports whether id names one of the product's groups.
//
// This is the predicate behind "a catalog naming an unknown mode does not get to
// invent one" (中台交付包 §4.3). It is exact rather than normalising: a value that
// needs trimming or case-folding to match is not a mode id, and accepting it
// would let a platform typo become a fourth group.
func IsProductMode(id string) bool {
	switch id {
	case ModePrivacy, ModeSmart, ModeDefault:
		return true
	default:
		return false
	}
}
