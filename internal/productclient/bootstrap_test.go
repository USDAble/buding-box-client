package productclient_test

import (
	"context"
	"encoding/json"
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

// TestBootstrapDecodesTheCreditsFieldByItsContractName pins the json tag that
// 需求基线 V-57 caught sitting one word away from the contract.
//
// The field read `json:"balance"` for as long as nothing consumed it, while
// 中台交付包 §4.3 names it `credits` - so the platform's number decoded into
// nothing, and that is indistinguishable from a platform that sent nothing.
// This is the nail whose absence let the tag drift.
//
// WHY IT DECODES A LITERAL RATHER THAN ASKING THE STAND-IN. The stand-in builds
// its answer by marshalling productclient.BootstrapData itself
// (clienttest/server.go's bootstrapResponse), so a mutated tag is written out
// and read back with the same wrong name and the round trip passes. Measured,
// not assumed: flipping this tag back to `balance` leaves the stand-in-driven
// nails green, which is the whole reason the defect lived. A wire shape can only
// be nailed against bytes that did not come from the type under test, so the
// body below is the contract spelled out by hand.
//
// The pointer is what is asserted, because "no balance in this answer" and "the
// balance is zero" are different facts in this product (需求基线 E9 rule 2): a
// tag that stopped matching leaves both nil, and only the pointer can notice.
// A field this build does not consume still has to decode correctly - the wrong
// tag is exactly what would turn it back into a balance source for the next
// person to come along.
func TestBootstrapDecodesTheCreditsFieldByItsContractName(t *testing.T) {
	// 中台交付包 §4.3: the account summary, an activation record and the credits
	// object, all inside `data`. Nothing here mentions the client's field names.
	body := []byte(`{
		"requestId": "req_fixture",
		"data": {
			"account": {"id": "acct_1", "phoneMasked": "138****1234", "nickname": "tester"},
			"credits": {"balanceMicroCredits": 2478200}
		}
	}`)

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var data productclient.BootstrapData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("unmarshal bootstrap data: %v", err)
	}

	if data.Account.PhoneMasked == "" {
		t.Error("the fixture body no longer decodes as a bootstrap answer at all")
	}
	if data.Credits.BalanceMicroCredits == nil {
		t.Fatal("the credits field decoded to nothing: the contract calls it `credits` (中台交付包 §4.3), not `balance`")
	}
	if got := *data.Credits.BalanceMicroCredits; got != 2_478_200 {
		t.Errorf("balance = %d, want 2478200", got)
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
