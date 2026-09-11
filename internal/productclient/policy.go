package productclient

import "encoding/json"

// This file models the signed policy envelope the platform sends: the account
// contract's dynamic half (中台交付包 §4.3). Field names live here and nowhere
// else, exactly like the rest of this package.
//
// Only what PR-4a needs to verify is modelled in full. The catalog and
// capabilities are carried as types because they travel inside the signature
// and the verifier must not lose them, but consuming them (projection into the
// picker, entry visibility) is later work.

// DisplayName is a model's name in each UI language. The platform owns the
// values; the client renders the entry matching the current UI language and
// keeps no id -> name table of its own, because a local table would shadow the
// server's copy and keep showing a stale name after a rename (需求基线 B6).
type DisplayName struct {
	Zh string `json:"zh"`
	En string `json:"en"`
}

// CatalogModel is one selectable model. ID is the stable technical key used in
// gateway requests, sessions and ledgers; it is ASCII and never localised.
type CatalogModel struct {
	ID               string          `json:"id"`
	DisplayName      DisplayName     `json:"displayName"`
	ModeIDs          []string        `json:"modeIds"`
	Transport        string          `json:"transport"`
	Capabilities     map[string]bool `json:"capabilities"`
	MaxContextTokens int             `json:"maxContextTokens"`
	MaxOutputTokens  int             `json:"maxOutputTokens"`
	Eligible         bool            `json:"eligible"`
	PricingVersion   string          `json:"pricingVersion"`
}

// CatalogMode carries only the mode metadata a model cannot express. Grouping
// itself (which model sits under which mode) is read from CatalogModel.ModeIDs
// and deliberately not repeated here: two copies of one fact drift apart, and
// the picker would then disagree with itself depending on which it read
// (中台交付包 §4.3「catalog.modes 的形状」, 开发规范 §3.8).
type CatalogMode struct {
	ID             string `json:"id"`
	DefaultModelID string `json:"defaultModelId"`
}

// Catalog is the model list plus the modes it is grouped into. Version is the
// catalog's own version, distinct from the policy version that covers it.
type Catalog struct {
	Version string         `json:"version"`
	TTLSec  int            `json:"ttlSec"`
	Models  []CatalogModel `json:"models"`
	Modes   []CatalogMode  `json:"modes"`
}

// Capability is one entry of the capability matrix. It governs whether a
// feature is offered at all, not merely whether its menu entry is drawn:
// available is architecture, visible is presentation, entitled is the account's
// right to use it, and PermissionPolicy is the runtime gate.
type Capability struct {
	ID               string `json:"id"`
	Available        bool   `json:"available"`
	Visible          bool   `json:"visible"`
	Entitled         bool   `json:"entitled"`
	PermissionPolicy string `json:"permissionPolicy"`
	MinClientVersion string `json:"minClientVersion"`
}

// Policy is the signed envelope's payload.
//
// The validity window is part of the signature on purpose: an unscrupulous
// intermediary can rewrite any field the signature does not cover, which would
// defeat both version monotonicity and policy revocation at once.
type Policy struct {
	PolicyVersion    string       `json:"policyVersion"`
	IssuedAt         string       `json:"issuedAt"`
	ExpiresAt        string       `json:"expiresAt"`
	MinClientVersion string       `json:"minClientVersion"`
	Audience         string       `json:"audience"`
	KeyID            string       `json:"keyId"`
	Catalog          Catalog      `json:"catalog"`
	Capabilities     []Capability `json:"capabilities"`
}

// PolicySignature is the detached signature over Policy's raw bytes.
//
// It is a sibling of the policy, not a member of it. A member would force the
// verifier to deserialise the policy, delete the signature field, and
// re-serialise it — and signer and verifier would then have to agree on a
// canonical form to the byte. Signing the bytes that travel removes that whole
// class of disagreement; see D-007 for when and how to move to JCS instead.
type PolicySignature struct {
	KeyID string `json:"keyId"`
	Sig   string `json:"sig"`
}

// PolicyEnvelope is the signed policy as it travels inside a response: the
// policy value's raw bytes beside its detached signature.
//
// Policy is json.RawMessage, not Policy, and that is load-bearing: it keeps the
// bytes the platform signed exactly as they arrived, so verification does not
// depend on this package's struct tags matching the platform's field order or
// number formatting. Note that a version field placed outside this envelope is
// not modelled at all — only the signed copy may be read as authoritative.
type PolicyEnvelope struct {
	Policy    json.RawMessage `json:"policy"`
	Signature PolicySignature `json:"policySignature"`
}
