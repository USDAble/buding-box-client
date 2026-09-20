package productruntime_test

// L-C4a / PR-5d1: the balance is refreshed FROM THE PLATFORM, and the ledger read
// is the only thing that ever writes it.
//
// The rule these nails pin was decided by a human on 2026-09-14: "the balance
// must be refreshed from the server; every consumption is synchronised from the
// server; the server deducts the credits; the client just honestly re-reads the
// balance from the server". 需求基线 E9 rule 2 is where it lives.
//
// WHAT MAKES THIS FALSIFIABLE RATHER THAN FOLKLORE. The stand-in's number is
// moved with SetBalance, so "the number on screen followed the server" is an
// observation about a moved server value rather than about a fixture constant. A
// client that computed, cached or estimated anything locally would keep showing
// the old number, and the nails below go red.
//
// The other half is the negative one, and it is the reason the plan lists a
// failure for every fallback: a refused read must NOT become a number. Zero is
// the most likely legitimate balance in this product - it is what makes the
// gateway answer 402 - so an unreadable answer that decoded to 0 would tell a
// paying user they are out of credits, and would do it silently.

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// creditsOf pulls the credits object out of a state-shaped body, failing loudly
// when the body is not one (a 200 that is really the SPA's index.html would
// otherwise read as "no credits field").
func creditsOf(t *testing.T, state map[string]any) map[string]any {
	t.Helper()
	credits, ok := state["credits"].(map[string]any)
	if !ok {
		t.Fatalf("state has no credits object: %v", state)
	}
	return credits
}

// balanceOf reads the balance as the contract types it: a number.
func balanceOf(t *testing.T, state map[string]any) int64 {
	t.Helper()
	raw, ok := creditsOf(t, state)["balance"]
	if !ok {
		t.Fatalf("credits object has no balance: %v", state)
	}
	number, ok := raw.(float64)
	if !ok {
		t.Fatalf("balance is %T, want a number: %v", raw, raw)
	}
	return int64(number)
}

// stateFromBody unwraps the {"state": ...} envelope of §2.15/§2.6/§2.7.
func stateFromBody(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	state, ok := body["state"].(map[string]any)
	if !ok {
		t.Fatalf("body has no state object: %v", body)
	}
	return state
}

// TestTheCreditsEndpointReadsTheLedgerAndPersistsIt is the positive path of
// 本地API契约 §2.15: the endpoint reads the ledger, the number lands in the answer
// AND on disk, and the read happened exactly once.
//
// The count matters. "Refresh on every trigger" is the rule; a client that read
// the ledger twice per call, or not at all and answered from a cache, would both
// pass a value-only assertion while breaking the rule in opposite directions.
func TestTheCreditsEndpointReadsTheLedgerAndPersistsIt(t *testing.T) {
	h := newHarness(t)
	h.activate()

	readsAfterLogin := h.platform.LedgerReads()

	h.platform.SetBalance(4_200_000)
	status, body := h.do(http.MethodGet, "/api/product/credits", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/product/credits = %d, want 200 (body: %v)", status, body)
	}

	if got := balanceOf(t, stateFromBody(t, body)); got != 4_200_000 {
		t.Errorf("balance in the answer = %d, want 4200000", got)
	}
	if got := balanceOf(t, h.readStateFile()); got != 4_200_000 {
		t.Errorf("balance on disk = %d, want 4200000: the answer must be the persisted truth", got)
	}
	if got := h.platform.LedgerReads(); got != readsAfterLogin+1 {
		t.Errorf("ledger reads = %d, want %d: one refresh reads the ledger exactly once",
			got, readsAfterLogin+1)
	}
}

// TestTheNumberOnScreenMovesOnlyWhenTheLedgerIsRead is the single-write-path rule
// stated as an observation rather than a claim.
//
// It reads the state twice with a server whose number has already moved, and the
// only thing in between is the ledger read. If a second path existed - the login
// answer's own credits field, a terminal frame's balance, a local estimate - the
// first read would show the new number and this nail would pass for the wrong
// reason. As written, the first read showing the OLD number is the assertion: a
// screen that has not asked the ledger does not know anything new.
func TestTheNumberOnScreenMovesOnlyWhenTheLedgerIsRead(t *testing.T) {
	h := newHarness(t)
	h.activate()

	stale := h.platform.Balance()
	if stale == 0 {
		t.Fatalf("the stand-in's starting balance is 0, which makes this nail vacuous")
	}
	fresh := stale + 1

	h.platform.SetBalance(fresh)

	// No ledger read yet: the state must still hold what the platform last said.
	status, body := h.do(http.MethodGet, "/api/product/state", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/product/state = %d, want 200", status)
	}
	if got := balanceOf(t, body); got != stale {
		t.Errorf("balance before any ledger read = %d, want the last known %d: "+
			"something other than the ledger read wrote it", got, stale)
	}

	// The one trigger that does read it.
	if status, body = h.do(http.MethodGet, "/api/product/credits", nil); status != http.StatusOK {
		t.Fatalf("GET /api/product/credits = %d, want 200 (body: %v)", status, body)
	}

	if status, body = h.do(http.MethodGet, "/api/product/state", nil); status != http.StatusOK {
		t.Fatalf("GET /api/product/state = %d, want 200", status)
	}
	if got := balanceOf(t, body); got != fresh {
		t.Errorf("balance after the ledger read = %d, want %d", got, fresh)
	}
}

// TestALedgerFailureKeepsTheOldValueAndTheCredential is the outage half.
//
// Three things must hold together, and each has been got wrong somewhere in this
// codebase before:
//
//   - the stored value does not move (a transport failure has no verdict in it,
//     V-43 - so it cannot be read as "your balance is now 0");
//   - the answer says what happened, through the shared funnel, so the UI can
//     show an outage rather than a wrong number;
//   - the credential stays, because an outage is not a logout (this is the L-A6
//     rule read in the other direction: only a REFUSED session clears it).
func TestALedgerFailureKeepsTheOldValueAndTheCredential(t *testing.T) {
	h := newHarness(t)
	h.activate()

	before := balanceOf(t, h.readStateFile())
	if before == 0 {
		t.Fatalf("the fixture balance is 0, which makes this nail unable to see a wipe")
	}
	if !h.credentialFileExists() {
		t.Fatal("fixture has no credential; the assertion below would pass vacuously")
	}

	h.platform.FailLedger(http.StatusBadGateway, productclient.CodeUpstreamUnavailable)

	status, body := h.do(http.MethodGet, "/api/product/credits", nil)
	if status == http.StatusOK {
		t.Fatalf("a failed ledger read answered 200: %v", body)
	}
	if code := codeOf(t, body); code != productclient.CodeUpstreamUnavailable {
		t.Errorf("code = %q, want %q (the platform's own code, forwarded)",
			code, productclient.CodeUpstreamUnavailable)
	}
	if got := balanceOf(t, h.readStateFile()); got != before {
		t.Errorf("balance moved on a failed read: %d, want %d", got, before)
	}
	if !h.credentialFileExists() {
		t.Error("the credential was cleared by an outage: an outage is not a logout (L-A6)")
	}
}

// TestAMalformedLedgerIsNotReadAsZero pins the absent-versus-zero distinction
// that productclient.ErrLedgerMalformed exists for.
//
// A successful HTTP answer carrying no balanceMicroCredits is a real shape: a
// platform that changed its mind, or a proxy that stripped the field. Decoding it
// into an int64 reads as 0, and 0 in this product is not "unknown" - it is "out
// of credits". So the failure must keep the old number.
//
// The status is 503 network_unavailable, which is the envelope's fallback for any
// error that is not a platform envelope (internal/productruntime/envelope.go).
// That wording is imprecise here - the network was fine and the platform's answer
// was unusable - and it is left as it is deliberately: the class (an outage the
// user may reasonably retry) is right, and a new local code would need a row in
// 本地API契约 §3 and a user-visible action to justify it. It is asserted rather
// than ignored so that a future reclassification has to come past this nail.
func TestAMalformedLedgerIsNotReadAsZero(t *testing.T) {
	h := newHarness(t)
	h.activate()

	before := balanceOf(t, h.readStateFile())
	if before == 0 {
		t.Fatalf("the fixture balance is 0; a wipe would be invisible")
	}

	h.platform.OmitLedgerBalance()

	status, body := h.do(http.MethodGet, "/api/product/credits", nil)
	if status == http.StatusOK {
		t.Fatalf("a ledger answer with no balance was accepted: %v", body)
	}
	if code := codeOf(t, body); code != productclient.CodeNetworkUnavailable {
		t.Errorf("code = %q, want %q (see this test's comment for why)",
			code, productclient.CodeNetworkUnavailable)
	}
	if got := balanceOf(t, h.readStateFile()); got != before {
		t.Errorf("balance = %d, want %d: an absent balance was read as a number", got, before)
	}
}

// TestALedgerFailureDoesNotBlockTheLogin pins B1 rule 1's shape for the balance,
// and it does double duty as the nail for "the login answer's own credits field
// is not read".
//
// The stand-in's login answer carries a balance of 12500. The ledger is broken.
// If the runtime read the balance out of the login answer - the field that sat
// there with a wrong json tag until V-57, and the reason the contract and the DTO
// disagreed for so long - the number below would be 12500. It must be 0, because
// nothing has told this fresh installation what its balance is, and a balance the
// client invented from a field it also refuses to trust would be the second write
// path E9 rule 2 exists to prevent.
func TestALedgerFailureDoesNotBlockTheLogin(t *testing.T) {
	h := newHarness(t)
	h.platform.SetBalance(12_500)
	h.platform.FailLedger(http.StatusBadGateway, productclient.CodeUpstreamUnavailable)

	state := h.activate()

	if balance := balanceOf(t, state); balance != 0 {
		t.Errorf("balance after a login whose ledger read failed = %d, want 0 "+
			"(12500 would mean the login answer's credits field was read)", balance)
	}
	// The login itself must have succeeded, and left a usable session behind.
	if loggedIn, _ := state["loggedIn"].(bool); !loggedIn {
		t.Error("loggedIn is false: a failed ledger read blocked the login (B1 rule 1)")
	}
	if !h.credentialFileExists() {
		t.Error("no credential after a login whose ledger read failed")
	}
	h.platform.FailLedger(0, "")
	if status, body := h.do(http.MethodGet, "/api/product/credits", nil); status != http.StatusOK {
		t.Fatalf("the ledger was still broken after clearing the injection: %d %v", status, body)
	}
}

// TestTheCreditsObjectCarriesOnlyTheBalance pins the collapse of the credits
// object to 本地API契约 §1.3's one field (2026-09-14).
//
// monthUsed and monthKey were removed because the platform sends neither and E9
// rule 5 forbids this client from summing by month - so both would have read 0
// for the life of the product, which is a screen claiming to know something it
// does not. Asserting the exact key set (rather than the absence of two names)
// is what stops a third field arriving the same way.
func TestTheCreditsObjectCarriesBalanceAndKnownState(t *testing.T) {
	h := newHarness(t)
	h.activate()

	status, body := h.do(http.MethodGet, "/api/product/state", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/product/state = %d, want 200", status)
	}

	credits := creditsOf(t, body)
	if len(credits) != 2 {
		names := make([]string, 0, len(credits))
		for name := range credits {
			names = append(names, name)
		}
		t.Errorf("credits object has %d fields (%v), want exactly [balance, known]",
			len(credits), strings.Join(names, ", "))
	}
	if known, ok := credits["known"].(bool); !ok || !known {
		t.Errorf("successful wallet must have known=true: %v", credits)
	}
	if _, ok := credits["balance"]; !ok {
		t.Errorf("credits object has no balance field: %v", credits)
	}
}

// TestTheCreditsRouteIsReachableThroughTheRealServer is PR-2b1's lesson applied
// to this endpoint: a handler that is correct but not mounted is a handler the
// browser cannot reach, and unit tests cannot tell the difference.
func TestTheCreditsRouteIsReachableThroughTheRealServer(t *testing.T) {
	m := newMountedHarness(t)
	// The login travels the same road, because the mounted harness closes the
	// bare handler server the shorter helper would use (mount_test.go).
	if status, raw := m.request(t, http.MethodPost, "/api/product/send-code",
		map[string]any{"phone": "13800001234"}, nil); status != http.StatusOK {
		t.Fatalf("send-code = %d, want 200 (body: %.200s)", status, raw)
	}
	if status, raw := m.request(t, http.MethodPost, "/api/product/login", map[string]any{
		"phone":          "13800001234",
		"code":           clienttest.FixtureSMSCode,
		"nickname":       "tester",
		"activationCode": clienttest.FixtureActivationCode,
		"boxCode":        clienttest.FixtureBoxCode,
	}, nil); status != http.StatusOK {
		t.Fatalf("login = %d, want 200 (body: %.200s)", status, raw)
	}
	m.platform.SetBalance(777_000)

	status, raw := m.request(t, http.MethodGet, "/api/product/credits", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/product/credits over the socket = %d, want 200 (body: %.200s)", status, raw)
	}

	var body struct {
		State struct {
			Credits struct {
				Balance int64 `json:"balance"`
			} `json:"credits"`
		} `json:"state"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body is not the state envelope (%v): %.200s", err, raw)
	}
	if body.State.Credits.Balance != 777_000 {
		t.Errorf("balance = %d, want 777000", body.State.Credits.Balance)
	}
}

// TestADeadSessionDuringABalanceRefreshReturnsToTheBlockedPage covers the branch
// that only exists because this call happens while a window is open.
//
// The platform is asked to forget the tokens it already issued, which is what a
// real revocation looks like from here (another device signed in, an operator
// killed the session). The refresh must then behave like every other authorised
// call: no credential left behind, and the user sent back to the blocked page
// rather than left with a screen that quietly stops updating (V-21 is the record
// of what happens when this branch is missed - the user is told their network is
// down and offered a retry that can never succeed).
func TestADeadSessionDuringABalanceRefreshReturnsToTheBlockedPage(t *testing.T) {
	h := newHarness(t)
	h.activate()
	if !h.credentialFileExists() {
		t.Fatal("fixture has no credential; the assertion below would pass vacuously")
	}

	h.platform.RevokeSessions()

	status, body := h.do(http.MethodGet, "/api/product/credits", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("a revoked session answered %d, want 401 (body: %v)", status, body)
	}
	if code := codeOf(t, body); code != productclient.CodeUnauthorized {
		t.Errorf("code = %q, want %q", code, productclient.CodeUnauthorized)
	}
	if h.credentialFileExists() {
		t.Error("the credential survived a refused refresh token: it does not heal (E12)")
	}
	if loggedIn, _ := h.readStateFile()["loggedIn"].(bool); loggedIn {
		t.Error("loggedIn is still true; a revoked session must return to the blocked page")
	}
}

// TestTheBalanceHasExactlyOneWriter is the single-owner rule checked against the
// source rather than against a claim, the way V-52 taught: it counts CALL SITES,
// not occurrences of a string.
//
// Production code only - tests may write the projection freely, which is how the
// store's own logout nail establishes a starting value - and the declaration
// itself is subtracted. Two callers would mean two paths to one number, which is
// exactly the shape 需求基线 E9 rule 2 removed.
func TestTheBalanceHasExactlyOneWriter(t *testing.T) {
	const declaration = "func (s *Store) SetCredits("
	root := filepath.Join("..", "..")

	var callers []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// ".." and "." name the walk root and its parents, not a hidden
			// directory: skipping them by prefix would skip the whole tree.
			name := d.Name()
			if name != ".." && name != "." && (name == "node_modules" || name == "dist" || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(src)
		total := strings.Count(text, "SetCredits(")
		if total == 0 {
			return nil
		}
		if n := strings.Count(text, declaration); n > 0 {
			total -= n
		}
		for i := 0; i < total; i++ {
			callers = append(callers, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree: %v", err)
	}

	if len(callers) != 1 {
		t.Errorf("SetCredits is called from %d places (%v), want exactly 1: "+
			"the balance has one source and one write path (需求基线 E9 rule 2)",
			len(callers), callers)
	}
	if len(callers) == 1 && !strings.HasSuffix(callers[0], filepath.Join("productruntime", "credits.go")) {
		t.Errorf("the only writer is %s, want the ledger read in productruntime/credits.go", callers[0])
	}
}
