package productclient_test

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// testAudience is a fixed ASCII data key, like the platform's `puddingbox`;
// the verifier takes it as an input so this package holds no product literal.
const testAudience = "puddingbox"

func testNow() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }

// testTrustStore is the anchor a developer build compiles in. It is built from
// the stand-in's own public key so the two cannot drift apart silently.
func testTrustStore(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		clienttest.FixtureSigningKeyID: clienttest.FixtureSigningPublicKey(),
	}
}

// policyBytes builds a policy of the shape the platform sends: compact, because
// json.Marshal emits no insignificant whitespace, which is what makes the
// "sign the bytes on the wire" scheme stable through Go's encoder.
func policyBytes(t *testing.T, mutate func(*productclient.Policy)) []byte {
	t.Helper()
	p := productclient.Policy{
		PolicyVersion: "2026-09-11.1",
		IssuedAt:      testNow().Add(-time.Minute).Format(time.RFC3339),
		ExpiresAt:     testNow().Add(time.Hour).Format(time.RFC3339),
		Audience:      testAudience,
		KeyID:         clienttest.FixtureSigningKeyID,
		Catalog: productclient.Catalog{
			Version: "2026-09-11.1",
			TTLSec:  3600,
			Models: []productclient.CatalogModel{{
				ID:          "buding-cloud-pro",
				DisplayName: productclient.DisplayName{Zh: "布丁专业版", En: "Pudding Pro"},
				ModeIDs:     []string{"smart", "default"},
				Transport:   "gateway",
				Eligible:    true,
			}},
		},
	}
	if mutate != nil {
		mutate(&p)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	return b
}

func signedEnvelope(t *testing.T, policy []byte) productclient.PolicyEnvelope {
	t.Helper()
	sig, err := productclient.SignPolicy(policy, clienttest.FixtureSigningKeyID, clienttest.FixtureSigningKey())
	if err != nil {
		t.Fatalf("sign policy: %v", err)
	}
	return productclient.PolicyEnvelope{Policy: policy, Signature: sig}
}

func verifyOpts(t *testing.T) productclient.VerifyOptions {
	t.Helper()
	return productclient.VerifyOptions{
		TrustedKeys: testTrustStore(t),
		Audience:    testAudience,
		Now:         testNow(),
		Skew:        time.Minute,
	}
}

// TestVerifyPolicyAcceptsAStandinSignature is the positive control: without it,
// every rejection test below could be passing for the wrong reason.
func TestVerifyPolicyAcceptsAStandinSignature(t *testing.T) {
	env := signedEnvelope(t, policyBytes(t, nil))

	got, err := env.Verify(verifyOpts(t))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.PolicyVersion != "2026-09-11.1" {
		t.Errorf("PolicyVersion = %q, want %q", got.PolicyVersion, "2026-09-11.1")
	}
	if got.Audience != testAudience {
		t.Errorf("Audience = %q, want %q", got.Audience, testAudience)
	}
	if len(got.Catalog.Models) != 1 || got.Catalog.Models[0].ID != "buding-cloud-pro" {
		t.Errorf("catalog did not survive verification: %+v", got.Catalog.Models)
	}
	// The signed bytes must be readable back exactly: a re-serialising decoder
	// would break the scheme, so pin that the raw value is what was signed.
	if !json.Valid(env.Policy) {
		t.Error("envelope kept no valid raw policy bytes")
	}
}

// TestVerifyPolicyRejectsATamperedPolicy is the core guarantee: one flipped
// byte anywhere in the signed value invalidates it.
func TestVerifyPolicyRejectsATamperedPolicy(t *testing.T) {
	policy := policyBytes(t, nil)
	env := signedEnvelope(t, policy)

	tampered := append([]byte(nil), policy...)
	i := strings.Index(string(tampered), `"gateway"`)
	if i < 0 {
		t.Fatal("fixture no longer contains the value this test tampers with")
	}
	tampered[i] = 'G'
	env.Policy = tampered

	if _, err := env.Verify(verifyOpts(t)); !errors.Is(err, productclient.ErrBadSignature) {
		t.Fatalf("Verify error = %v, want ErrBadSignature", err)
	}
}

func TestVerifyPolicyRejectsAnUntrustedKeyID(t *testing.T) {
	env := signedEnvelope(t, policyBytes(t, nil))

	opts := verifyOpts(t)
	opts.TrustedKeys = map[string]string{"some-other-key": opts.TrustedKeys[clienttest.FixtureSigningKeyID]}

	if _, err := env.Verify(opts); !errors.Is(err, productclient.ErrUntrustedKey) {
		t.Fatalf("Verify error = %v, want ErrUntrustedKey", err)
	}
}

// TestVerifyPolicyRejectsASignatureMadeByAnotherKey pins that the keyId is not
// taken on trust: a signature must verify against the key the id names, and the
// anchor is compiled in rather than read from the response.
//
// The attack modelled here is the real one: an intermediary signs with a key it
// controls while claiming the id of the trusted one. Swapping the anchor as
// well is not modelled, because it is not available to an attacker — the
// trust store comes from the runtime profile, never from the wire.
func TestVerifyPolicyRejectsASignatureMadeByAnotherKey(t *testing.T) {
	policy := policyBytes(t, nil)

	_, otherPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sig, err := productclient.SignPolicy(policy, clienttest.FixtureSigningKeyID, otherPriv)
	if err != nil {
		t.Fatalf("SignPolicy: %v", err)
	}
	env := productclient.PolicyEnvelope{Policy: policy, Signature: sig}

	if _, err := env.Verify(verifyOpts(t)); !errors.Is(err, productclient.ErrBadSignature) {
		t.Fatalf("Verify error = %v, want ErrBadSignature", err)
	}
}

func TestVerifyPolicyRejectsAKeyIDMismatch(t *testing.T) {
	// The policy claims one key; the detached signature names another. The
	// contract says the two must agree, and disagreement is a verification
	// failure rather than something to resolve in either direction.
	env := signedEnvelope(t, policyBytes(t, func(p *productclient.Policy) {
		p.KeyID = "policy-2027-b"
	}))

	if _, err := env.Verify(verifyOpts(t)); !errors.Is(err, productclient.ErrBadSignature) {
		t.Fatalf("Verify error = %v, want ErrBadSignature", err)
	}
}

func TestVerifyPolicyRejectsAnotherAudience(t *testing.T) {
	// A valid signature over a policy addressed to a different product: the
	// bytes are genuine, but replaying them here must not work.
	env := signedEnvelope(t, policyBytes(t, func(p *productclient.Policy) {
		p.Audience = "some-other-product"
	}))

	if _, err := env.Verify(verifyOpts(t)); !errors.Is(err, productclient.ErrAudience) {
		t.Fatalf("Verify error = %v, want ErrAudience", err)
	}
}

func TestVerifyPolicyRejectsOutsideItsWindow(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*productclient.Policy)
	}{
		{"expired", func(p *productclient.Policy) {
			p.IssuedAt = testNow().Add(-2 * time.Hour).Format(time.RFC3339)
			p.ExpiresAt = testNow().Add(-time.Hour).Format(time.RFC3339)
		}},
		{"not yet valid", func(p *productclient.Policy) {
			p.IssuedAt = testNow().Add(time.Hour).Format(time.RFC3339)
			p.ExpiresAt = testNow().Add(2 * time.Hour).Format(time.RFC3339)
		}},
		{"expired past the allowed skew", func(p *productclient.Policy) {
			p.IssuedAt = testNow().Add(-2 * time.Hour).Format(time.RFC3339)
			p.ExpiresAt = testNow().Add(-90 * time.Second).Format(time.RFC3339)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := signedEnvelope(t, policyBytes(t, tc.mutate))
			if _, err := env.Verify(verifyOpts(t)); !errors.Is(err, productclient.ErrClockWindow) {
				t.Fatalf("Verify error = %v, want ErrClockWindow", err)
			}
		})
	}
}

// TestVerifyPolicyToleratesClockSkewWithinTheBound separates "the clock is a
// little off" from "the policy is over", so a skewed host is not an outage.
func TestVerifyPolicyToleratesClockSkewWithinTheBound(t *testing.T) {
	env := signedEnvelope(t, policyBytes(t, func(p *productclient.Policy) {
		p.IssuedAt = testNow().Add(-time.Hour).Format(time.RFC3339)
		p.ExpiresAt = testNow().Add(-30 * time.Second).Format(time.RFC3339)
	}))

	if _, err := env.Verify(verifyOpts(t)); err != nil {
		t.Fatalf("Verify within the skew bound: %v", err)
	}
}

// TestVerifyPolicyFailsClosedOnMalformedInput covers the states where "no
// answer" must never become "accepted": an unparsable timestamp is not an
// absent window, and a missing signature is not an unsigned-but-fine policy.
func TestVerifyPolicyFailsClosedOnMalformedInput(t *testing.T) {
	cases := []struct {
		name  string
		env   func(*testing.T) productclient.PolicyEnvelope
		opts  func(*testing.T) productclient.VerifyOptions
		wants error
	}{
		{
			name: "no signature at all",
			env: func(t *testing.T) productclient.PolicyEnvelope {
				return productclient.PolicyEnvelope{Policy: policyBytes(t, nil)}
			},
			opts:  verifyOpts,
			wants: productclient.ErrPolicyMalformed,
		},
		{
			name: "signature without a key id",
			env: func(t *testing.T) productclient.PolicyEnvelope {
				env := signedEnvelope(t, policyBytes(t, nil))
				env.Signature.KeyID = ""
				return env
			},
			opts:  verifyOpts,
			wants: productclient.ErrPolicyMalformed,
		},
		{
			name: "no trusted keys configured",
			env: func(t *testing.T) productclient.PolicyEnvelope {
				return signedEnvelope(t, policyBytes(t, nil))
			},
			opts: func(t *testing.T) productclient.VerifyOptions {
				o := verifyOpts(t)
				o.TrustedKeys = nil
				return o
			},
			wants: productclient.ErrUntrustedKey,
		},
		{
			name: "no expected audience configured",
			env: func(t *testing.T) productclient.PolicyEnvelope {
				return signedEnvelope(t, policyBytes(t, nil))
			},
			opts: func(t *testing.T) productclient.VerifyOptions {
				o := verifyOpts(t)
				o.Audience = ""
				return o
			},
			wants: productclient.ErrPolicyMalformed,
		},
		{
			name: "unparsable issuedAt",
			env: func(t *testing.T) productclient.PolicyEnvelope {
				return signedEnvelope(t, policyBytes(t, func(p *productclient.Policy) {
					p.IssuedAt = "sometime last week"
				}))
			},
			opts:  verifyOpts,
			wants: productclient.ErrPolicyMalformed,
		},
		{
			name: "unparsable expiresAt",
			env: func(t *testing.T) productclient.PolicyEnvelope {
				return signedEnvelope(t, policyBytes(t, func(p *productclient.Policy) {
					p.ExpiresAt = ""
				}))
			},
			opts:  verifyOpts,
			wants: productclient.ErrPolicyMalformed,
		},
		{
			name: "signature is not base64",
			env: func(t *testing.T) productclient.PolicyEnvelope {
				env := signedEnvelope(t, policyBytes(t, nil))
				env.Signature.Sig = "not base64!!"
				return env
			},
			opts:  verifyOpts,
			wants: productclient.ErrBadSignature,
		},
		{
			name: "policy is not JSON",
			env: func(t *testing.T) productclient.PolicyEnvelope {
				return signedEnvelope(t, []byte("this is not json"))
			},
			opts:  verifyOpts,
			wants: productclient.ErrPolicyMalformed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.env(t).Verify(tc.opts(t))
			if !errors.Is(err, tc.wants) {
				t.Fatalf("Verify error = %v, want %v", err, tc.wants)
			}
		})
	}
}

// TestVerifyPolicyReadsVersionOnlyFromTheSignedEnvelope pins the security note
// in 中台交付包 §4.3: anything outside the signed policy is attacker-writable,
// so the version the verifier surfaces must come from inside the signature.
func TestVerifyPolicyReadsVersionOnlyFromTheSignedEnvelope(t *testing.T) {
	policy := policyBytes(t, nil)
	env := signedEnvelope(t, policy)

	// A full response as it would arrive: the signed envelope, plus an unsigned
	// sibling claiming a much newer version.
	raw := []byte(`{"policy":` + string(policy) +
		`,"policySignature":{"keyId":"` + env.Signature.KeyID + `","sig":"` + env.Signature.Sig + `"}` +
		`,"policyVersion":"2099-01-01.9"}`)

	var decoded productclient.PolicyEnvelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	got, err := decoded.Verify(verifyOpts(t))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.PolicyVersion != "2026-09-11.1" {
		t.Fatalf("PolicyVersion = %q; the unsigned sibling field must not be readable through this type", got.PolicyVersion)
	}
}
