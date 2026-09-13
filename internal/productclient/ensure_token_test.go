package productclient_test

// A process that has just started holds a refresh token and nothing else
// (需求基线 E6 规则 2: the access token is never written to disk). Every call
// that goes through doAuthorized repairs that by itself - it runs without a
// bearer, takes the 401 and refreshes. The gateway turn does not: it is an
// OpenAI-protocol request whose bearer comes straight from the holder, so with
// no token it can only be refused (V-39).
//
// EnsureToken is that repair, made callable. These nails are about the three
// answers it can give: it trades, it has nothing to do, or it refuses.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// relaunch drops the access token and keeps the refresh token, which is exactly
// what a restart leaves in memory.
func (h *harness) relaunch(t *testing.T) string {
	t.Helper()
	data, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	h.creds.Set(productclient.Credentials{RefreshToken: data.RefreshToken})
	return data.RefreshToken
}

func TestEnsureTokenTradesTheRefreshTokenForAnAccessToken(t *testing.T) {
	h := newHarness(t)
	old := h.relaunch(t)
	before := h.standin.RefreshCount()

	if err := h.client.EnsureToken(context.Background()); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}

	token := h.creds.AccessToken()
	if token == "" {
		t.Fatal("no access token after EnsureToken; the gateway turn would still be refused")
	}
	// Not just "something is held": it has to be the token the stand-in issued
	// for the exchange, or the first turn would be refused with a 401 instead.
	if n := h.standin.RefreshCount(); n != before+1 {
		t.Errorf("refresh count = %d, want %d (exactly one exchange)", n, before+1)
	}
	if _, err := h.client.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap with the exchanged token = %v; it is not a token the platform accepts", err)
	}
	// The old refresh token is spent: the platform rotates, and that is what
	// makes persisting the new one a requirement rather than a nicety (V-42).
	if _, err := h.client.Refresh(context.Background(), old); err == nil {
		t.Error("the pre-exchange refresh token still works; this stand-in is not rotating")
	}
}

func TestEnsureTokenIsANoOpWhileATokenIsHeld(t *testing.T) {
	h := newHarness(t)
	h.relaunch(t)
	ctx := context.Background()

	if err := h.client.EnsureToken(ctx); err != nil {
		t.Fatalf("first EnsureToken: %v", err)
	}
	before := h.standin.RefreshCount()
	if err := h.client.EnsureToken(ctx); err != nil {
		t.Fatalf("second EnsureToken: %v", err)
	}
	if n := h.standin.RefreshCount(); n != before {
		t.Errorf("refresh count went from %d to %d; a held token must not be spent again", before, n)
	}
}

// A refused refresh token means the session is over, and it must be over in
// memory too (C12/L-A6): leaving a dead token in the holder would have every
// later turn retry a refresh the platform has already said no to.
func TestARefusedRefreshEndsTheSessionInMemory(t *testing.T) {
	h := newHarness(t)
	h.creds.Set(productclient.Credentials{RefreshToken: "rt_never-issued"})

	err := h.client.EnsureToken(context.Background())
	if !errors.Is(err, productclient.ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	if got := h.creds.Get(); got.AccessToken != "" || got.RefreshToken != "" {
		t.Errorf("holder = %+v, want it emptied after a refusal", got)
	}
}

// The other direction, and the reason the refusal above is classified rather
// than assumed: an attempt that never reached the platform is not an answer.
// Clearing here would turn a dropped connection into a forced SMS login, which
// is what C12 forbids ("only unauthorized clears") and what
// TestOutagesDoNotClearTheCredential asserts one layer up.
func TestATransportFailureDuringTheExchangeKeepsTheCredential(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close() // nothing is listening: the exchange cannot complete

	creds := &productclient.CredentialHolder{}
	creds.Set(productclient.Credentials{RefreshToken: "rt_still-good"})
	client := productclient.New(dead.URL+"/v1", productclient.ClientMeta{
		Version: "test", Platform: "test", Arch: "test", InstallID: installID,
	}, creds)

	err := client.EnsureToken(context.Background())
	if err == nil {
		t.Fatal("want an error when the exchange cannot be completed")
	}
	if errors.Is(err, productclient.ErrSessionExpired) {
		t.Errorf("err = %v, want it NOT to claim the session ended: the platform never said so", err)
	}
	if got := creds.Get().RefreshToken; got != "rt_still-good" {
		t.Errorf("refresh token = %q, want it kept for the next attempt", got)
	}
}

// Fail closed: with nothing to trade there is no exchange to make, and the
// request must not go anywhere looking for one.
func TestEnsureTokenWithNoCredentialIsRefusedWithoutADial(t *testing.T) {
	h := newHarness(t)

	err := h.client.EnsureToken(context.Background())
	if !errors.Is(err, productclient.ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	if n := h.standin.RefreshCount(); n != 0 {
		t.Errorf("refresh count = %d, want 0 with no credential to spend", n)
	}
}
