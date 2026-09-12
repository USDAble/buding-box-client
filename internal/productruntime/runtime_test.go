package productruntime_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
	"github.com/open-octo/octo-agent/internal/productruntime"
	"github.com/open-octo/octo-agent/internal/productstate"
)

// harness wires the local runtime to the platform stand-in, which is the same
// topology as a running client: web UI -> local service -> platform.
type harness struct {
	t        *testing.T
	rt       *productruntime.Runtime
	local    *httptest.Server
	platform *clienttest.Server
	root     string
}

func newHarness(t *testing.T) *harness {
	return newHarnessWithControlPlane(t, productruntime.ControlPlaneStatus{Configured: true, HasTrustedKeys: true})
}

// newHarnessWithControlPlane is newHarness with the two compile-time profile
// facts overridden. The default matches the stand-in platform this harness
// serves (a build that CAN reach a control plane), and the override exists so
// the four blocked-page outcomes can be driven from a test at all - the embedded
// profile is a compile-time constant, so there is no other way to reach the
// "unconfigured" and "no keys" cases (本地API契约 §2.13).
func newHarnessWithControlPlane(t *testing.T, status productruntime.ControlPlaneStatus) *harness {
	t.Helper()
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)

	platform := clienttest.New()
	platformSrv := httptest.NewServer(platform.Handler())
	t.Cleanup(platformSrv.Close)

	state, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("productstate.Open: %v", err)
	}
	creds, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("credentialstore.Open: %v", err)
	}
	// Versioned base URL, matching the only shape a control-plane host has
	// (production.json: https://api.invalid/v1). The client's paths are relative
	// to the version segment, so an unversioned base would silently test a
	// composition that never happens at run time.
	client := productclient.New(platformSrv.URL+"/v1", productclient.ClientMeta{
		Version:   "test",
		Platform:  "test",
		Arch:      "test",
		InstallID: state.InstallID(),
	}, &productclient.CredentialHolder{})

	rt := productruntime.New(productruntime.Deps{
		State:        state,
		Creds:        creds,
		Platform:     client,
		ControlPlane: status,
	})
	local := httptest.NewServer(rt.Handler())
	t.Cleanup(local.Close)

	return &harness{t: t, rt: rt, local: local, platform: platform, root: root}
}

// do issues a request against the local service and returns status plus the
// decoded body. The body is a map so the tests can assert on the exact envelope
// shape - which is what half of these tests are about.
func (h *harness) do(method, path string, body any) (int, map[string]any) {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatalf("encode request: %v", err)
		}
	}
	req, err := http.NewRequest(method, h.local.URL+path, &buf)
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.local.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		h.t.Fatalf("decode response of %s %s: %v", method, path, err)
	}
	return resp.StatusCode, decoded
}

func (h *harness) sendCode(phone string) (int, map[string]any) {
	h.t.Helper()
	return h.do(http.MethodPost, "/api/product/send-code", map[string]any{"phone": phone})
}

func (h *harness) login(payload map[string]any) (int, map[string]any) {
	h.t.Helper()
	return h.do(http.MethodPost, "/api/product/login", payload)
}

// activate runs the full five-field first activation and fails the test if it
// did not succeed, returning the resulting state object.
func (h *harness) activate() map[string]any {
	h.t.Helper()
	if status, _ := h.sendCode("13800001234"); status != http.StatusOK {
		h.t.Fatalf("send-code status = %d, want 200", status)
	}
	status, body := h.login(map[string]any{
		"phone":          "13800001234",
		"code":           clienttest.FixtureSMSCode,
		"nickname":       "tester",
		"activationCode": clienttest.FixtureActivationCode,
		"boxCode":        clienttest.FixtureBoxCode,
	})
	if status != http.StatusOK {
		h.t.Fatalf("login status = %d body = %v, want 200", status, body)
	}
	state, ok := body["state"].(map[string]any)
	if !ok {
		h.t.Fatalf("login body has no state object: %v", body)
	}
	return state
}

func (h *harness) readStateFile() map[string]any {
	h.t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.root, "product-state.json"))
	if err != nil {
		h.t.Fatalf("read state file: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		h.t.Fatalf("unmarshal state file: %v", err)
	}
	return m
}

func (h *harness) credentialFileExists() bool {
	_, err := os.Stat(filepath.Join(h.root, "credential.json"))
	return err == nil
}

// codeOf pulls the business-level code out of a body, failing if absent.
func codeOf(t *testing.T, body map[string]any) string {
	t.Helper()
	code, ok := body["code"].(string)
	if !ok {
		t.Fatalf("body has no business-level code: %v", body)
	}
	return code
}

// fieldErrorsOf pulls the field-level envelope out of a body, failing if absent.
func fieldErrorsOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	fe, ok := body["fieldErrors"].(map[string]any)
	if !ok {
		t.Fatalf("body has no fieldErrors envelope: %v", body)
	}
	return fe
}

// L-E1 / E8: the state endpoint is the first call the UI makes, and it returns
// the state object directly rather than wrapped in "state" (本地API契约 §2.1).
func TestStateIsReturnedUnwrapped(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(http.MethodGet, "/api/product/state", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, wrapped := body["state"]; wrapped {
		t.Error("state endpoint wrapped its payload in \"state\"; the contract says unwrapped")
	}
	if got, ok := body["loggedIn"]; !ok || got != false {
		t.Errorf("loggedIn = %v, want false on a fresh install", body["loggedIn"])
	}
	if _, ok := body["schemaVersion"]; !ok {
		t.Error("state is missing schemaVersion")
	}
}

// A fresh install must be able to read its own state before logging in, which is
// how the UI decides to show the login screen (本地API契约 §1.4).
func TestStateIsReachableWhileLoggedOut(t *testing.T) {
	h := newHarness(t)

	status, _ := h.do(http.MethodGet, "/api/product/state", nil)
	if status == http.StatusForbidden {
		t.Error("state endpoint was gated; the login screen could never render")
	}
}

// 本地API契约 §2.2, the counter-intuitive one: a bad phone number on send-code
// comes back as a BUSINESS-level code, not as fieldErrors.phone. The frontend
// reads body.code and files it under the phone field itself; "tidying" this into
// the field envelope produces no error and the wrong message.
func TestSendCodeInvalidPhoneUsesBusinessEnvelope(t *testing.T) {
	h := newHarness(t)

	status, body := h.sendCode("138")
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if _, isFieldLevel := body["fieldErrors"]; isFieldLevel {
		t.Fatalf("send-code used the field envelope for a phone error: %v", body)
	}
	if got := codeOf(t, body); got != productclient.CodeInvalidPhone {
		t.Errorf("code = %q, want %q", got, productclient.CodeInvalidPhone)
	}
}

// 本地API契约 §2.2: the response carries the cooldown the UI counts down.
func TestSendCodeReturnsCooldown(t *testing.T) {
	h := newHarness(t)

	status, body := h.sendCode("13800001234")
	if status != http.StatusOK {
		t.Fatalf("status = %d body = %v, want 200", status, body)
	}
	if _, ok := body["cooldownSec"]; !ok {
		t.Errorf("response has no cooldownSec: %v", body)
	}
}

// L-A1: first activation succeeds and the box code in the result comes from the
// platform, not from the form. The UI must render the server's copy, because a
// later login has no form at all (需求基线 E6.1).
func TestFirstActivationReturnsWrappedStateWithServerBoxCode(t *testing.T) {
	h := newHarness(t)

	state := h.activate()

	if got := state["loggedIn"]; got != true {
		t.Errorf("loggedIn = %v, want true", got)
	}
	if got := state["activated"]; got != true {
		t.Errorf("activated = %v, want true", got)
	}
	activation, ok := state["activation"].(map[string]any)
	if !ok {
		t.Fatalf("state has no activation object: %v", state)
	}
	if got := activation["boxCode"]; got != clienttest.FixtureBoxCode {
		t.Errorf("boxCode = %v, want %q (from the platform, not the form)", got, clienttest.FixtureBoxCode)
	}
	account, ok := state["account"].(map[string]any)
	if !ok {
		t.Fatalf("state has no account object: %v", state)
	}
	if got := account["phoneMasked"]; got == "13800001234" {
		t.Error("phone number was not masked in the state returned to the UI")
	}
}

// L-A1: activation writes the credential to disk so the session survives a
// restart, and writes no token into the state file (需求基线 E6 / E6.1).
func TestFirstActivationPersistsCredentialButNoTokenInState(t *testing.T) {
	h := newHarness(t)

	h.activate()

	if !h.credentialFileExists() {
		t.Fatal("credential.json was not written; the session would not survive a restart")
	}
	raw, err := os.ReadFile(filepath.Join(h.root, "product-state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	for _, needle := range []string{"refreshToken", "accessToken", "rt_", "at_"} {
		if strings.Contains(string(raw), needle) {
			t.Errorf("state file contains %q; tokens belong only in credential.json", needle)
		}
	}
	// The plaintext phone number must not be persisted either.
	if strings.Contains(string(raw), "13800001234") {
		t.Error("state file contains the plaintext phone number")
	}
}

// 本地API契约 §2.3: an empty activation code or box code is a FIELD-level error,
// distinct from the business-level failures the platform reports.
func TestEmptyActivationCredentialsAreFieldLevel(t *testing.T) {
	h := newHarness(t)
	if status, _ := h.sendCode("13800001234"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}

	t.Run("activationCode", func(t *testing.T) {
		status, body := h.login(map[string]any{
			"phone": "13800001234", "code": clienttest.FixtureSMSCode,
			"nickname": "tester", "activationCode": "", "boxCode": clienttest.FixtureBoxCode,
		})
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d body = %v, want 400", status, body)
		}
		if got := fieldErrorsOf(t, body)["activationCode"]; got != "invalid_activation" {
			t.Errorf("fieldErrors.activationCode = %v, want invalid_activation", got)
		}
	})

	t.Run("boxCode", func(t *testing.T) {
		status, body := h.login(map[string]any{
			"phone": "13800001234", "code": clienttest.FixtureSMSCode,
			"nickname": "tester", "activationCode": clienttest.FixtureSecondActivationCode, "boxCode": "",
		})
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d body = %v, want 400", status, body)
		}
		if got := fieldErrorsOf(t, body)["boxCode"]; got != "invalid_box_code" {
			t.Errorf("fieldErrors.boxCode = %v, want invalid_box_code", got)
		}
	})
}

// L-A4: the activation failures must stay distinguishable. A user who mistypes
// one character has to be able to see which of the two credentials is wrong -
// they are independent checks against the platform (需求基线 E1 规则 2).
func TestActivationFailuresStayDistinguishable(t *testing.T) {
	cases := []struct {
		name       string
		activation string
		boxCode    string
		phone      string
		wantCode   string
		wantMasked bool
	}{
		{
			name:       "activation code not recognised",
			activation: "BUDING-NOPE-0000", boxCode: clienttest.FixtureBoxCode,
			phone: "13800001234", wantCode: productclient.CodeActivationInvalid,
		},
		{
			name:       "box code not recognised",
			activation: clienttest.FixtureActivationCode, boxCode: clienttest.FixtureUnknownBoxCode,
			phone: "13800001234", wantCode: productclient.CodeBoxCodeUnknown,
		},
		{
			name:       "activation code belongs to a different box",
			activation: clienttest.FixtureActivationCode, boxCode: clienttest.FixtureOtherBoxCode,
			phone: "13800001234", wantCode: productclient.CodeBoxCodeMismatch,
		},
		{
			name:       "activation code already bound to another phone",
			activation: clienttest.FixtureBoundPhoneActivationCode, boxCode: clienttest.FixtureBoxCode,
			phone: "13800001234", wantCode: productclient.CodePhoneMismatch, wantMasked: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if status, _ := h.sendCode(tc.phone); status != http.StatusOK {
				t.Fatalf("send-code status = %d, want 200", status)
			}
			status, body := h.login(map[string]any{
				"phone": tc.phone, "code": clienttest.FixtureSMSCode,
				"nickname": "tester", "activationCode": tc.activation, "boxCode": tc.boxCode,
			})
			// The contract lets a business error ride on 400, 403 or 409 (§1.2);
			// what is contractual is the machine code and the envelope shape.
			if status != http.StatusBadRequest && status != http.StatusForbidden && status != http.StatusConflict {
				t.Fatalf("status = %d body = %v, want 400/403/409", status, body)
			}
			if _, isFieldLevel := body["fieldErrors"]; isFieldLevel {
				t.Fatalf("activation failure used the field envelope: %v", body)
			}
			if got := codeOf(t, body); got != tc.wantCode {
				t.Errorf("code = %q, want %q", got, tc.wantCode)
			}
			if tc.wantMasked {
				if _, ok := body["phoneMasked"]; !ok {
					t.Errorf("phone_mismatch must carry phoneMasked so the UI can name the bound number: %v", body)
				}
			}
		})
	}
}

// L-A1 一码一用: a used activation code is reported as used, and that is the one
// failure whose only remedy is customer support (需求基线 E1 规则 2).
func TestUsedActivationCodeIsReportedAsUsed(t *testing.T) {
	h := newHarness(t)
	h.activate() // consumes FixtureActivationCode for this installation

	// The same fixture, so the code really is spent. The point is that a second
	// attempt - after a logout, from another account - is refused.
	if status, _ := h.do(http.MethodPost, "/api/product/logout", nil); status != http.StatusOK {
		t.Fatalf("logout failed")
	}
	if status, _ := h.sendCode("13800002222"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}
	status, body := h.login(map[string]any{
		"phone": "13800002222", "code": clienttest.FixtureSMSCode,
		"nickname": "second", "activationCode": clienttest.FixtureActivationCode, "boxCode": clienttest.FixtureBoxCode,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d body = %v, want 400", status, body)
	}
	if got := codeOf(t, body); got != productclient.CodeActivationCodeUsed {
		t.Errorf("code = %q, want %q", got, productclient.CodeActivationCodeUsed)
	}
}

// L-A2: a later login sends neither credential, and the box code is still
// reported - it comes from the platform's record, not from a form (需求基线 E2).
func TestSecondLoginOmitsCredentialsAndStillReportsBoxCode(t *testing.T) {
	h := newHarness(t)
	h.activate()

	status, body := h.do(http.MethodPost, "/api/product/logout", nil)
	if status != http.StatusOK {
		t.Fatalf("logout status = %d body = %v, want 200", status, body)
	}

	if status, _ := h.sendCode("13800001234"); status != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200", status)
	}
	status, body = h.login(map[string]any{
		"phone": "13800001234", "code": clienttest.FixtureSMSCode, "nickname": "tester",
	})
	if status != http.StatusOK {
		t.Fatalf("second login status = %d body = %v, want 200", status, body)
	}
	state := body["state"].(map[string]any)
	activation, ok := state["activation"].(map[string]any)
	if !ok {
		t.Fatalf("second login returned no activation object: %v", state)
	}
	if got := activation["boxCode"]; got != clienttest.FixtureBoxCode {
		t.Errorf("boxCode = %v, want %q after a login that never sent one", got, clienttest.FixtureBoxCode)
	}
}

// L-A5: a legacy record with no box code is not an error. The field is simply
// absent and the UI shows a dash (需求基线 PQ19).
func TestLoginWithoutBoxCodeOnRecordIsNotAnError(t *testing.T) {
	h := newHarness(t)
	h.activate()

	// The platform's copy of the record has no box code (an older row).
	h.platform.ClearBoxCode("13800001234")

	if status, _ := h.do(http.MethodPost, "/api/product/logout", nil); status != http.StatusOK {
		t.Fatalf("logout failed")
	}
	if status, _ := h.sendCode("13800001234"); status != http.StatusOK {
		t.Fatalf("send-code failed")
	}
	status, body := h.login(map[string]any{
		"phone": "13800001234", "code": clienttest.FixtureSMSCode, "nickname": "tester",
	})
	if status != http.StatusOK {
		t.Fatalf("login status = %d body = %v, want 200 - a missing box code must not block login", status, body)
	}
	state := body["state"].(map[string]any)
	if activation, ok := state["activation"].(map[string]any); ok {
		if got, present := activation["boxCode"]; present && got != "" {
			t.Errorf("boxCode = %v, want absent or empty", got)
		}
	}
}

// E7: logging out deletes the credential and clears the login flag, but the
// activation record and bound number stay - logging out is not un-activating,
// and the second-login form compares against the number (需求基线 E7 / E2).
func TestLogoutDeletesCredentialAndKeepsActivation(t *testing.T) {
	h := newHarness(t)
	h.activate()
	if !h.credentialFileExists() {
		t.Fatal("precondition failed: no credential after activation")
	}

	status, body := h.do(http.MethodPost, "/api/product/logout", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d body = %v, want 200", status, body)
	}
	if got, ok := body["ok"]; !ok || got != true {
		t.Errorf("logout body = %v, want {\"ok\":true}", body)
	}
	if h.credentialFileExists() {
		t.Error("logout left credential.json behind; the session was not ended")
	}

	onDisk := h.readStateFile()
	if got := onDisk["loggedIn"]; got != false {
		t.Errorf("loggedIn on disk = %v, want false", got)
	}
	if got := onDisk["activated"]; got != true {
		t.Errorf("activated on disk = %v, want true - logout must not un-activate", got)
	}
	activation, ok := onDisk["activation"].(map[string]any)
	if !ok {
		t.Fatalf("activation record was dropped by logout: %v", onDisk)
	}
	if got := activation["boxCode"]; got != clienttest.FixtureBoxCode {
		t.Errorf("boxCode = %v, want %q to survive logout", got, clienttest.FixtureBoxCode)
	}
	account, ok := onDisk["account"].(map[string]any)
	if !ok {
		t.Fatalf("bound account was dropped by logout: %v", onDisk)
	}
	if got := account["phoneMasked"]; got == "" {
		t.Error("bound phone number was dropped by logout; the second-login form needs it")
	}
}

// E8 规则 5: the interface language can be set before logging in, because the
// login screen itself has to be readable.
func TestLocaleIsAcceptedWhileLoggedOut(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(http.MethodPut, "/api/product/locale", map[string]any{"locale": "zh"})
	if status != http.StatusOK {
		t.Fatalf("status = %d body = %v, want 200", status, body)
	}
	if got, ok := body["ok"]; !ok || got != true {
		t.Errorf("locale body = %v, want {\"ok\":true}", body)
	}

	_, state := h.do(http.MethodGet, "/api/product/state", nil)
	prefs, ok := state["prefs"].(map[string]any)
	if !ok {
		t.Fatalf("state has no prefs object: %v", state)
	}
	if got := prefs["locale"]; got != "zh" {
		t.Errorf("prefs.locale = %v, want zh", got)
	}
}

// 本地API契约 §2.5: an unsupported locale is a field-level error. The frontend
// does not parse this today, so fixing the shape now costs nothing.
func TestLocaleInvalidValueIsFieldLevel(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(http.MethodPut, "/api/product/locale", map[string]any{"locale": "fr"})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d body = %v, want 400", status, body)
	}
	if got := fieldErrorsOf(t, body)["locale"]; got != "invalid_value" {
		t.Errorf("fieldErrors.locale = %v, want invalid_value", got)
	}
}

// 本地API契约 §1.2: an unreachable platform is a local or upstream failure, not a
// business error, and it must be told apart from a rejected credential.
func TestUnreachablePlatformIsNotABusinessError(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)

	state, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("productstate.Open: %v", err)
	}
	creds, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("credentialstore.Open: %v", err)
	}
	// A server that is already closed, so every call fails to connect.
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	client := productclient.New(dead.URL, productclient.ClientMeta{InstallID: state.InstallID()}, &productclient.CredentialHolder{})

	rt := productruntime.New(productruntime.Deps{State: state, Creds: creds, Platform: client})
	srv := httptest.NewServer(rt.Handler())
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Post(srv.URL+"/api/product/send-code", "application/json",
		strings.NewReader(`{"phone":"13800001234"}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 500 {
		t.Errorf("status = %d, want 5xx for an unreachable platform", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := body["code"]; got != productclient.CodeNetworkUnavailable {
		t.Errorf("code = %v, want %q - an outage must not look like a rejected login", got, productclient.CodeNetworkUnavailable)
	}
}
