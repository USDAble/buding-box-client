package productruntime_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// The platform owns "is this installation activated"; the local file only holds
// the last answer it gave (V-44).
//
// The shape under test is a signed-out installation whose local state still says
// activated. When the platform answers that this number holds no usable
// activation, the two sides disagree - and the local copy has to follow, because
// otherwise the interface keeps offering a sign-in whose answer can never change,
// while the form that could fix it is the one it is not showing.
//
// Reachable in production, not only against the stand-in: an account removed
// server-side, a re-bound number, a platform migration, or the copy-data/-to-
// another-machine path that E1 rule 7 explicitly blesses.
func TestARefusedSignInWithdrawsTheLocalActivationClaim(t *testing.T) {
	h := newHarness(t)
	if state := h.activate(); state["activated"] != true {
		t.Fatalf("precondition: after activation state.activated = %v, want true", state["activated"])
	}
	if status, _ := h.do(http.MethodPost, "/api/product/logout", nil); status != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", status)
	}

	// A number the platform has no record of, signing in without an activation
	// credential - the only shape the short form can produce.
	if status, _ := h.sendCode("13800002222"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}
	status, body := h.login(map[string]any{
		"phone": "13800002222", "code": clienttest.FixtureSMSCode, "nickname": "second",
	})
	// activation_required is the registered name for "this phone holds no usable
	// activation" (中台交付包 §4.2); the stand-in answered activation_invalid for
	// it until V-45, which pointed the user at a field of a form they could not
	// reach.
	if status != http.StatusForbidden {
		t.Fatalf("status = %d body = %v, want 403", status, body)
	}
	if got := codeOf(t, body); got != productclient.CodeActivationRequired {
		t.Fatalf("code = %q, want %q", got, productclient.CodeActivationRequired)
	}
	if _, isFieldLevel := body["fieldErrors"]; isFieldLevel {
		t.Errorf("activation_required is about the account, not one field: %v", body)
	}

	_, after := h.do(http.MethodGet, "/api/product/state", nil)
	if got, _ := after["activated"].(bool); got {
		t.Errorf("state.activated = true after the platform refused the sign-in: the wall keeps showing a " +
			"form whose answer cannot change (V-44)")
	}
	// What the platform last said is kept (E7 keeps records across a session
	// ending): the box code is what the licence page shows, and the masked number
	// is what the sign-in form compares against.
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

// A fresh data root signing into an account the platform already holds (E1 rule
// 2, PQ28).
//
// This is the recovery path that used to be "contact support and nothing else":
// the activation code is one-shot, so a user who lost data/ could not re-activate
// and the client insisted on both credentials before it would even try. The
// platform decides whether an activation is needed, so the client now sends what
// the user filled in and lets it answer - and the activation record it answers
// with is what fills the licence page back in (ApplyLogin takes both the box code
// and the expiry from the platform, never from the form).
func TestAFreshDataRootSignsIntoAnAccountThePlatformAlreadyHas(t *testing.T) {
	h := newHarness(t)

	// The account exists because it was activated elsewhere. Done over the wire
	// rather than through this runtime, so the local state file is genuinely that
	// of a new installation: no activation record, no credential.
	activateOnThePlatform(t, h, "13800001234")

	// The local state must not have learned anything from that.
	if state := h.readStateFile(); state["activated"] != false {
		t.Fatalf("precondition: local activated = %v, want false (a fresh data root)", state["activated"])
	}

	if status, _ := h.sendCode("13800001234"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}
	status, body := h.login(map[string]any{
		"phone": "13800001234", "code": clienttest.FixtureSMSCode, "nickname": "tester",
	})
	if status != http.StatusOK {
		t.Fatalf("signing in without an activation credential = %d body = %v, want 200 (PQ28: a fresh "+
			"installation only needs the phone to reach an account the platform already has)", status, body)
	}
	state, ok := body["state"].(map[string]any)
	if !ok {
		t.Fatalf("body has no state object: %v", body)
	}
	if state["activated"] != true || state["loggedIn"] != true {
		t.Errorf("state = %v, want activated and loggedIn", state)
	}
	activation, ok := state["activation"].(map[string]any)
	if !ok {
		t.Fatalf("state has no activation record: %v", state)
	}
	if got := activation["boxCode"]; got != clienttest.FixtureBoxCode {
		t.Errorf("boxCode = %v, want %q (it must come from the platform answer, not from a form)",
			got, clienttest.FixtureBoxCode)
	}
}

// The relaxation is a SHAPE rule, not an invitation: half a pair is still refused
// locally, so the user is told which of the two is missing without a round trip.
func TestAHalfFilledActivationPairIsStillRefusedLocally(t *testing.T) {
	h := newHarness(t)
	if status, _ := h.sendCode("13800001234"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}
	status, body := h.login(map[string]any{
		"phone": "13800001234", "code": clienttest.FixtureSMSCode, "nickname": "tester",
		"activationCode": clienttest.FixtureActivationCode, // and no box code
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d body = %v, want 400", status, body)
	}
	// A field-level envelope is produced only by the local validator, so its
	// presence is the proof that nothing was sent to the platform.
	if got := fieldErrorsOf(t, body)["boxCode"]; got != "invalid_box_code" {
		t.Errorf("fieldErrors.boxCode = %v, want %q", got, "invalid_box_code")
	}
}

// The claim must be reclaimable: withdrawing it is only a recovery if the form it
// leaves the user on can complete (需求基线 E1's one-to-many rule also gets
// exercised here - a second code for the same box).
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
	}); status != http.StatusForbidden {
		t.Fatalf("the sign-in that withdraws the claim = %d %v, want 403", status, body)
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
	if got := codeOf(t, body); got != productclient.CodeNetworkUnavailable {
		t.Errorf("code = %q, want %q: a transport failure is not an activation refusal",
			got, productclient.CodeNetworkUnavailable)
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
	}); status != http.StatusForbidden {
		t.Fatalf("the refused sign-in = %d %v, want 403", status, body)
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

// activateOnThePlatform creates an activated account straight on the stand-in, as
// if the box had been set up on another machine. Only the JSON round trip is
// duplicated here; the fixture rules stay the stand-in's.
func activateOnThePlatform(t *testing.T, h *harness, phone string) {
	t.Helper()
	post := func(path string, payload map[string]any, want int) {
		t.Helper()
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(payload); err != nil {
			t.Fatalf("encode %s: %v", path, err)
		}
		resp, err := http.Post(h.platformSrv.URL+path, "application/json", buf)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("POST %s = %d, want %d", path, resp.StatusCode, want)
		}
	}
	post("/v1/auth/sms/send", map[string]any{"phone": phone}, http.StatusOK)
	post("/v1/auth/login", map[string]any{
		"phone": phone, "code": clienttest.FixtureSMSCode, "nickname": "tester",
		"activationCode": clienttest.FixtureActivationCode, "boxCode": clienttest.FixtureBoxCode,
	}, http.StatusOK)
}
