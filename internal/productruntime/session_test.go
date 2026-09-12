package productruntime

// A restarted process must come back signed in (需求基线 E6, L-E2). The refresh
// token is the only long-lived credential and it lives on disk in
// data/credential.json; the access token deliberately does not (E6 规则 2), so a
// restart has nothing in memory and must put the disk value back into the holder
// the platform client uses.
//
// V-32 recorded the state these tests were written against: nothing read that
// file back. The first authorised call after a restart was therefore refused,
// the refused refresh made L-A6 clear the credential, and the user landed on the
// login page again — every launch. E6's own acceptance ("reopen the process and
// it logs in without a code") could not pass.
//
// The first test walks the whole road instead of asserting on a struct field,
// because "the holder has a string in it" is not the user-visible fact.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productstate"
)

// firstProcess replays the two writes handleLogin makes when a session begins:
// the account lands in product-state.json, and the refresh token lands in
// credential.json. Everything else the process held is dropped, which is what
// makes what follows a restart rather than a continuation.
func (f *catalogFixture) firstProcess() {
	f.t.Helper()
	if err := f.rt.deps.State.ApplyLogin(productstate.LoginOutcome{
		PhoneMasked: f.login.Account.PhoneMasked,
		Nickname:    f.login.Account.Nickname,
	}, productstate.Credits{}); err != nil {
		f.t.Fatalf("ApplyLogin: %v", err)
	}
	if err := f.rt.deps.Creds.Save(credentialstore.Credential{
		RefreshToken:       f.login.RefreshToken,
		AccountPhoneMasked: f.login.Account.PhoneMasked,
		InstallID:          f.rt.deps.State.InstallID(),
	}); err != nil {
		f.t.Fatalf("Save credential: %v", err)
	}
}

// restart builds the client a fresh process would use: a brand-new holder, no
// access token anywhere, and only what RestoreSession can find on disk.
func (f *catalogFixture) restart() *productclient.Client {
	f.t.Helper()
	tokens := &productclient.CredentialHolder{}
	RestoreSession(f.rt.deps.State, f.rt.deps.Creds, tokens)
	return productclient.New(f.baseURL, productclient.ClientMeta{
		Version: "test", Platform: "test", Arch: "test", InstallID: f.rt.deps.State.InstallID(),
	}, tokens)
}

// TestARestartedProcessStartsSignedIn is E6's acceptance in test form.
func TestARestartedProcessStartsSignedIn(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.firstProcess()

	client := f.restart()
	if _, err := client.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap after a restart = %v, want success from the credential on disk", err)
	}
	// Not just "it worked": it must have worked by spending the refresh token.
	// Without one there is nothing to trade, so this pins that the token — not
	// luck, and not the stand-in being lenient — carried the session.
	if n := f.platform.RefreshCount(); n != 1 {
		t.Errorf("refresh count = %d, want exactly 1 (the restored token)", n)
	}
}

// TestARestoredSessionCarriesNoAccessToken pins the other half of E6 规则 2: the
// credential file holds the refresh token and nothing else, so the restored
// holder must not arrive with a bearer token it could have taken from disk.
func TestARestoredSessionCarriesNoAccessToken(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.firstProcess()

	tokens := &productclient.CredentialHolder{}
	RestoreSession(f.rt.deps.State, f.rt.deps.Creds, tokens)
	if got := tokens.AccessToken(); got != "" {
		t.Errorf("restored access token = %q, want none (it is never written to disk)", got)
	}
	if got := tokens.Get().RefreshToken; got == "" {
		t.Error("no refresh token was restored; the restart has nothing to trade")
	}
}

// TestALoggedOutStateIsNotResurrected covers what a failed logout leaves behind:
// credentialstore.Delete can fail on a read-only stick, and E7 says a logout ends
// the session regardless. Restoring on the file's presence alone would sign the
// user back in after they asked to leave.
func TestALoggedOutStateIsNotResurrected(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.firstProcess()

	if err := f.rt.deps.State.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	// The delete is what normally removes the file; this test keeps it on
	// purpose, because the failure it models is the delete not happening.
	if _, err := os.Stat(filepath.Join(f.root, "credential.json")); err != nil {
		t.Fatalf("credential file missing (%v); this test needs it to survive the logout", err)
	}

	tokens := &productclient.CredentialHolder{}
	RestoreSession(f.rt.deps.State, f.rt.deps.Creds, tokens)
	if got := tokens.Get().RefreshToken; got != "" {
		t.Errorf("restored a session the logout was supposed to end (token %q)", got)
	}
}

// TestADamagedCredentialDoesNotStopStartup: the file is user-editable storage on
// a stick that gets yanked, so a truncated file must leave the process starting
// logged-out rather than dead (E6 规则 5).
func TestADamagedCredentialDoesNotStopStartup(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.firstProcess()

	if err := os.WriteFile(filepath.Join(f.root, "credential.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("damage the credential: %v", err)
	}

	tokens := &productclient.CredentialHolder{}
	RestoreSession(f.rt.deps.State, f.rt.deps.Creds, tokens)
	if got := tokens.Get().RefreshToken; got != "" {
		t.Errorf("restored %q from a damaged file, want nothing", got)
	}
}

// TestNoCredentialLeavesTheHolderEmpty is the first-launch case, and the guard
// against a restore that invents a session when there is nothing to restore.
func TestNoCredentialLeavesTheHolderEmpty(t *testing.T) {
	f := newCatalogFixture(t)
	if err := f.rt.deps.State.ApplyLogin(productstate.LoginOutcome{PhoneMasked: "138****9999"}, productstate.Credits{}); err != nil {
		t.Fatalf("ApplyLogin: %v", err)
	}

	tokens := &productclient.CredentialHolder{}
	RestoreSession(f.rt.deps.State, f.rt.deps.Creds, tokens)
	if got := tokens.Get(); got.RefreshToken != "" || got.AccessToken != "" {
		t.Errorf("holder = %+v, want empty when no credential exists", got)
	}
}
