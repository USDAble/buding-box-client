package productruntime_test

import (
	"net/http"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// V-44: the platform owns "is this installation activated"; the local file only
// holds the last answer it gave.
//
// The shape under test is a signed-out installation that still believes it is
// activated, so the wall shows the two-field form and sends no activation
// credential (BlockedView.svelte:34/144-145/276). When the platform answers that
// this number has no usable activation, the two sides disagree - and the local
// copy has to follow, because otherwise the interface asks the user to fix a
// field it is not rendering, and the only way back to the five-field form is to
// edit data/product-state.json from outside the product.
//
// Reachable in production, not only against the stand-in: an account removed
// server-side, a re-bound number, a platform migration, or the copy-data/-to-
// another-machine path that E1 rule 7 explicitly blesses.
func TestARefusedSecondLoginSwitchesTheWallBackToTheActivationForm(t *testing.T) {
	h := newHarness(t)
	if state := h.activate(); state["activated"] != true {
		t.Fatalf("precondition: after activation state.activated = %v, want true", state["activated"])
	}
	if status, _ := h.do(http.MethodPost, "/api/product/logout", nil); status != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", status)
	}

	// A number the platform has no record of, signing in without an activation
	// credential - the only shape the two-field form can produce.
	if status, _ := h.sendCode("13800002222"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}
	status, body := h.login(map[string]any{
		"phone": "13800002222", "code": clienttest.FixtureSMSCode, "nickname": "second",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d body = %v, want 400", status, body)
	}
	// The refusal itself is contractual and must not change: what changes is the
	// form the user is looking at when they read it.
	if got := codeOf(t, body); got != productclient.CodeActivationInvalid {
		t.Fatalf("code = %q, want %q", got, productclient.CodeActivationInvalid)
	}

	_, after := h.do(http.MethodGet, "/api/product/state", nil)
	if got, _ := after["activated"].(bool); got {
		t.Errorf("state.activated = true after the platform refused the sign-in: the wall stays on the " +
			"two-field form and answers with a field it does not render (V-44)")
	}
	// What the platform last said is kept (E7 keeps records across a session
	// ending): the box code is what the licence page shows, and the masked
	// number is what the sign-in form compares against.
	account, ok := after["account"].(map[string]any)
	if !ok {
		t.Fatalf("state has no account object: %v", after)
	}
	if got := account["phoneMasked"]; got != "138****1234" {
		t.Errorf("account.phoneMasked = %v, want %q (the refusal is not an un-binding)", got, "138****1234")
	}
	if _, ok := after["activation"].(map[string]any); !ok {
		t.Errorf("the activation record was dropped: %v", after)
	}
}

// The claim must be reclaimable: switching the wall back is only a recovery if
// the form it switches to can actually complete (需求基线 E1's one-to-many rule
// also gets exercised here - a second code for the same box).
func TestTheWithdrawnClaimCanBeReclaimedWithAFreshCode(t *testing.T) {
	h := newHarness(t)
	h.activate()
	if status, _ := h.do(http.MethodPost, "/api/product/logout", nil); status != http.StatusOK {
		t.Fatalf("logout failed")
	}
	if status, _ := h.sendCode("13800002222"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}
	if status, body := h.login(map[string]any{
		"phone": "13800002222", "code": clienttest.FixtureSMSCode, "nickname": "second",
	}); status != http.StatusBadRequest {
		t.Fatalf("the sign-in that withdraws the claim = %d %v, want 400", status, body)
	}

	status, body := h.login(map[string]any{
		"phone": "13800002222", "code": clienttest.FixtureSMSCode, "nickname": "second",
		"activationCode": clienttest.FixtureSecondActivationCode, "boxCode": clienttest.FixtureBoxCode,
	})
	if status != http.StatusOK {
		t.Fatalf("re-activation status = %d body = %v, want 200", status, body)
	}
	state, ok := body["state"].(map[string]any)
	if !ok {
		t.Fatalf("re-activation body has no state: %v", body)
	}
	if state["activated"] != true {
		t.Errorf("state.activated = %v after a successful activation, want true", state["activated"])
	}
}

// Scope (开发规范 §3.10): only the platform denying the claim withdraws it. A
// transport failure is not an answer about the session, let alone about the
// authorization, and un-activating a paying user because the network blinked
// would be the worst possible reading of a dropped connection.
func TestATransportFailureNeverWithdrawsTheClaim(t *testing.T) {
	h := newHarness(t)
	h.activate()
	if status, _ := h.do(http.MethodPost, "/api/product/logout", nil); status != http.StatusOK {
		t.Fatalf("logout failed")
	}
	h.platformSrv.Close()

	status, body := h.login(map[string]any{
		"phone": "13800002222", "code": clienttest.FixtureSMSCode, "nickname": "second",
	})
	if status == http.StatusOK {
		t.Fatalf("login against a closed platform = 200 %v", body)
	}
	if got := codeOf(t, body); got == productclient.CodeActivationInvalid {
		t.Errorf("code = %q: a transport failure was reported as an activation refusal", got)
	}

	if got := h.readStateFile()["activated"]; got != true {
		t.Errorf("state file activated = %v after a transport failure, want true (V-44's scope)", got)
	}
}

// The other failures on this path are not about the activation either: a wrong
// SMS code is a typo, and it must not un-activate anything.
func TestAWrongSMSCodeNeverWithdrawsTheClaim(t *testing.T) {
	h := newHarness(t)
	h.activate()
	if status, _ := h.do(http.MethodPost, "/api/product/logout", nil); status != http.StatusOK {
		t.Fatalf("logout failed")
	}
	if status, _ := h.sendCode("13800002222"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}
	status, body := h.login(map[string]any{
		"phone": "13800002222", "code": "000000", "nickname": "second",
	})
	if status == http.StatusOK {
		t.Fatalf("login with a wrong SMS code = 200 %v", body)
	}
	if got := fieldErrorsOf(t, body)["code"]; got != productclient.CodeInvalidCode {
		t.Fatalf("field error for code = %v, want %q", got, productclient.CodeInvalidCode)
	}
	if got := h.readStateFile()["activated"]; got != true {
		t.Errorf("state file activated = %v after a wrong SMS code, want true", got)
	}
}

// Both halves of the condition matter. An attempt that DID offer an activation
// credential is the user saying "activate this", not "sign me in as an activated
// installation" - and the platform refusing that code says nothing about whether
// this installation already holds a valid activation.
func TestAnAttemptThatCarriedAnActivationCredentialLeavesTheClaimAlone(t *testing.T) {
	h := newHarness(t)
	h.activate()
	if status, _ := h.do(http.MethodPost, "/api/product/logout", nil); status != http.StatusOK {
		t.Fatalf("logout failed")
	}
	if status, _ := h.sendCode("13800001234"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}
	status, body := h.login(map[string]any{
		"phone": "13800001234", "code": clienttest.FixtureSMSCode, "nickname": "tester",
		"activationCode": "BUDING-NOPE-0000", "boxCode": clienttest.FixtureBoxCode,
	})
	if status == http.StatusOK {
		t.Fatalf("login with an unknown activation code = 200 %v", body)
	}
	if got := codeOf(t, body); got != productclient.CodeActivationInvalid {
		t.Fatalf("code = %q, want %q", got, productclient.CodeActivationInvalid)
	}
	if got := h.readStateFile()["activated"]; got != true {
		t.Errorf("state file activated = %v after refusing a code the user offered, want true", got)
	}
}

// A refused sign-in is not a session and not an authorization: it must not
// invent a credential, flip the login flag, or re-issue the install id.
func TestTheRefusalInventedNoSession(t *testing.T) {
	h := newHarness(t)
	h.activate()
	if status, _ := h.do(http.MethodPost, "/api/product/logout", nil); status != http.StatusOK {
		t.Fatalf("logout failed")
	}
	before := h.readStateFile()
	if status, _ := h.sendCode("13800002222"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}
	if status, body := h.login(map[string]any{
		"phone": "13800002222", "code": clienttest.FixtureSMSCode, "nickname": "second",
	}); status != http.StatusBadRequest {
		t.Fatalf("the refused sign-in = %d %v, want 400", status, body)
	}

	after := h.readStateFile()
	if after["loggedIn"] != false {
		t.Errorf("state file loggedIn = %v, want false", after["loggedIn"])
	}
	if after["installId"] != before["installId"] {
		t.Errorf("installId changed on a refused sign-in: %v -> %v", before["installId"], after["installId"])
	}
	if h.credentialFileExists() {
		t.Error("a refused sign-in wrote a credential; a refusal is not a session (C12)")
	}
}
