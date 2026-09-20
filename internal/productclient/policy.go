package productclient

import (
	"encoding/json"
	"fmt"
)

// This file models the signed policy envelope the platform sends: the account
// contract's dynamic half (中台交付包 §4.3). Field names live here and nowhere
// else, exactly like the rest of this package.
//
// Only what this client consumes is modelled. The envelope may carry more than
// is declared here and that is fine — encoding/json ignores unknown fields — so
// an extra platform field is never a reason to reject a policy.
//
// `capabilities[]` is deliberately NOT modelled (2026-09-15, 人工拍板). The
// capability matrix is permanently unimplemented (需求基线 B7, 待解决问题 D-009):
// entry visibility is decided by web/src/lib/features.ts's static list and the
// tool gate is upstream's internal/permission engine, so nothing would read it.
// A type with no consumer is worse than an absent one — it reads as "we parse
// this and act on it", which invites the platform to implement a field that
// changes nothing on this side (that is what 中台交付包 §4.4 was deleted for).
// If the matrix is ever reopened, the type comes back with its consumer.

// DisplayName is a model's name in each UI language. The platform owns the
// values; the client renders the entry matching the current UI language and
// keeps no id -> name table of its own, because a local table would shadow the
// server's copy and keep showing a stale name after a rename (需求基线 B6).
type DisplayName struct {
	Zh string `json:"zh-CN"`
	En string `json:"en"`
}

// CatalogVendor is the stable parent shown by the model selector. Identity is
// deliberately separate from DisplayName: names may change or be localised,
// while VendorID remains the grouping key stored in the signed contract.
type CatalogVendor struct {
	ID          string      `json:"id"`
	DisplayName DisplayName `json:"displayName"`
}

// CatalogModel is one selectable model. ID is the stable technical key used in
// gateway requests, sessions and ledgers; it is ASCII and never localised.
type CatalogModel struct {
	ReasoningOptions     []string        `json:"reasoningOptions,omitempty"`
	AvailabilityReason   string          `json:"availabilityReason,omitempty"`
	ID                   string          `json:"id"`
	VendorID             string          `json:"vendorId"`
	DisplayName          DisplayName     `json:"displayName"`
	Transport            string          `json:"transport"`
	Capabilities         map[string]bool `json:"capabilities"`
	MaxContextTokens     int             `json:"maxContextTokens"`
	MaxOutputTokens      int             `json:"maxOutputTokens"`
	Eligible             bool            `json:"eligible"`
	Confidential         bool            `json:"confidential"`
	ConfidentialPriority *int            `json:"confidentialPriority,omitempty"`
	PricingVersion       string          `json:"pricingVersion"`
}

// Catalog is the vendor hierarchy plus its model rows. Version is the catalog's
// own version, distinct from the policy version that covers it.
type Catalog struct {
	DefaultModelID string          `json:"defaultModelId,omitempty"`
	Version        string          `json:"version"`
	TTLSec         int             `json:"ttlSec"`
	Vendors        []CatalogVendor `json:"vendors"`
	Models         []CatalogModel  `json:"models"`
}

const (
	CatalogTransportGateway = "gateway"
	MinConfidentialPriority = 0
	MaxConfidentialPriority = 1_000_000
)

// ValidateCatalog applies the semantic half of the signed catalog contract.
// Signature verification proves who sent the bytes; it does not make broken
// references or contradictory eligibility safe to consume. Any invalid row
// rejects the whole catalog so different consumers cannot repair it in
// different ways.
func ValidateCatalog(c Catalog) error {
	vendors := make(map[string]struct{}, len(c.Vendors))
	for _, vendor := range c.Vendors {
		if vendor.ID == "" {
			return fmt.Errorf("%w: catalog vendor id is empty", ErrPolicyMalformed)
		}
		if _, duplicate := vendors[vendor.ID]; duplicate {
			return fmt.Errorf("%w: duplicate catalog vendor id %q", ErrPolicyMalformed, vendor.ID)
		}
		vendors[vendor.ID] = struct{}{}
	}

	models := make(map[string]struct{}, len(c.Models))
	for _, model := range c.Models {
		if model.ID == "" {
			return fmt.Errorf("%w: catalog model id is empty", ErrPolicyMalformed)
		}
		if _, duplicate := models[model.ID]; duplicate {
			return fmt.Errorf("%w: duplicate catalog model id %q", ErrPolicyMalformed, model.ID)
		}
		models[model.ID] = struct{}{}
		if _, known := vendors[model.VendorID]; !known {
			return fmt.Errorf("%w: catalog model %q references unknown vendor %q", ErrPolicyMalformed, model.ID, model.VendorID)
		}
		if model.Transport != CatalogTransportGateway {
			return fmt.Errorf("%w: catalog model %q has unsupported transport %q", ErrPolicyMalformed, model.ID, model.Transport)
		}
		if model.Confidential && !model.Eligible {
			return fmt.Errorf("%w: confidential catalog model %q is not eligible", ErrPolicyMalformed, model.ID)
		}
		if model.ConfidentialPriority != nil {
			priority := *model.ConfidentialPriority
			if !model.Confidential {
				return fmt.Errorf("%w: non-confidential catalog model %q carries confidentialPriority", ErrPolicyMalformed, model.ID)
			}
			if priority < MinConfidentialPriority || priority > MaxConfidentialPriority {
				return fmt.Errorf("%w: catalog model %q confidentialPriority %d is outside %d..%d", ErrPolicyMalformed, model.ID, priority, MinConfidentialPriority, MaxConfidentialPriority)
			}
		}
	}
	return nil
}

// Policy is the signed envelope's payload.
//
// The validity window is part of the signature on purpose: an unscrupulous
// intermediary can rewrite any field the signature does not cover, which would
// defeat both version monotonicity and policy revocation at once.
type Policy struct {
	PolicyVersion    string  `json:"policyVersion"`
	IssuedAt         string  `json:"issuedAt"`
	ExpiresAt        string  `json:"expiresAt"`
	MinClientVersion string  `json:"minClientVersion"`
	Audience         string  `json:"audience"`
	KeyID            string  `json:"keyId"`
	Catalog          Catalog `json:"catalog"`
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

// IsEmpty reports whether the response carried no envelope at all.
//
// Absence has to be one predicate rather than a length check scattered across
// callers, because `policy` is typed json.RawMessage and Go's decoder stores the
// four bytes of a JSON `null` in it rather than leaving it nil: a response that
// omits the policy and one that sends `"policy": null` would otherwise need two
// different tests, and a caller that knew only one of them would treat "no
// catalog" as a catalog of four bytes.
func (e PolicyEnvelope) IsEmpty() bool {
	switch string(e.Policy) {
	case "", "null":
		return true
	default:
		return false
	}
}

// DecodePolicy parses the envelope's policy bytes WITHOUT verifying them.
//
// It exists for exactly one caller: the catalog projection, which reads back an
// envelope that has already been verified. The cache stores the envelope verbatim
// (需求基线 B2 规则 1) and holds no parsed policy, so re-reading one requires a
// parse - and putting that parse here keeps the policy's field names in their one
// owner (开发规范 §3.8) instead of having productruntime unmarshal a shape it
// does not own.
//
// It must never be the path by which a policy off the wire is read. The trust
// argument is the *caller's*, not this method's: the only bytes it is correct to
// pass are ones that already came out of Verify or off a cache that only Verify
// ever wrote. A caller with a fresh response wants Verify, which is also the one
// place that can answer whether the bytes are genuine.
func (e PolicyEnvelope) DecodePolicy() (Policy, error) {
	var policy Policy
	if err := json.Unmarshal(e.Policy, &policy); err != nil {
		return Policy{}, fmt.Errorf("%w: %v", ErrPolicyMalformed, err)
	}
	return policy, nil
}
