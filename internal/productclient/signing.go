package productclient

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Signing and verification of the policy envelope (中台交付包 §4.3).
//
// There is exactly one implementation, used by both the signer side (the
// stand-in, and tests) and the verifier side (this client). Two implementations
// would drift, and the failure mode of that drift is the worst kind: green
// tests against the stand-in and a verification failure in production, because
// both sides were consistently wrong in the same way (开发计划 §3.3).
//
// The scheme is deliberately not JCS (RFC 8785). What is signed is the exact
// byte sequence the platform puts on the wire, and what is verified is the
// exact byte sequence that arrived. Signer and verifier therefore cannot
// disagree about the "message" — only about the bytes, which are the message.
// The cost is a sensitivity to intermediaries that re-serialise JSON, which
// this product does not have; see D-007 for the conditions that would change
// that and the four rules to follow when they do.

var (
	// ErrPolicyMalformed means the envelope cannot be tested at all: no
	// signature, an unusable window, or a local misconfiguration such as a
	// missing expected audience. It is never a reason to accept the policy.
	ErrPolicyMalformed = errors.New("productclient: policy envelope is malformed")

	// ErrUntrustedKey means the signature names a key this build does not
	// trust, or one whose recorded value is unusable. Retrying will not help:
	// the anchor is compiled in.
	ErrUntrustedKey = errors.New("productclient: policy signed by an untrusted key")

	// ErrBadSignature means the bytes do not match the signature. Either the
	// policy was altered in transit or the signature does not belong to it.
	ErrBadSignature = errors.New("productclient: policy signature does not verify")

	// ErrAudience means a genuine signature over a policy addressed to a
	// different product. Replaying it here must not work.
	ErrAudience = errors.New("productclient: policy audience is not this product")

	// ErrClockWindow means the policy is outside its validity window, allowing
	// for the skew the caller permits.
	ErrClockWindow = errors.New("productclient: policy is outside its validity window")
)

// VerifyOptions are everything verification needs beyond the envelope itself.
//
// TrustedKeys and Audience are inputs rather than package constants so that
// this package holds no product identity and no key material: the anchor is
// internal/productprofile's, and the audience is contract data the caller
// owns. Both are required — an unset value fails closed rather than silently
// disabling the check it governs.
type VerifyOptions struct {
	// TrustedKeys maps keyId to a base64 ed25519 public key, as compiled into
	// the runtime profile.
	TrustedKeys map[string]string

	// Audience is the product this client expects the policy to be addressed
	// to. A policy for another product is rejected even if its signature is
	// perfect.
	Audience string

	// Now is the verification clock. A zero value will fail the window check,
	// which is the intended fail-closed direction.
	Now time.Time

	// Skew is how far the local clock may be off the platform's. Pass 0 for a
	// strict window.
	Skew time.Duration
}

// Verify checks the envelope's signature and returns the policy it protects.
//
// Callers must use the returned policy, never their own parse of the raw
// bytes: the checks below (audience, window, key identity) are only meaningful
// on data the signature actually covers.
func (e PolicyEnvelope) Verify(opts VerifyOptions) (Policy, error) {
	var zero Policy

	// Local misconfiguration first: a missing audience or an empty anchor means
	// the caller cannot answer the question at all, and "no answer" must not
	// read as "fine".
	if opts.Audience == "" {
		return zero, fmt.Errorf("%w: no expected audience configured", ErrPolicyMalformed)
	}
	if len(opts.TrustedKeys) == 0 {
		return zero, fmt.Errorf("%w: no trusted keys configured", ErrUntrustedKey)
	}
	if e.Signature.KeyID == "" || e.Signature.Sig == "" {
		return zero, fmt.Errorf("%w: envelope carries no signature", ErrPolicyMalformed)
	}
	if len(e.Policy) == 0 {
		return zero, fmt.Errorf("%w: envelope carries no policy", ErrPolicyMalformed)
	}

	pubB64, ok := opts.TrustedKeys[e.Signature.KeyID]
	if !ok {
		return zero, fmt.Errorf("%w: %q", ErrUntrustedKey, e.Signature.KeyID)
	}
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return zero, fmt.Errorf("%w: %q has an unusable public key", ErrUntrustedKey, e.Signature.KeyID)
	}
	sig, err := base64.StdEncoding.DecodeString(e.Signature.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return zero, fmt.Errorf("%w: signature is not a valid ed25519 value", ErrBadSignature)
	}

	// The signature is checked before anything it covers is interpreted: the
	// window and audience below are attacker-writable until this passes.
	if !ed25519.Verify(ed25519.PublicKey(pub), e.Policy, sig) {
		return zero, ErrBadSignature
	}

	var policy Policy
	if err := json.Unmarshal(e.Policy, &policy); err != nil {
		return zero, fmt.Errorf("%w: %v", ErrPolicyMalformed, err)
	}

	// The policy names a key and the detached signature names one too. They
	// must agree; if they do not, one of them is not what its author signed.
	if policy.KeyID != e.Signature.KeyID {
		return zero, fmt.Errorf("%w: policy names key %q, signature names %q",
			ErrBadSignature, policy.KeyID, e.Signature.KeyID)
	}

	if policy.Audience != opts.Audience {
		return zero, fmt.Errorf("%w: %q", ErrAudience, policy.Audience)
	}

	issuedAt, err := time.Parse(time.RFC3339, policy.IssuedAt)
	if err != nil {
		return zero, fmt.Errorf("%w: issuedAt %q", ErrPolicyMalformed, policy.IssuedAt)
	}
	expiresAt, err := time.Parse(time.RFC3339, policy.ExpiresAt)
	if err != nil {
		return zero, fmt.Errorf("%w: expiresAt %q", ErrPolicyMalformed, policy.ExpiresAt)
	}
	if opts.Now.Before(issuedAt.Add(-opts.Skew)) || opts.Now.After(expiresAt.Add(opts.Skew)) {
		return zero, fmt.Errorf("%w: valid %s..%s, now %s (skew %s)",
			ErrClockWindow, policy.IssuedAt, policy.ExpiresAt, opts.Now.Format(time.RFC3339), opts.Skew)
	}

	return policy, nil
}

// SignPolicy produces the detached signature for policy. It is the other half
// of Verify, in the same file on purpose so the two cannot diverge.
//
// keyID is recorded beside the signature but is also expected to appear inside
// the policy; Verify rejects an envelope where the two disagree.
func SignPolicy(policy []byte, keyID string, priv ed25519.PrivateKey) (PolicySignature, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return PolicySignature{}, fmt.Errorf("productclient: signing key must be %d bytes, got %d",
			ed25519.PrivateKeySize, len(priv))
	}
	return PolicySignature{
		KeyID: keyID,
		Sig:   base64.StdEncoding.EncodeToString(ed25519.Sign(priv, policy)),
	}, nil
}
