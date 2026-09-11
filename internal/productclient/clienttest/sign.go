package clienttest

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
)

// The stand-in has to sign the policy envelope, because a client that verifies
// signatures is useless without a signer to test against. This file is that
// signer: a fixture key pair and a fixture policy, both deliberately small.
//
// The key pair is a TEST key. Its public half is compiled into the developer
// runtime profile (profiles/developer.json) as the only trusted key, which is
// what makes the developer build able to verify the stand-in at all. The
// production profile carries a different, real key and is unaffected; a test
// asserts the two halves stay in step (profile_key_test.go).

// FixtureSigningKeyID is the keyId the stand-in signs under. It must appear in
// developer.json's trustedKeyIDs or a developer build rejects every policy.
const FixtureSigningKeyID = "dev-policy-2026-a"

// FixtureSigningKeySeed is the base64 32-byte ed25519 seed behind that key.
// Recorded so the pair is reproducible; it protects nothing, and it is trusted
// by no build that ships.
const FixtureSigningKeySeed = "v8zF/jeDm1+T+6S85E2jRhppX9tzXLHijUaa7sKpMZI="

// FixturePolicyAudience is the audience the fixture policy is addressed to.
// The platform's real value is contract data owned by the caller of Verify;
// this constant exists so the stand-in and its tests agree on one spelling.
const FixturePolicyAudience = "puddingbox"

// fixtureSigningKey decodes the seed once. A malformed compile-time constant is
// a programming error, not a runtime condition, so it panics at init exactly
// like the embedded runtime profiles do.
var fixtureSigningKey = mustFixtureSigningKey()

func mustFixtureSigningKey() ed25519.PrivateKey {
	seed, err := base64.StdEncoding.DecodeString(FixtureSigningKeySeed)
	if err != nil || len(seed) != ed25519.SeedSize {
		panic("clienttest: FixtureSigningKeySeed must be base64 of 32 bytes")
	}
	return ed25519.NewKeyFromSeed(seed)
}

// FixtureSigningKey returns the stand-in's signing key.
func FixtureSigningKey() ed25519.PrivateKey { return fixtureSigningKey }

// FixtureSigningPublicKey returns its base64 public half — the value
// developer.json must carry under FixtureSigningKeyID.
func FixtureSigningPublicKey() string {
	return base64.StdEncoding.EncodeToString(fixtureSigningKey.Public().(ed25519.PublicKey))
}

// FixturePolicy is the policy the stand-in signs.
//
// Its models use `buding-*` ids: fixed ASCII data keys that do not follow the
// English brand name and must match what the gateway and ledger see, so they are
// an annotated exception to the no-brand-literal rule (开发规范 §3.1 规则 2).
//
// It is intentionally a small catalog — enough for a picker to have something to
// show and for the three product modes to be populated. A richer fixture
// (every eligibility and degradation state) belongs with the catalog projection
// work, not with signature verification.
func FixturePolicy(now time.Time) productclient.Policy {
	issued := now.UTC().Add(-time.Minute).Format(time.RFC3339)
	expires := now.UTC().Add(time.Hour).Format(time.RFC3339)

	return productclient.Policy{
		PolicyVersion: "2026-09-11.1",
		IssuedAt:      issued,
		ExpiresAt:     expires,
		Audience:      FixturePolicyAudience,
		KeyID:         FixtureSigningKeyID,
		Catalog: productclient.Catalog{
			Version: "2026-09-11.1",
			TTLSec:  3600,
			Models: []productclient.CatalogModel{
				{
					ID:               "buding-privacy-1",
					DisplayName:      productclient.DisplayName{Zh: "布丁隐私版", En: "Pudding Private"},
					ModeIDs:          []string{"privacy"},
					Transport:        "gateway",
					Capabilities:     map[string]bool{"stream": true, "tools": true},
					MaxContextTokens: 32000,
					MaxOutputTokens:  4096,
					Eligible:         true,
					PricingVersion:   "2026-09-a",
				},
				{
					ID:               "buding-cloud-pro",
					DisplayName:      productclient.DisplayName{Zh: "布丁专业版", En: "Pudding Pro"},
					ModeIDs:          []string{"smart", "default"},
					Transport:        "gateway",
					Capabilities:     map[string]bool{"stream": true, "tools": true, "vision": true},
					MaxContextTokens: 128000,
					MaxOutputTokens:  8192,
					Eligible:         true,
					PricingVersion:   "2026-09-a",
				},
				{
					ID:               "buding-cloud-fast",
					DisplayName:      productclient.DisplayName{Zh: "布丁快速版", En: "Pudding Fast"},
					ModeIDs:          []string{"default"},
					Transport:        "gateway",
					Capabilities:     map[string]bool{"stream": true},
					MaxContextTokens: 32000,
					MaxOutputTokens:  4096,
					Eligible:         true,
					PricingVersion:   "2026-09-a",
				},
			},
			Modes: []productclient.CatalogMode{
				{ID: "privacy", DefaultModelID: "buding-privacy-1"},
				{ID: "smart", DefaultModelID: "buding-cloud-pro"},
				{ID: "default", DefaultModelID: "buding-cloud-fast"},
			},
		},
		Capabilities: []productclient.Capability{
			{ID: "cloud_chat", Available: true, Visible: true, Entitled: true, PermissionPolicy: "ask"},
		},
	}
}

// signedPolicy builds the envelope as it travels: the policy is marshalled once,
// that exact byte sequence is signed, and the same bytes are what the client
// receives. Marshalling once matters — signing one encoding and sending another
// is the failure this whole scheme is designed to avoid.
func signedPolicy(now time.Time) (productclient.PolicyEnvelope, error) {
	raw, err := json.Marshal(FixturePolicy(now))
	if err != nil {
		return productclient.PolicyEnvelope{}, fmt.Errorf("clienttest: marshal fixture policy: %w", err)
	}
	sig, err := productclient.SignPolicy(raw, FixtureSigningKeyID, fixtureSigningKey)
	if err != nil {
		return productclient.PolicyEnvelope{}, fmt.Errorf("clienttest: sign fixture policy: %w", err)
	}
	return productclient.PolicyEnvelope{Policy: raw, Signature: sig}, nil
}
