package productruntime

// A restarted process holds a refresh token and nothing else (E6 规则 2), while
// the gateway turn presents whatever token the holder has - so without an
// exchange the first turn of every launch is refused (V-39). These nails cover
// the exchange, the deadline it runs under, and the write that keeps the NEXT
// launch from being stranded (V-42).
//
// They live in the package because the subject is the wiring between three
// owners that are all package-level here: the client that owns the rotation, the
// credential store that owns the file, and the endpoint factory that owns the
// turn.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
)

// relaunched builds the pair a fresh process has: a holder carrying only what is
// on disk, and the client that will spend it. session_test.go's restart returns
// only the client because those nails never look inside the holder.
func (f *catalogFixture) relaunched() (*productclient.CredentialHolder, *productclient.Client) {
	f.t.Helper()
	tokens := &productclient.CredentialHolder{}
	RestoreSession(f.rt.deps.State, f.rt.deps.Creds, tokens)
	client := productclient.New(f.baseURL, productclient.ClientMeta{
		Version: "test", Platform: "test", Arch: "test", InstallID: f.rt.deps.State.InstallID(),
	}, tokens)
	return tokens, client
}

// renewerFor is the assembly cmd/octo-desktop performs, in three fields.
func (f *catalogFixture) renewerFor(tokens *productclient.CredentialHolder, client *productclient.Client) SessionRenewer {
	return SessionRenewer{Platform: client, Tokens: tokens, Creds: f.rt.deps.Creds, State: f.rt.deps.State}
}

// gatewayFor is the endpoint the provider dials, wired to the same holder the
// renewal fills.
func (f *catalogFixture) gatewayFor(tokens *productclient.CredentialHolder, ensure func(context.Context) error) GatewayEndpoint {
	return GatewayEndpoint{Host: f.baseURL, Tokens: tokens, Ensure: ensure}
}

func (f *catalogFixture) credentialFile() []byte {
	f.t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.root, "credential.json"))
	if err != nil {
		f.t.Fatalf("read credential.json: %v", err)
	}
	return raw
}

// TestARelaunchedProcessReachesTheGatewayWithoutSigningInAgain is V-39's
// acceptance: restart, then send a message. Before the renewal existed the turn
// was refused locally as "not signed in", with a valid credential sitting on
// disk the whole time.
func TestARelaunchedProcessReachesTheGatewayWithoutSigningInAgain(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.firstProcess()

	tokens, client := f.relaunched()
	if tokens.AccessToken() != "" {
		t.Fatal("the relaunch already holds an access token; this nail would prove nothing")
	}

	reply, err := streamText(t, f.gatewayFor(tokens, f.renewerFor(tokens, client).Ensure))
	if err != nil {
		t.Fatalf("a gateway turn after a relaunch = %v, want success from the credential on disk (V-39)", err)
	}
	if !strings.Contains(reply, "buding-privacy-1") {
		t.Errorf("reply = %q, want the bare catalog id the gateway was asked for", reply)
	}
	// Not just "it worked": it must have worked by spending the restored token,
	// so this pins the exchange rather than the stand-in being lenient.
	if n := f.platform.RefreshCount(); n != 1 {
		t.Errorf("refresh count = %d, want exactly 1", n)
	}
}

// TestTheRotatedTokenIsOnDiskWithTheTurn is V-42's acceptance. The platform
// rotates on every refresh and the old value stops working, so a build that only
// fixed the exchange would hand the next launch a token the platform has already
// thrown away.
//
// It asserts the disk value is a token the platform ACCEPTS, not merely that the
// file changed - "a file was written" and "the right file was written" are
// different claims, and only the second one keeps the user signed in.
func TestTheRotatedTokenIsOnDiskWithTheTurn(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.firstProcess()
	spent := f.login.RefreshToken

	tokens, client := f.relaunched()
	if _, err := streamText(t, f.gatewayFor(tokens, f.renewerFor(tokens, client).Ensure)); err != nil {
		t.Fatalf("the gateway turn = %v", err)
	}

	cred, present, err := f.rt.deps.Creds.Load()
	if err != nil || !present {
		t.Fatalf("credential unreadable after the turn (present=%v, err=%v)", present, err)
	}
	if got := tokens.Get().RefreshToken; cred.RefreshToken != got {
		t.Errorf("credential holds %q, want the token now in memory (%q)", cred.RefreshToken, got)
	}
	if cred.RefreshToken == spent {
		t.Error("the credential still holds the pre-exchange token; the rotation was not written")
	}
	// The fields that are not this operation's business must survive it: the
	// masked number is what an offline start shows, and the install id is the
	// state file's value mirrored for diagnostics (E6.3).
	if cred.AccountPhoneMasked != f.login.Account.PhoneMasked {
		t.Errorf("accountPhoneMasked = %q, want %q", cred.AccountPhoneMasked, f.login.Account.PhoneMasked)
	}
	if want := f.rt.deps.State.InstallID(); cred.InstallID != want {
		t.Errorf("installId = %q, want %q", cred.InstallID, want)
	}
	if cred.SchemaVersion != credentialstore.CurrentSchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", cred.SchemaVersion, credentialstore.CurrentSchemaVersion)
	}
	if cred.ObtainedAt == "" {
		t.Error("obtainedAt is empty; the token in this file is newly obtained and the field is what says when")
	}

	// The two claims together: what is on disk can be spent, and what was there
	// before cannot.
	if _, err := client.Refresh(context.Background(), spent); err == nil {
		t.Error("the pre-exchange token still works; this stand-in is not rotating, so the nail above proves nothing")
	}
	if _, err := client.Refresh(context.Background(), cred.RefreshToken); err != nil {
		t.Errorf("the token on disk was refused: %v", err)
	}
}

// TestWithoutTheWriteTheNextLaunchIsStranded is the counter-nail that makes the
// pair above a requirement rather than a preference. It performs exactly the
// exchange without the write, which is what fixing V-39 alone would ship, and
// shows the consequence: the launch after that is refused, and the user is sent
// back to the login screen with a usable credential having been deleted.
func TestWithoutTheWriteTheNextLaunchIsStranded(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.firstProcess()

	// The exchange, with nothing owning the file: the turn works.
	tokens, client := f.relaunched()
	renew := SessionRenewer{Platform: client, Tokens: tokens} // no Creds: nothing is written
	if _, err := streamText(t, f.gatewayFor(tokens, renew.Ensure)); err != nil {
		t.Fatalf("the first turn after a relaunch = %v", err)
	}

	// The next launch restores the value the platform has already rotated away.
	tokens2, client2 := f.relaunched()
	if _, err := streamText(t, f.gatewayFor(tokens2, f.renewerFor(tokens2, client2).Ensure)); err == nil {
		t.Fatal("the second relaunch succeeded on a token the platform had already spent; the write is not what V-42 is about")
	}
	if _, present, _ := f.rt.deps.Creds.Load(); present {
		t.Error("the spent credential is still on disk; the start after this one would repeat the same failure")
	}
	if f.rt.deps.State.State().LoggedIn {
		t.Error("loggedIn survived a session the platform refused")
	}
}

// A refused refresh token means the session is over (C12/L-A6), and the turn
// must not be the only thing that knows: the credential leaves the disk, the
// login flag goes down, and there is one attempt rather than a retry loop.
func TestARefusedRefreshEndsTheSessionAndLeavesNoCredential(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.firstProcess()
	// A value the platform never issued: what a revoked or rotated-away session
	// looks like to the next launch.
	if err := f.rt.deps.Creds.Save(credentialstore.Credential{
		RefreshToken:       "rt_revoked",
		AccountPhoneMasked: f.login.Account.PhoneMasked,
		InstallID:          f.rt.deps.State.InstallID(),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tokens, client := f.relaunched()
	_, err := streamText(t, f.gatewayFor(tokens, f.renewerFor(tokens, client).Ensure))
	if err == nil {
		t.Fatal("want the turn refused when the platform refuses the refresh token")
	}
	if !IsSessionExpired(err) {
		t.Errorf("turn error = %v, want it to carry the expired-session tier so the caller handles it as L-A6", err)
	}
	if got := tokens.Get(); got.AccessToken != "" || got.RefreshToken != "" {
		t.Errorf("holder = %+v, want it emptied", got)
	}
	if _, present, _ := f.rt.deps.Creds.Load(); present {
		t.Error("the credential is still on disk after the platform refused the session")
	}
	if f.rt.deps.State.State().LoggedIn {
		t.Error("loggedIn is still true; the next start would show a session that is gone")
	}
	if n := f.platform.RefreshCount(); n != 1 {
		t.Errorf("refresh count = %d, want 1: a refused token does not heal by retrying", n)
	}
}

// The exchange is bounded, so a gateway that accepts a connection and never
// answers holds nothing open (开发规范 §3.9). The deadline is asserted where it
// belongs - on the context the renewal is handed - because a turn that waits
// forever cannot be distinguished from a turn that is working.
func TestTheTokenExchangeRunsUnderADeadline(t *testing.T) {
	stub := &gatewayStub{}
	var remaining time.Duration
	var sawDeadline bool
	held := &productclient.CredentialHolder{} // signed out

	endpoint := GatewayEndpoint{
		Host:   stub.start(t),
		Tokens: held,
		Ensure: func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			sawDeadline = ok
			if ok {
				remaining = time.Until(deadline)
			}
			return errNoTokenInThisStub
		},
	}

	_, err := endpoint.Sender(reasoningOff)
	if err == nil {
		t.Fatal("want the turn refused when the exchange fails")
	}
	if !sawDeadline {
		t.Error("the exchange was handed a context with no deadline")
	}
	if sawDeadline && (remaining <= 0 || remaining > gatewayEnsureBudget) {
		t.Errorf("deadline is %.1fs away, want it within (0s, %s]", remaining.Seconds(), gatewayEnsureBudget)
	}
	// The reason the exchange failed has to reach the reader, or an operator
	// chasing "the turn did not start" has nothing to go on.
	if !strings.Contains(err.Error(), errNoTokenInThisStub.Error()) {
		t.Errorf("error = %v, want it to name why the exchange failed", err)
	}
	if _, _, _, _, hits := stub.seen(); hits != 0 {
		t.Errorf("the gateway was dialed %d time(s) after a failed exchange", hits)
	}
}

var errNoTokenInThisStub = errors.New("stub: no token configured")

// Fail closed, from the other end: with nothing to exchange there is no request
// to make, so neither the platform nor the gateway may be dialed.
func TestNoCredentialRefusesTheTurnWithoutDialingAnything(t *testing.T) {
	f := newCatalogFixture(t)
	stub := &gatewayStub{}
	tokens := &productclient.CredentialHolder{}
	client := productclient.New(f.baseURL, productclient.ClientMeta{
		Version: "test", Platform: "test", Arch: "test", InstallID: f.rt.deps.State.InstallID(),
	}, tokens)

	if _, err := (f.gatewayFor(tokens, f.renewerFor(tokens, client).Ensure)).Sender(reasoningOff); err == nil {
		t.Fatal("want the turn refused with no credential at all")
	}
	if n := f.platform.RefreshCount(); n != 0 {
		t.Errorf("the platform was asked to refresh %d time(s) with no credential to spend", n)
	}
	if _, _, _, _, hits := stub.seen(); hits != 0 {
		t.Errorf("the gateway was dialed %d time(s) with no credential", hits)
	}
}

// A data root that cannot be written (the U-disk write-protect switch, a full
// disk) must not cost the user the session they are in the middle of. It costs
// them the NEXT launch, and the log has to say so rather than let the failure be
// discovered a day later as "I had to sign in again" (§3.9: name the fallback,
// say whether the user is told, say when it recovers).
func TestAReadOnlyDataRootStillChatsAndSaysSo(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.firstProcess()

	if err := os.Chmod(f.root, 0o500); err != nil {
		t.Skipf("cannot make the data root read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.root, 0o700) })
	if err := os.WriteFile(filepath.Join(f.root, "probe"), []byte("x"), 0o600); err == nil {
		_ = os.Remove(filepath.Join(f.root, "probe"))
		t.Skip("the data root is still writable (running as root?); this nail needs the write to fail")
	}
	before := f.credentialFile()

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	tokens, client := f.relaunched()
	reply, err := streamText(t, f.gatewayFor(tokens, f.renewerFor(tokens, client).Ensure))
	if err != nil {
		t.Fatalf("a read-only data root must not break the turn in progress: %v", err)
	}
	if reply == "" {
		t.Fatal("the stand-in streamed an empty reply")
	}
	if got := f.credentialFile(); !bytes.Equal(got, before) {
		t.Errorf("credential.json changed on a read-only root:\n before %s\n after  %s", before, got)
	}
	if !strings.Contains(logs.String(), "could not be written") {
		t.Errorf("nothing was logged about the credential that could not be written: %s", logs.String())
	}
}
