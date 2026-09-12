package productruntime

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productstate"
)

// These tests live in the package rather than the _test package because the
// subject is the platform-failure funnel itself, which is unexported on purpose:
// it is the one place every handler must pass through, so it must not become
// part of the package's callable surface.
//
// L-A6 says a session the platform has revoked ends with the local credential
// gone and the user back on the login page. The funnel is where that is
// enforced, so its input is an error value regardless of which call produced it.
// The first authorised platform call (the catalog fetch, PR-4b) is what makes the
// path reachable end to end; until then this is the only place to pin it.

// sessionFixture opens the two stores a runtime needs against a temporary data
// root and returns a runtime plus the paths it will consult.
type sessionFixture struct {
	rt       *Runtime
	state    *productstate.Store
	creds    *credentialstore.Store
	dataRoot string
}

func newSessionFixture(t *testing.T) *sessionFixture {
	t.Helper()
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
	return &sessionFixture{
		rt:       New(Deps{State: state, Creds: creds}),
		state:    state,
		creds:    creds,
		dataRoot: root,
	}
}

// signIn leaves the fixture in the state a successful activation produces: an
// activation record, a bound number, and a refresh token on disk.
func (f *sessionFixture) signIn(t *testing.T) {
	t.Helper()
	if err := f.state.ApplyLogin(productstate.LoginOutcome{
		PhoneMasked: "138****1234",
		Nickname:    "tester",
		ActivatedAt: "2026-09-12T00:00:00Z",
		ExpiresAt:   "2027-09-12T00:00:00Z",
		BoxCode:     "BOX-TEST",
	}, f.state.State().Credits); err != nil {
		t.Fatalf("ApplyLogin: %v", err)
	}
	if err := f.creds.Save(credentialstore.Credential{
		RefreshToken:       "refresh-token-under-test",
		AccountPhoneMasked: "138****1234",
		InstallID:          f.state.InstallID(),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// credentialOnDisk reports whether the refresh token is still readable. The
// boolean from Load is the presence flag; a missing file is not an error.
func (f *sessionFixture) credentialOnDisk() bool {
	_, present, err := f.creds.Load()
	return err == nil && present
}

// TestSessionExpiryClearsTheCredential is L-A6: a refused refresh token must not
// leave a usable credential behind.
func TestSessionExpiryClearsTheCredential(t *testing.T) {
	f := newSessionFixture(t)
	f.signIn(t)
	if !f.credentialOnDisk() {
		t.Fatal("fixture did not write a credential; the assertion below would pass vacuously")
	}

	rec := httptest.NewRecorder()
	f.rt.failPlatform(rec, productclient.ErrSessionExpired)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (the unauthorized tier)", rec.Code, http.StatusUnauthorized)
	}
	if body := rec.Body.String(); !containsCode(body, productclient.CodeUnauthorized) {
		t.Errorf("body = %s, want the %s code", body, productclient.CodeUnauthorized)
	}
	if f.credentialOnDisk() {
		t.Error("the credential is still on disk after the platform refused the refresh token")
	}
}

// TestSessionExpiryKeepsTheActivationRecord is the other half of L-A6, and the
// one that is easy to lose by reusing the logout path wholesale: an expired
// session is not an un-activation (需求基线 E7). If this regresses, the user has
// to re-enter an activation code that can only be used once - a paid customer
// locked out by a routine token rotation.
func TestSessionExpiryKeepsTheActivationRecord(t *testing.T) {
	f := newSessionFixture(t)
	f.signIn(t)

	rec := httptest.NewRecorder()
	f.rt.failPlatform(rec, productclient.ErrSessionExpired)

	st := f.state.PublicState()
	if st.LoggedIn {
		t.Error("loggedIn is still true after the session expired")
	}
	if !st.Activated {
		t.Error("activated was cleared; an expired session is not an un-activation (E7)")
	}
	if st.Activation == nil || st.Activation.BoxCode != "BOX-TEST" {
		t.Errorf("activation record = %+v, want the box code preserved", st.Activation)
	}
	if st.Account == nil || st.Account.PhoneMasked != "138****1234" {
		t.Errorf("account = %+v, want the bound number preserved for the second login", st.Account)
	}
}

// TestOutagesDoNotClearTheCredential is the nail in the other direction, and the
// reason the funnel keys on ErrSessionExpired rather than on "the call failed".
// Clearing on an outage would turn a flaky network into a forced re-login, and a
// customer whose drive is briefly unreadable loses nothing but their session.
func TestOutagesDoNotClearTheCredential(t *testing.T) {
	cases := map[string]error{
		"transport failure never reached the platform": &productclient.Error{
			Code: productclient.CodeNetworkUnavailable,
		},
		"the platform answered 5xx": &productclient.Error{
			Code:   productclient.CodeUpstreamUnavailable,
			Status: http.StatusBadGateway,
		},
		"the account is restricted, which is not an expired session": &productclient.Error{
			Code:   "account_restricted",
			Status: http.StatusForbidden,
		},
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSessionFixture(t)
			f.signIn(t)

			rec := httptest.NewRecorder()
			f.rt.failPlatform(rec, err)

			if !f.credentialOnDisk() {
				t.Error("the credential was cleared on a failure that does not mean the session is gone")
			}
			if !f.state.PublicState().LoggedIn {
				t.Error("loggedIn was cleared on a failure that does not mean the session is gone")
			}
		})
	}
}

// TestUnreadableCredentialStillReturnsToTheLoginPage covers the bounded
// degradation in P4-拦截页.md §4.4: when the credential cannot be removed, the
// user must still land on the login page rather than on a workspace whose every
// request will fail. The store directory is made read-only to force the failure.
func TestUnreadableCredentialStillReturnsToTheLoginPage(t *testing.T) {
	f := newSessionFixture(t)
	f.signIn(t)

	// A read-only directory makes the rename-aside that Delete relies on fail.
	if err := os.Chmod(f.dataRoot, 0o500); err != nil {
		t.Skipf("cannot make the data root read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.dataRoot, 0o700) })

	rec := httptest.NewRecorder()
	f.rt.failPlatform(rec, productclient.ErrSessionExpired)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d even when the credential cannot be removed",
			rec.Code, http.StatusUnauthorized)
	}
	if f.state.PublicState().LoggedIn {
		t.Error("loggedIn survived; the interface would keep showing a session that is gone")
	}
}

// containsCode is a tiny helper so the assertions read as intent rather than
// as string plumbing.
func containsCode(body, code string) bool {
	return strings.Contains(body, `"code":"`+code+`"`) ||
		strings.Contains(body, `"code": "`+code+`"`)
}
