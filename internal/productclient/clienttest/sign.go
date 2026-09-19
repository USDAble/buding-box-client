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
//
// It must equal the audience this build answers to,
// brand.Load().BrandID - sign_test.go asserts it, because the two values are
// edited in different files and a drift between them rejects every catalog.
const FixturePolicyAudience = "puddingbox"

// FixturePolicyVersion is the catalog version the fixture policy carries.
//
// It is a fixed ASCII data key rather than a date computed at run time: the
// monotonicity rule is about a version moving forward, so a test has to be able
// to name a version and then serve an older one
// (Server.SetCatalogVersion).
const FixturePolicyVersion = "2026-09-11.1"

// FixtureCatalogTTLSec is the catalog's own freshness claim in the fixture
// policy, one hour, matching the signature window above it. Both axes are
// asserted against it: 需求基线 B2 rule 1 takes the earlier of the two, so a
// fixture with only one of them set would not exercise the rule.
const FixtureCatalogTTLSec = 3600

// FixtureDictionaryVersion is the first signed server-dictionary snapshot.
const FixtureDictionaryVersion = "43"

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
	return fixturePolicy(now, FixturePolicyVersion, FixturePolicyAudience, FixtureCatalogTTLSec, "")
}

// fixturePolicy is FixturePolicy with the fields a fault injection moves: the
// catalog version (a rollback), the audience (a policy addressed to another
// product), the catalog's own freshness claim (an absurd ttlSec), and the id of
// one model to mark ineligible (a model the platform has taken away). Everything
// else stays fixed so a failure can only come from the field the test moved.
//
// ttlSec <= 0 means FixtureCatalogTTLSec, the same bound FixturePolicy carries.
// ineligibleModel == "" means the healthy catalog, where every model is offered.
func fixturePolicy(now time.Time, version, audience string, ttlSec int, ineligibleModel string) productclient.Policy {
	if ttlSec <= 0 {
		ttlSec = FixtureCatalogTTLSec
	}
	issued := now.UTC().Add(-time.Minute).Format(time.RFC3339)
	expires := now.UTC().Add(time.Hour).Format(time.RFC3339)

	// offered is the eligibility rule for one fixture model id. It is written
	// once so the three catalog entries cannot disagree about it, and so
	// "no model is withdrawn" (ineligibleModel == "") needs no special case: the
	// empty id names nothing, so every real id is offered.
	offered := func(id string) bool { return id != ineligibleModel }

	return productclient.Policy{
		PolicyVersion: version,
		IssuedAt:      issued,
		ExpiresAt:     expires,
		Audience:      audience,
		KeyID:         FixtureSigningKeyID,
		Catalog: productclient.Catalog{
			Version: version,
			TTLSec:  ttlSec,
			Vendors: []productclient.CatalogVendor{
				{ID: "buding", DisplayName: productclient.DisplayName{Zh: "布丁模型", En: "Pudding Models"}},
				{ID: "partner", DisplayName: productclient.DisplayName{Zh: "合作模型", En: "Partner Models"}},
			},
			Models: []productclient.CatalogModel{
				{
					ID:                   "buding-privacy-1",
					VendorID:             "buding",
					DisplayName:          productclient.DisplayName{Zh: "布丁隐私版", En: "Pudding Private"},
					Transport:            productclient.CatalogTransportGateway,
					Capabilities:         map[string]bool{"stream": true, "tools": true},
					MaxContextTokens:     32000,
					MaxOutputTokens:      4096,
					Eligible:             offered("buding-privacy-1"),
					Confidential:         offered("buding-privacy-1"),
					ConfidentialPriority: intPtr(100),
					PricingVersion:       "2026-09-a",
				},
				{
					ID:               "buding-cloud-pro",
					VendorID:         "buding",
					DisplayName:      productclient.DisplayName{Zh: "布丁专业版", En: "Pudding Pro"},
					Transport:        productclient.CatalogTransportGateway,
					Capabilities:     map[string]bool{"stream": true, "tools": true, "vision": true},
					MaxContextTokens: 128000,
					MaxOutputTokens:  8192,
					Eligible:         offered("buding-cloud-pro"),
					PricingVersion:   "2026-09-a",
				},
				{
					ID:               "buding-cloud-fast",
					VendorID:         "partner",
					DisplayName:      productclient.DisplayName{Zh: "布丁快速版", En: "Pudding Fast"},
					Transport:        productclient.CatalogTransportGateway,
					Capabilities:     map[string]bool{"stream": true},
					MaxContextTokens: 32000,
					MaxOutputTokens:  4096,
					Eligible:         offered("buding-cloud-fast"),
					PricingVersion:   "2026-09-a",
				},
			},
		},
	}
}

func intPtr(v int) *int { return &v }

// signedPolicy builds the envelope as it travels: the policy is marshalled once,
// that exact byte sequence is signed, and the same bytes are what the client
// receives. Marshalling once matters — signing one encoding and sending another
// is the failure this whole scheme is designed to avoid.
func signedPolicy(now time.Time) (productclient.PolicyEnvelope, error) {
	return signedPolicyFor(now, FixturePolicyVersion, FixturePolicyAudience, false, FixtureCatalogTTLSec, "")
}

// signedPolicyFor signs a policy with the given identity fields, optionally
// tampering with the payload afterwards (Server.TamperPolicy) and optionally
// withdrawing one model (Server.SetModelIneligible).
func signedPolicyFor(now time.Time, version, audience string, tamper bool, ttlSec int, ineligibleModel string) (productclient.PolicyEnvelope, error) {
	raw, err := json.Marshal(fixturePolicy(now, version, audience, ttlSec, ineligibleModel))
	if err != nil {
		return productclient.PolicyEnvelope{}, fmt.Errorf("clienttest: marshal fixture policy: %w", err)
	}
	sig, err := productclient.SignPolicy(raw, FixtureSigningKeyID, fixtureSigningKey)
	if err != nil {
		return productclient.PolicyEnvelope{}, fmt.Errorf("clienttest: sign fixture policy: %w", err)
	}
	if tamper {
		upperFirstLetter(raw)
	}
	return productclient.PolicyEnvelope{Policy: raw, Signature: sig}, nil
}

func signedDictionary(now time.Time, version, keyID string, words []string, digest string, tamper bool) (productclient.SensitiveDictionaryEnvelope, error) {
	if digest == "" {
		digest = productclient.SensitiveDictionarySHA256(words)
	}
	dictionary := productclient.SensitiveDictionary{
		Version:   version,
		Mode:      "full",
		Words:     words,
		SHA256:    digest,
		KeyID:     keyID,
		Audience:  FixturePolicyAudience,
		IssuedAt:  now.UTC().Add(-time.Minute).Format(time.RFC3339),
		ExpiresAt: now.UTC().Add(time.Hour).Format(time.RFC3339),
	}
	raw, err := json.Marshal(dictionary)
	if err != nil {
		return productclient.SensitiveDictionaryEnvelope{}, fmt.Errorf("clienttest: marshal fixture dictionary: %w", err)
	}
	sig, err := productclient.SignPayload(raw, keyID, fixtureSigningKey)
	if err != nil {
		return productclient.SensitiveDictionaryEnvelope{}, fmt.Errorf("clienttest: sign fixture dictionary: %w", err)
	}
	if tamper {
		upperFirstLetter(raw)
	}
	return productclient.SensitiveDictionaryEnvelope{Dictionary: raw, DictionarySignature: sig}, nil
}

// upperFirstLetter changes exactly one byte inside an otherwise valid policy.
// Uppercasing a letter is enough: the payload still parses, so the only thing
// that can reject it is the signature - which is the distinction between "an
// intermediary rewrote this" and "the signature itself is corrupt".
func upperFirstLetter(raw []byte) {
	for i, b := range raw {
		if b >= 'a' && b <= 'z' {
			raw[i] = b - ('a' - 'A')
			return
		}
	}
}
