package productclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// authServer records what the client sent and replies with scripted bodies, so
// the tests can assert on the wire shape rather than on internal state.
type authServer struct {
	t       *testing.T
	paths   []string
	bodies  []map[string]any
	headers []http.Header
	// status/body index by request ordinal; the last entry repeats.
	script []scriptedResponse
}

type scriptedResponse struct {
	status int
	body   string
}

func newAuthServer(t *testing.T, script ...scriptedResponse) (*authServer, *httptest.Server) {
	t.Helper()
	s := &authServer{t: t, script: script}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.paths = append(s.paths, r.URL.Path)
		s.headers = append(s.headers, r.Header.Clone())
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		s.bodies = append(s.bodies, body)

		idx := len(s.paths) - 1
		if idx >= len(s.script) {
			idx = len(s.script) - 1
		}
		resp := s.script[idx]
		if resp.status == 0 {
			resp.status = http.StatusOK
		}
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	return s, srv
}

func (s *authServer) lastPath() string { return s.paths[len(s.paths)-1] }
func (s *authServer) lastBody() map[string]any {
	if len(s.bodies) == 0 {
		return nil
	}
	return s.bodies[len(s.bodies)-1]
}
func (s *authServer) lastHeader(key string) string { return s.headers[len(s.headers)-1].Get(key) }

func TestSendCodePostsPhoneAndCorrelation(t *testing.T) {
	srvScript := scriptedResponse{status: 200, body: `{"data":{"cooldownSec":60,"expiresInSec":600},"requestId":"r1"}`}
	srv, ts := newAuthServer(t, srvScript)
	defer ts.Close()

	tr := newTestTransport(t, ts.URL, nil, nil)
	cd, err := NewAuthClient(tr).SendCode(context.Background(), "13800001234")
	if err != nil {
		t.Fatalf("SendCode: %v", err)
	}
	if cd.CooldownSec != 60 || cd.ExpiresInSec != 600 {
		t.Fatalf("cooldown = %+v", cd)
	}
	if got := srv.lastPath(); got != "/auth/sms/send" {
		t.Fatalf("path = %q, want /auth/sms/send", got)
	}
	body := srv.lastBody()
	if body["phone"] != "13800001234" {
		t.Errorf("phone = %v", body["phone"])
	}
	if body["purpose"] != "login" {
		t.Errorf("purpose = %v, want login", body["purpose"])
	}
	id, _ := body["clientRequestId"].(string)
	if id == "" {
		t.Fatal("clientRequestId is empty; the platform expects one on every side-effecting call")
	}
	// The body field and the idempotency header must be the same value: two ids
	// for one call would make the ledger unattributable.
	if got := srv.lastHeader("Idempotency-Key"); got != id {
		t.Fatalf("Idempotency-Key = %q, body clientRequestId = %q; must match", got, id)
	}
	if got := srv.lastHeader("Authorization"); got != "" {
		t.Errorf("Authorization = %q on SendCode, want none (there is no session yet)", got)
	}
}

// TestSendCodeGeneratesADistinctIDPerCall: the frozen signature takes no id, so
// the client generates one — but reusing a single generated id for the process
// lifetime would make every login attempt look like a retry of the first and
// suppress the second SMS the user actually asked for.
func TestSendCodeGeneratesADistinctIDPerCall(t *testing.T) {
	srv, ts := newAuthServer(t, scriptedResponse{status: 200, body: `{"data":{"cooldownSec":1,"expiresInSec":1}}`})
	defer ts.Close()

	client := NewAuthClient(newTestTransport(t, ts.URL, nil, nil))
	for i := 0; i < 2; i++ {
		if _, err := client.SendCode(context.Background(), "13800000000"); err != nil {
			t.Fatalf("SendCode %d: %v", i, err)
		}
	}
	first, _ := srv.bodies[0]["clientRequestId"].(string)
	second, _ := srv.bodies[1]["clientRequestId"].(string)
	if first == "" || second == "" || first == second {
		t.Fatalf("ids = %q, %q; want two distinct values", first, second)
	}
}

func TestLoginFillsTheInstallIDFromTheTransport(t *testing.T) {
	login := `{"data":{"accessToken":"a","refreshToken":"r","accessTokenExpiresInSec":7200,
	  "account":{"id":"acct_1","phoneMasked":"138****1234","nickname":"n"},
	  "activation":{"status":"active","activatedAt":"2026-09-10T08:00:00Z","expiresAt":"2027-09-10T08:00:00Z"}}}`
	srv, ts := newAuthServer(t, scriptedResponse{status: 200, body: login})
	defer ts.Close()

	tr := newTestTransport(t, ts.URL, nil, nil)
	res, err := NewAuthClient(tr).Login(context.Background(), LoginRequest{
		Phone: "13800001234", Code: "123456", ActivationCode: "BUDING-XXXX-XXXX",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if res.AccessToken != "a" || res.RefreshToken != "r" || res.AccessTokenExpiresSec != 7200 {
		t.Fatalf("session = %+v", res.Session)
	}
	if res.Account.ID != "acct_1" || res.Activation.Status != "active" {
		t.Fatalf("login result = %+v", res)
	}
	body := srv.lastBody()
	if body["installId"] != "install-abc" {
		t.Fatalf("installId = %v, want the transport's identifier (the same one in X-Install-Id)", body["installId"])
	}
	if got := srv.lastHeader("X-Install-Id"); got != body["installId"] {
		t.Fatalf("X-Install-Id = %q but body installId = %v; they must not disagree", got, body["installId"])
	}
	if body["code"] != "123456" || body["activationCode"] != "BUDING-XXXX-XXXX" {
		t.Fatalf("body = %v", body)
	}
	if _, set := body["clientRequestId"]; !set {
		t.Fatal("clientRequestId missing from the login body")
	}
}

// TestLoginKeepsACallerSuppliedIdentity: the fill-in is a convenience, not an
// override. A caller that already chose an id or an install id keeps it.
func TestLoginKeepsACallerSuppliedIdentity(t *testing.T) {
	srv, ts := newAuthServer(t, scriptedResponse{status: 200, body: `{"data":{"accessToken":"a","refreshToken":"r","accessTokenExpiresInSec":1}}`})
	defer ts.Close()

	client := NewAuthClient(newTestTransport(t, ts.URL, nil, nil))
	_, err := client.Login(context.Background(), LoginRequest{
		Phone: "1", Code: "2", InstallID: "caller-install", ClientRequestID: "caller-crq",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	body := srv.lastBody()
	if body["installId"] != "caller-install" {
		t.Errorf("installId = %v, want the caller's", body["installId"])
	}
	if body["clientRequestId"] != "caller-crq" {
		t.Errorf("clientRequestId = %v, want the caller's", body["clientRequestId"])
	}
	if got := srv.lastHeader("Idempotency-Key"); got != "caller-crq" {
		t.Errorf("Idempotency-Key = %q, want the caller's id", got)
	}
}

func TestRefreshSendsTheTokenAndReturnsTheRotatedPair(t *testing.T) {
	srv, ts := newAuthServer(t, scriptedResponse{status: 200, body: `{"data":{"accessToken":"new-a","refreshToken":"new-r","accessTokenExpiresInSec":7200}}`})
	defer ts.Close()

	sess, err := NewAuthClient(newTestTransport(t, ts.URL, nil, nil)).Refresh(context.Background(), "old-r")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if sess.AccessToken != "new-a" || sess.RefreshToken != "new-r" {
		t.Fatalf("session = %+v; the rotated refresh token is what the caller must persist", sess)
	}
	if got := srv.lastPath(); got != "/auth/refresh" {
		t.Fatalf("path = %q", got)
	}
	if srv.lastBody()["refreshToken"] != "old-r" {
		t.Fatalf("body = %v", srv.lastBody())
	}
}

// TestAuthWritesAreNotRetried pins the rule at the client level: a 5xx on any of
// these four must reach the caller once, because a second SendCode is a second
// SMS and a second Refresh spends a rotation slot.
func TestAuthWritesAreNotRetried(t *testing.T) {
	srv, ts := newAuthServer(t, scriptedResponse{status: 503, body: `{"code":"upstream_unavailable"}`})
	defer ts.Close()

	client := NewAuthClient(newTestTransport(t, ts.URL, nil, noSleep))
	ctx := context.Background()

	if _, err := client.SendCode(ctx, "1"); CodeOf(err) != CodeUpstreamDown {
		t.Errorf("SendCode err = %v", err)
	}
	if _, err := client.Login(ctx, LoginRequest{Phone: "1", Code: "2"}); CodeOf(err) != CodeUpstreamDown {
		t.Errorf("Login err = %v", err)
	}
	if _, err := client.Refresh(ctx, "r"); CodeOf(err) != CodeUpstreamDown {
		t.Errorf("Refresh err = %v", err)
	}
	if err := client.Logout(ctx); CodeOf(err) != CodeUpstreamDown {
		t.Errorf("Logout err = %v", err)
	}

	if len(srv.paths) != 4 {
		t.Fatalf("server saw %d requests, want 4 — one per call, no retries (paths: %v)", len(srv.paths), srv.paths)
	}
}

func TestLogoutPostsToTheSessionEndpoint(t *testing.T) {
	srv, ts := newAuthServer(t, scriptedResponse{status: 200, body: `{"data":{},"requestId":"r1"}`})
	defer ts.Close()

	tokens := &stubTokens{token: "tok"}
	err := NewAuthClient(newTestTransport(t, ts.URL, tokens, nil)).Logout(context.Background())
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if got := srv.lastPath(); got != "/auth/logout" {
		t.Fatalf("path = %q, want /auth/logout", got)
	}
	if got := srv.lastHeader("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization = %q; the platform identifies the session from the access token", got)
	}
}

// TestLoginSurfacesPhoneMismatchAsItsOwnCode: the interceptor page routes
// `phone_mismatch` to an operator, not to a retry, so the code must survive
// mapping intact and must not be merged into a generic auth failure.
func TestLoginSurfacesPhoneMismatchAsItsOwnCode(t *testing.T) {
	_, ts := newAuthServer(t, scriptedResponse{
		status: 403,
		body:   `{"code":"phone_mismatch","message":"contact support","requestId":"rq"}`,
	})
	defer ts.Close()

	_, err := NewAuthClient(newTestTransport(t, ts.URL, nil, nil)).Login(context.Background(), LoginRequest{Phone: "1", Code: "2"})

	if CodeOf(err) != CodePhoneMismatch {
		t.Fatalf("code = %q, want %q", CodeOf(err), CodePhoneMismatch)
	}
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("err = %T, want *Error", err)
	}
	if pe.Op != "Login" {
		t.Errorf("Op = %q, want Login", pe.Op)
	}
	if pe.Retryable() {
		t.Error("phone_mismatch was treated as retryable; it needs an operator, not a retry")
	}
}

func TestNewClientRequestIDIsUniqueAndOpaque(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := NewClientRequestID()
		if len(id) != 36 {
			t.Fatalf("id = %q (len %d), want a 36-character UUID shape", id, len(id))
		}
		if seen[id] {
			t.Fatalf("duplicate id %q after %d draws", id, i)
		}
		seen[id] = true
		if strings.Count(id, "-") != 4 {
			t.Fatalf("id = %q, want 4 hyphens", id)
		}
	}
}
