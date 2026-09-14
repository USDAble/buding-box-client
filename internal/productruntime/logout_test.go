package productruntime_test

// V-54 / PR-2f: logout must revoke the platform session.
//
// The acceptance is the portable product's own scenario, so the nails below are
// written from the copy's point of view: they take the refresh token OUT OF THE
// CREDENTIAL FILE (which is what a copied data/ carries), log out, and then ask
// the platform to exchange that token. Before this PR every one of those
// exchanges succeeded - logging out was two local acts and the platform was never
// told, although E7 said it must be and 交付包 §4.1 #4 names the endpoint.
//
// WHY THE EXCHANGE IS NOT ATTEMPTED BEFORE THE LOGOUT AS A CONTROL. The refresh
// endpoint rotates the token unconditionally (contract §4.2.3), so a positive
// pre-check would consume the very token the nail then expects to be dead, and
// the 401 would prove nothing. Nothing rotates it in between either: the logout's
// own call is authorised by the access token, which is still valid, so no refresh
// runs. That the 401 is caused by the logout is established by mutation instead -
// drop the revoke and this file goes red.
//
// The failure half is what needed a human decision (PQ29, §3.7): an unreachable
// platform must not trap the user in a signed-in state. Option 1 was chosen - log
// out locally and say so - so these nails assert both the local logout AND that
// the answer carries the fact, because "it fell back and kept working" is not an
// answer (开发规范 §3.9).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// logoutBody is 本地API契约 §1.4 #4's response, spelled out field by field rather
// than shared with the handler's struct: it is a Go/JS boundary shape, and a test
// that reuses the struct cannot catch a rename that breaks the browser.
type logoutBody struct {
	OK      bool `json:"ok"`
	Revoked bool `json:"revoked"`
}

// copiedRefreshToken is the refresh token a copy of data/ would carry. Read from
// the file rather than from the login response, because the file IS the artifact
// under discussion (E1 rule 7) - and the bearer never reaches the frontend.
func (m *mountedHarness) copiedRefreshToken(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(m.root, "credential.json"))
	if err != nil {
		t.Fatalf("read credential.json: %v", err)
	}
	var cred struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.Unmarshal(raw, &cred); err != nil {
		t.Fatalf("decode credential.json: %v", err)
	}
	if cred.RefreshToken == "" {
		t.Fatalf("credential.json holds no refreshToken: %s", raw)
	}
	return cred.RefreshToken
}

// refreshWith asks the platform to exchange a refresh token, as a copy of data/
// would on another machine. It returns the platform's status.
func (m *mountedHarness) refreshWith(t *testing.T, refreshToken string) int {
	t.Helper()
	body, err := json.Marshal(map[string]any{"refreshToken": refreshToken})
	if err != nil {
		t.Fatalf("encode refresh: %v", err)
	}
	resp, err := http.Post(m.platformSrv.URL+"/v1/auth/refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func (m *mountedHarness) logout(t *testing.T) logoutBody {
	t.Helper()
	status, raw := m.request(t, http.MethodPost, "/api/product/logout", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("logout = %d, want 200 (body: %.300s)", status, raw)
	}
	var body logoutBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode logout (%v): %.300s", err, raw)
	}
	return body
}

// 1. THE ACCEPTANCE (L-E2's reverse criterion). A copy of data/ that was logged
// out can no longer refresh; before PR-2f this returned 200 and a fresh pair.
func TestLogoutMakesACopiedCredentialUseless(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)

	copied := m.copiedRefreshToken(t)

	body := m.logout(t)
	if !body.Revoked {
		t.Fatalf("logout reported revoked=false against a healthy platform")
	}
	if got := m.refreshWith(t, copied); got != http.StatusUnauthorized {
		t.Fatalf("refresh with the credential a copy would carry = %d, want 401 (the copy must not outlive the logout)", got)
	}
}

// 2. THE LOCAL SESSION STILL ENDS, the same way it did before: the credential
// file goes and the login flag drops. The activation record stays, because
// logging out is not un-activating (E7).
func TestLogoutStillClearsTheLocalSession(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)
	if !m.credentialFileExists() {
		t.Fatalf("activation wrote no credential.json, so this nail would prove nothing")
	}

	m.logout(t)

	if m.credentialFileExists() {
		t.Fatalf("credential.json survived logout")
	}
	if state := m.readStateFile(); state["loggedIn"] != false {
		t.Fatalf("loggedIn = %v, want false", state["loggedIn"])
	}
	status, raw := m.request(t, http.MethodGet, "/api/product/state", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("state = %d, want 200", status)
	}
	// Decoded as the object itself: 本地API契约 §1.4 #1 returns state unwrapped,
	// unlike login's {"state": ...} (runtime.go:298 records the asymmetry).
	var envelope struct {
		Activated bool `json:"activated"`
		LoggedIn  bool `json:"loggedIn"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if !envelope.Activated {
		t.Fatalf("activated = %v after logout, want true (logout must not un-activate)", envelope.Activated)
	}
	if envelope.LoggedIn {
		t.Fatalf("loggedIn = true after logout, want false")
	}
}

// 3. AN UNREACHABLE PLATFORM MUST NOT TRAP THE USER (PQ29 option 1). The local
// logout happens and the answer says the platform session was NOT revoked, so the
// UI can tell the truth instead of implying it was.
func TestLogoutWithAnUnreachablePlatformStillLogsOutLocallyAndSaysSo(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)

	// The platform goes away after the login - a pulled u-disk, a dropped
	// connection, or simply no network.
	m.platformSrv.Close()

	body := m.logout(t)
	if body.Revoked {
		t.Fatalf("logout reported revoked=true against an unreachable platform")
	}
	if !body.OK {
		t.Fatalf("ok = false, want true: the user asked to sign out and did sign out")
	}
	if m.credentialFileExists() {
		t.Fatalf("credential.json survived logout (the local half must not depend on the network)")
	}
	if state := m.readStateFile(); state["loggedIn"] != false {
		t.Fatalf("loggedIn = %v, want false", state["loggedIn"])
	}
}

// 4. A REFUSED SESSION IS THE OTHER FAILURE SHAPE, handled the same way from the
// user's side: he still gets signed out. The two shapes stay distinguishable
// inside the client (only one is fixed by signing in again, V-43) but are not
// duplicated into the response, because the UI renders one sentence for both.
func TestLogoutWithARefusedSessionStillLogsOutLocally(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)
	// Injected on the revoke itself rather than reached with
	// RefuseSessionsAfterLogin: that switch only affects tokens issued after it is
	// armed, and arming it before the login fails the runtime's own post-login
	// catalog fetch first - the nail would then be about a different failure.
	m.platform.FailLogout(http.StatusUnauthorized, "unauthorized")

	body := m.logout(t)
	if body.Revoked {
		t.Fatalf("logout reported revoked=true while the platform refused the session")
	}
	if m.credentialFileExists() {
		t.Fatalf("credential.json survived logout")
	}
	if state := m.readStateFile(); state["loggedIn"] != false {
		t.Fatalf("loggedIn = %v, want false", state["loggedIn"])
	}
}

// 5. THE ROUTE EXISTS ON THE PLATFORM, and refuses without a bearer. A 404 here
// would mean the client's call goes nowhere while the response still claims
// "revoked" - the failure mode where a local act is dressed up as a remote one.
func TestTheStandinServesTheLogoutRoute(t *testing.T) {
	platform := clienttest.New()
	srv := httptest.NewServer(platform.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/auth/logout", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post logout: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Fatalf("POST /v1/auth/logout = 404, want the route to exist (the contract has had it since 交付包 §4.1 #4)")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /v1/auth/logout without a bearer = %d, want 401", resp.StatusCode)
	}
}
