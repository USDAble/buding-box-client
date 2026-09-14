package productclient_test

// V-54: logout must revoke the platform session.
//
// WHAT THESE NAILS ARE FOR. handleLogout used to delete the local credential and
// clear the login flag, both of which are local acts - it never went out to the
// network. The contract has carried `POST /v1/auth/logout` ("revoke the current
// refresh token / session") since 交付包 §4.1 #4, and 需求基线 E7 says logout
// clears the server-side session too, but no client path ever called it. The
// user-visible consequence is the one an offline-first portable product cares
// about most (E1 rule 7): a COPY of data/ - the u-disk's whole reason for
// existing - kept refreshing after the user logged out.
//
// WHY A SECOND CLIENT STANDS IN FOR THE COPY. The scenario is "someone copied
// data/ before the logout, and the logout has to kill that copy". Asserting with
// the original client would prove nothing: logging out clears its holder, so it
// could only report "no credential" and would pass even against a platform that
// still honoured the token. The copy therefore gets its OWN holder, seeded with
// the pair captured before the logout - exactly the bytes another machine would
// have - and the nail drives it through the client's ordinary authorised path.
//
// The platform here is the real stand-in on a real socket, because only the
// platform half can answer "is that token still good".

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// logoutFixture is the smallest thing that can hold a session: a stand-in, the
// signed-in client, and the same stand-in address so a second client (the copy)
// can dial it.
type logoutFixture struct {
	standin *clienttest.Server
	baseURL string
	client  *productclient.Client
	creds   *productclient.CredentialHolder
}

func newLogoutFixture(t *testing.T) *logoutFixture {
	t.Helper()
	standin := clienttest.New()
	srv := httptest.NewServer(standin.Handler())
	t.Cleanup(srv.Close)

	baseURL := srv.URL + "/v1"
	tokens := &productclient.CredentialHolder{}
	client := productclient.New(baseURL, productclient.ClientMeta{
		Version: "test", Platform: "test", Arch: "test", InstallID: installID,
	}, tokens)

	ctx := context.Background()
	if _, err := client.SendSMS(ctx, productclient.SendSMSRequest{Phone: anyPhone}); err != nil {
		t.Fatalf("SendSMS: %v", err)
	}
	if _, err := client.Login(ctx, productclient.LoginRequest{
		Phone:          anyPhone,
		Code:           smsCode,
		Nickname:       "tester",
		ActivationCode: clienttest.FixtureActivationCode,
		BoxCode:        clienttest.FixtureBoxCode,
	}); err != nil {
		t.Fatalf("Login: %v", err)
	}
	return &logoutFixture{standin: standin, baseURL: baseURL, client: client, creds: tokens}
}

// copyOf returns a second client holding the same credentials, as another machine
// with a copied data/ would. An independent holder is the point: the copy must
// not inherit the original's later state (see the file comment).
func (f *logoutFixture) copyOf(t *testing.T) (*productclient.Client, *productclient.CredentialHolder) {
	t.Helper()
	held := f.creds.Get()
	if held.RefreshToken == "" || held.AccessToken == "" {
		t.Fatalf("the signed-in holder is missing a token: %+v", held)
	}
	tokens := &productclient.CredentialHolder{}
	tokens.Set(held)
	return productclient.New(f.baseURL, productclient.ClientMeta{
		Version: "test", Platform: "test", Arch: "test", InstallID: installID,
	}, tokens), tokens
}

// 1. THE ACCEPTANCE (L-E2's reverse criterion). A copy that worked before the
// logout cannot sign in after it. Before PR-2f both halves of this nail passed
// the control and the assertion failed - the copy kept refreshing.
func TestALoggedOutCopyOfTheCredentialCanNoLongerSignIn(t *testing.T) {
	f := newLogoutFixture(t)
	copied, _ := f.copyOf(t)

	// Control: the copy is alive to begin with. Bootstrap is used because it is an
	// authorised call the app really makes at startup, and because it spends the
	// access token rather than the refresh token - so it does not itself rotate
	// the credential this nail is about.
	if _, err := copied.Bootstrap(context.Background()); err != nil {
		t.Fatalf("the copy failed before any logout (%v), so this nail would prove nothing", err)
	}

	if err := f.client.Logout(context.Background()); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if _, err := copied.Bootstrap(context.Background()); !errors.Is(err, productclient.ErrSessionExpired) {
		t.Fatalf("the copy after logout = %v, want ErrSessionExpired (the copied credential must not outlive the logout)", err)
	}
}

// 2. THE HOLDER IS RELEASED ON SUCCESS. The session is over by the user's own act,
// so no token is left behind for the gateway sender to present - the same rule
// doAuthorized applies to a refused refresh.
func TestLogoutDropsTheHeldCredentials(t *testing.T) {
	f := newLogoutFixture(t)

	if err := f.client.Logout(context.Background()); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if got := f.creds.Get(); got.AccessToken != "" || got.RefreshToken != "" {
		t.Fatalf("holder after logout = %+v, want both tokens empty", got)
	}
}

// 3. AN EXPIRED ACCESS TOKEN MUST NOT BLOCK LOGOUT. This is why Logout goes
// through doAuthorized: a token that lapsed while the app sat open is the ordinary
// state, and the session still has to be revocable then. The stand-in refuses a
// bearer it does not know, so this exercises the exchange-and-replay.
func TestLogoutRevokesEvenWhenTheAccessTokenHadLapsed(t *testing.T) {
	f := newLogoutFixture(t)
	copied, _ := f.copyOf(t)

	// Lapse the access token, keeping the refresh token valid - what time does.
	held := f.creds.Get()
	f.creds.Set(productclient.Credentials{RefreshToken: held.RefreshToken})

	if err := f.client.Logout(context.Background()); err != nil {
		t.Fatalf("Logout with a lapsed access token = %v, want success (the exchange should carry it)", err)
	}
	if _, err := copied.Bootstrap(context.Background()); !errors.Is(err, productclient.ErrSessionExpired) {
		t.Fatalf("the copy after logout = %v, want ErrSessionExpired (the revoke must have happened under the refreshed token)", err)
	}
}

// 4. AN UNREACHABLE PLATFORM IS REPORTED, NOT SWALLOWED. The two outcomes must
// stay distinguishable, because only one of them is a fact about the session -
// and the caller has to be able to tell the user which happened (PQ29 option 1).
func TestLogoutReportsAnUnreachablePlatform(t *testing.T) {
	f := newLogoutFixture(t)

	// A server that is closed before use: connection refused, no verdict in it.
	dead := httptest.NewServer(f.standin.Handler())
	dead.Close()
	unreachable := productclient.New(dead.URL+"/v1", productclient.ClientMeta{
		Version: "test", Platform: "test", Arch: "test", InstallID: installID,
	}, f.creds)

	err := unreachable.Logout(context.Background())
	if err == nil {
		t.Fatalf("Logout against an unreachable platform = nil, want an error the caller can report")
	}
	if errors.Is(err, productclient.ErrSessionExpired) {
		// A refused session and a dead network are different facts, and V-43 is the
		// incident that came from conflating them.
		t.Fatalf("Logout against an unreachable platform = ErrSessionExpired, want a transport error")
	}
}
