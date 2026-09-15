package clienttest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
)

// The four Fail* switches share one gate rule: armed when the STATUS is
// non-zero, with the business code as optional metadata. They used to disagree —
// bootstrap, logout and ledger tested the code; completions tested the status —
// so FailCompletions(502, "") produced a 502 while FailBootstrap(502, "") left
// the fixture healthy and said nothing. The rule and its rationale are on
// FailBootstrap; these tests are what stops the four from drifting apart again,
// because nothing else would notice: a switch that is silently off is
// indistinguishable from a switch nobody armed.

// TestABareStatusArmsEveryFailureSwitch is the rule itself. One case per family
// member, all with an empty code, so a member that goes back to testing the code
// fails here rather than in whichever walkthrough used it next.
func TestABareStatusArmsEveryFailureSwitch(t *testing.T) {
	const phone = "13800001234"

	cases := []struct {
		name    string
		arm     func(*Server)
		request func(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder
		want    int
	}{
		{
			name: "bootstrap",
			arm:  func(s *Server) { s.FailBootstrap(http.StatusBadGateway, "") },
			request: func(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder {
				return get(t, h, "/v1/client/bootstrap", token)
			},
			want: http.StatusBadGateway,
		},
		{
			name: "logout",
			arm:  func(s *Server) { s.FailLogout(http.StatusServiceUnavailable, "") },
			request: func(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder {
				return post(t, h, "/v1/auth/logout", `{}`, token)
			},
			want: http.StatusServiceUnavailable,
		},
		{
			name: "ledger",
			arm:  func(s *Server) { s.FailLedger(http.StatusBadGateway, "") },
			request: func(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder {
				return get(t, h, "/v1/credits/ledger", token)
			},
			want: http.StatusBadGateway,
		},
		{
			name: "completions",
			arm:  func(s *Server) { s.FailCompletions(http.StatusPaymentRequired, "") },
			request: func(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder {
				return post(t, h, "/v1/chat/completions",
					`{"model":"buding-cloud-pro","messages":[{"role":"user","content":"hi"}],"stream":false}`, token)
			},
			want: http.StatusPaymentRequired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A healthy run first, so the assertion below cannot be satisfied by a
			// route that fails for its own reasons.
			healthy := New()
			h := healthy.Handler()
			unarmed := tc.request(t, h, signInFor(t, h, phone))
			if unarmed.Code != http.StatusOK {
				t.Fatalf("%s answers %d with nothing armed; the test is pointed at the wrong route or the wrong token",
					tc.name, unarmed.Code)
			}

			armed := New()
			tc.arm(armed)
			h = armed.Handler()
			got := tc.request(t, h, signInFor(t, h, phone))
			if got.Code != tc.want {
				t.Errorf("%s answers %d with a bare status armed, want %d: %s",
					tc.name, got.Code, tc.want, got.Body.String())
			}
		})
	}
}

// TestAnEmptyStatusIsStillNotAnInjection. The gate moved from the code to the
// status, so "off" had to move with it. Every setter's zero value is the healthy
// fixture, and a reset call has to keep meaning reset — credits_test.go resets
// the ledger that way mid-test.
func TestAnEmptyStatusIsStillNotAnInjection(t *testing.T) {
	stub := New()
	stub.FailBootstrap(http.StatusBadGateway, productclient.CodeUpstreamUnavailable)
	stub.FailBootstrap(0, "") // the reset
	stub.FailLedger(http.StatusBadGateway, productclient.CodeUpstreamUnavailable)
	stub.FailLedger(0, "")

	h := stub.Handler()
	token := signInFor(t, h, "13800001234")
	if got := get(t, h, "/v1/client/bootstrap", token); got.Code != http.StatusOK {
		t.Errorf("bootstrap answers %d after a reset, want 200: %s", got.Code, got.Body.String())
	}
	if got := get(t, h, "/v1/credits/ledger", token); got.Code != http.StatusOK {
		t.Errorf("ledger answers %d after a reset, want 200: %s", got.Code, got.Body.String())
	}
}

// TestACodeLessRefusalCarriesNoBusinessCode. What keying on the status buys is
// the case the client already models and the fixture could not produce: a proxy's
// 502, which has no business code in it (需求基线 B4's catalogTransport). The
// envelope must then be empty of a code rather than carrying an invented one —
// the status↔code registry has exactly one owner
// (internal/productclient/testdata/wire-error-codes.txt) and a fixture that made
// one up would be a second.
func TestACodeLessRefusalCarriesNoBusinessCode(t *testing.T) {
	stub := New()
	stub.FailLedger(http.StatusBadGateway, "")
	h := stub.Handler()

	got := get(t, h, "/v1/credits/ledger", signInFor(t, h, "13800001234"))
	if got.Code != http.StatusBadGateway {
		t.Fatalf("ledger answers %d, want 502", got.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the refusal: %v", err)
	}
	if code, ok := body["code"]; ok && code != "" {
		t.Errorf("a code-less refusal invented code %q", code)
	}
}

func get(t *testing.T, h http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func post(t *testing.T, h http.Handler, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// signInFor runs the fixture's two-step sign-in through the handler and returns
// the access token. The phone is a parameter because the fixtures bind some
// activation codes to specific numbers.
func signInFor(t *testing.T, h http.Handler, phone string) string {
	t.Helper()
	send := post(t, h, "/v1/auth/sms/send",
		`{"phone":"`+phone+`","purpose":"login","clientRequestId":"req-gate-1"}`, "")
	if send.Code != http.StatusOK {
		t.Fatalf("sms/send = %d, want 200: %s", send.Code, send.Body.String())
	}
	login := post(t, h, "/v1/auth/login",
		`{"phone":"`+phone+`","code":"`+FixtureSMSCode+`","activationCode":"`+FixtureActivationCode+
			`","boxCode":"`+FixtureBoxCode+`","installId":"33333333-3333-4333-8333-333333333333"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200: %s", login.Code, login.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(login.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	data, _ := body["data"].(map[string]any)
	token, _ := data["accessToken"].(string)
	if token == "" {
		t.Fatalf("login returned no access token: %v", body)
	}
	return token
}
