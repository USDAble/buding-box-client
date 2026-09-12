package productclient_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// The bootstrap response is one `data` object carrying two things: the account
// summary and the signed policy envelope (中台交付包 §4.3). The account half was
// decoded from the beginning; this file pins the other half, because a client
// that silently drops the envelope would pass every account test in this
// package and still leave the picker with no catalog to show - which is the
// first requirement this fork ever wrote down.

// TestBootstrapCarriesTheCatalogEnvelope is the client half of 缺口 ①: the
// envelope arrives, and the policy inside it verifies against the key and the
// audience this build trusts.
func TestBootstrapCarriesTheCatalogEnvelope(t *testing.T) {
	h := newHarness(t)
	if _, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode); err != nil {
		t.Fatalf("first activation: %v", err)
	}

	data, err := h.client.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	// Both halves in one assertion, because embedding the envelope is the change
	// that could break the account fields: a struct whose JSON tags collide with
	// the envelope's would decode one and lose the other.
	if data.Account.PhoneMasked == "" {
		t.Error("the account half of bootstrap stopped decoding")
	}
	if data.Activation == nil || data.Activation.BoxCode != clienttest.FixtureBoxCode {
		t.Errorf("the activation half of bootstrap stopped decoding: %+v", data.Activation)
	}
	if data.IsEmpty() {
		t.Fatal("bootstrap carried no signed policy; the catalog cannot be reached at all")
	}
	if data.Signature.KeyID != clienttest.FixtureSigningKeyID {
		t.Errorf("keyId = %q, want %q", data.Signature.KeyID, clienttest.FixtureSigningKeyID)
	}

	policy, err := data.PolicyEnvelope.Verify(productclient.VerifyOptions{
		TrustedKeys: map[string]string{clienttest.FixtureSigningKeyID: clienttest.FixtureSigningPublicKey()},
		Audience:    clienttest.FixturePolicyAudience,
		Now:         time.Now(),
		Skew:        time.Minute,
	})
	if err != nil {
		t.Fatalf("the bootstrap envelope does not verify: %v", err)
	}
	if policy.Catalog.Version != clienttest.FixturePolicyVersion {
		t.Errorf("catalog version = %q, want %q", policy.Catalog.Version, clienttest.FixturePolicyVersion)
	}
	if len(policy.Catalog.Models) == 0 {
		t.Error("the verified catalog offers no model; the picker would be empty")
	}
}

// TestBootstrapWithoutAPolicyIsNotAFailedBootstrap is the shape of the
// "no catalog in this build" degradation (需求基线 B4): the account is signed in
// and the envelope is simply absent. Returning an error here would turn a
// catalog problem into a login problem.
//
// It also pins how absence is spelled: the zero envelope with the `null` the
// encoder writes for a nil json.RawMessage - which is why absence is asked with
// IsEmpty() rather than with a length check, and why "no policy" is one
// question rather than three.
func TestBootstrapWithoutAPolicyIsNotAFailedBootstrap(t *testing.T) {
	h := newHarness(t)
	if _, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	h.standin.OmitPolicy()

	data, err := h.client.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap without a policy: %v", err)
	}
	if !data.IsEmpty() {
		t.Errorf("envelope = %s, want none", data.Policy)
	}
	if data.Account.PhoneMasked == "" {
		t.Error("the account half was lost along with the missing policy")
	}
}

// TestBootstrapRejectsAPolicyForAnotherAudience is the fail-closed half of
// bounded degradation (开发规范 §3.9): an envelope addressed to another product
// must not be accepted just because it is correctly signed. There is no
// fallback to "trust it anyway" - a wrong audience is a wrong build, and the
// catalog it carries is not necessarily the one this product is allowed to run.
func TestBootstrapRejectsAPolicyForAnotherAudience(t *testing.T) {
	h := newHarness(t)
	if _, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	h.standin.SetPolicyAudience("some-other-product")

	data, err := h.client.Bootstrap(context.Background())
	// Transport is unaffected: the envelope is delivered, it just does not
	// verify. Keeping those two apart is what lets PR-4c word them differently.
	if err != nil {
		t.Fatalf("Bootstrap: %v - a policy that will not verify is not a transport failure", err)
	}
	_, err = data.PolicyEnvelope.Verify(productclient.VerifyOptions{
		TrustedKeys: map[string]string{clienttest.FixtureSigningKeyID: clienttest.FixtureSigningPublicKey()},
		Audience:    clienttest.FixturePolicyAudience,
		Now:         time.Now(),
		Skew:        time.Minute,
	})
	if !errors.Is(err, productclient.ErrAudience) {
		t.Fatalf("verify error = %v, want ErrAudience", err)
	}
}
