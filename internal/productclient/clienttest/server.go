// Package clienttest is a stand-in for the central platform: a real HTTP
// handler that speaks the same envelopes, error codes and types as
// productclient, so the account lifecycle can be exercised without a network.
//
// It replaces the platform boundary, NOT the local service boundary. The
// frontend once carried its own stand-in for the latter (web/src/dev, removed
// by PR-3 on 2026-09-13); the two were never interchangeable, and the local
// boundary now has no substitute at all - /api/* always reaches the Go service.
//
// It is a library, not a command: callers wrap Handler with httptest. The
// runnable binary that drives it by hand is cmd/productstub, which serves
// Handler and prints the fixtures it accepts.
//
// It serves both halves of what the desktop build talks to: the control plane
// (auth + bootstrap) and, since PR-5a, the built-in gateway's completions
// endpoint (handleCompletions). They share one process because the developer
// profile points both hosts at it.
package clienttest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productphone"
)

// Fixture values. The BUDING-DEMO-* and BOX-DEMO-* strings are fixed ASCII data
// keys, not brand copy (开发规范 §3.1 规则 2). They used to be shared with a
// frontend stand-in that described the same test account; that stand-in is gone
// (PR-3), so this is now the only definition of them.
const (
	// FixtureActivationCode is the ordinary first-activation code.
	FixtureActivationCode = "BUDING-DEMO-0001"
	// FixtureSecondActivationCode is a second, unused code for the SAME box code.
	// It exists to pin the one-to-many rule: one box code accepts more than one
	// activation code (需求基线 E1 规则 2).
	FixtureSecondActivationCode = "BUDING-DEMO-0002"
	// FixtureBoundPhoneActivationCode is issued to FixtureBoundPhone. Redeeming
	// it from another phone must fail with phone_mismatch.
	FixtureBoundPhoneActivationCode = "BUDING-DEMO-0003"

	// FixtureBoxCode is the box the first two codes were issued with.
	FixtureBoxCode = "BOX-DEMO-0001"
	// FixtureOtherBoxCode is a real box, but not the one paired with
	// FixtureActivationCode - using them together is a mismatch, not an unknown.
	FixtureOtherBoxCode = "BOX-DEMO-0002"
	// FixtureUnknownBoxCode is a box no code was ever issued with.
	FixtureUnknownBoxCode = "BOX-DEMO-9999"

	// FixtureSMSCode is the only login code the stand-in accepts.
	FixtureSMSCode = "123456"
	// FixtureBoundPhone owns FixtureBoundPhoneActivationCode.
	FixtureBoundPhone = "13800000000"

	// StandinPromptTokens / StandinCompletionTokens are what the stand-in reports
	// in its usage frame for one turn (PR-5d2).
	//
	// Exported because the nails that prove the number travelled have to compare
	// against the same value the fixture sends, and because they are deliberately
	// NOT derivable from the reply: the fixture's answer is ~60 characters, so a
	// chars/4 estimate is around 15, while the prompt count is two orders of
	// magnitude larger and the completion count is not a whole number of
	// quarter-characters either. That gap is the point — a build that fell back
	// to the transcript estimate instead of reading the usage frame must fail
	// the nail rather than coincidentally match it (V-57).
	StandinPromptTokens     = 4096
	StandinCompletionTokens = 217

	fixtureBalanceMicroCredits = 12500
	fixtureAccessTokenTTL      = 7200
)

type codeFixture struct {
	boxCode string
	phone   string // non-empty when the code was issued to a specific buyer
}

type accountState struct {
	id          string
	phone       string
	phoneMasked string
	nickname    string
	activation  productclient.Activation
}

// Server is a stateful stand-in. It is safe for concurrent use.
type Server struct {
	mu sync.Mutex

	codes    map[string]codeFixture // activation code -> what it was issued with
	used     map[string]string      // activation code -> account that consumed it
	accounts map[string]*accountState
	smsSent  map[string]string         // phone -> login code
	access   map[string]string         // access token -> phone
	refresh  map[string]string         // refresh token -> phone
	feedback map[string]feedbackRecord // account + idempotency key -> accepted receipt

	seq          int
	refreshCount int
	now          func() time.Time

	// Fault injection. Every switch is off by default, so a Server built with
	// New() behaves like a healthy platform and the switches only ever appear in
	// the test that needs one - a platform broken by default would make every
	// other test depend on the fault it is not testing.
	bootstrapCount   int
	completionCount  int     // turns the stand-in gateway served (see handleCompletions)
	lastEffort       *string // reasoning_effort as received: nil means the field was ABSENT, "" means present and empty
	bootstrapStatus  int     // non-zero: bootstrap fails with this status and code
	bootstrapCode    string  // the code that failure carries
	logoutStatus     int     // non-zero: logout fails with this status and code
	logoutCode       string  // the code that failure carries
	ledgerStatus     int     // non-zero: the ledger read fails with this status and code
	ledgerCode       string  // the code that failure carries
	ledgerBalance    *int64  // nil means fixtureBalanceMicroCredits
	ledgerNoBalance  bool    // answer without balanceMicroCredits at all (a malformed answer)
	ledgerCount      int     // ledger reads served, so "one per trigger" is checkable
	tamperPolicy     bool    // sign correctly, then change a byte of the payload
	omitPolicy       bool    // answer bootstrap with no envelope at all
	catalogVersion   string  // "" means FixturePolicyVersion
	policyAudience   string  // "" means FixturePolicyAudience
	catalogTTLSec    int     // 0 means FixtureCatalogTTLSec
	ineligibleModel  string  // "" means every fixture model is offered
	refuseSessionsAt bool    // hand out tokens the server will not recognise

	// The gateway's own refusal (L-C4c). Zero means the stand-in serves turns the
	// way a platform with credit does; non-zero makes it answer like the control
	// plane does when there is none: a flat {"code":…} envelope and no provider
	// call. Deliberately NOT a per-route flag on the control-plane side: the 402
	// belongs to the turn endpoint, and the client's whole point is that it does not
	// predict it locally (PQ8).
	completionStatus int
	completionCode   string

	catalogRefreshCount int  // refresh endpoint hits, so "exactly once" is checkable
	unchangedInBody     bool // spell "nothing new" as {"unchanged":true} instead of 304
	dictionaryVersion   string
	dictionaryWords     []string
	dictionaryKeyID     string
	dictionaryDigest    string
	tamperDictionary    bool
	dictionaryStatus    int
	dictionaryCode      string
	dictionaryCount     int

	// Tool-call injection (PR-5b2). Empty name means the stand-in never asks
	// for a tool, which is the default and the healthy shape.
	//
	// WHY THIS ONE IS BEHIND A SWITCH WHILE THE REASONING TRACE IS NOT. The
	// trace is passive data: whether the user sees it is the client's decision
	// (show_reasoning), so gating it here as well would make "the client dropped
	// it" and "it was never sent" indistinguishable. A tool call is a request
	// for action - it changes the shape of the turn and raises a local
	// permission prompt - so emitting one by default would put an approval
	// dialog in front of every other hand walkthrough and would alter the reply
	// text that several existing nails assert on.
	toolCallName string // "" = off
	toolCallArgs string
	toolCallID   string
	emittedCalls int

	// What the last completions request carried, so a test can assert on what
	// arrived rather than on what it hoped was sent.
	lastToolNames []string
	lastToolCalls []toolResultSeen
}

type feedbackRecord struct {
	category string
	content  string
	receipt  productclient.FeedbackData
}

// toolResultSeen is one role:"tool" message as the client sent it back: the
// call it answers, and the text of the result.
type toolResultSeen struct {
	CallID  string
	Content string
}

// LastReasoningEffort reports the reasoning_effort the stand-in last received,
// and whether the field was there at all.
//
// The two-value answer is the point: PQ27 requires an absent field to be legal
// (the UI's "off" level), and a client that sent a default instead of nothing
// would be buying reasoning the user did not ask for. Decoding into a plain
// string cannot tell those apart, which is why the field is a pointer.
func (s *Server) LastReasoningEffort() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastEffort == nil {
		return "", false
	}
	return *s.lastEffort, true
}

// RefuseSessionsAfterLogin makes the stand-in issue tokens it does not record,
// so the very next authorised call is refused and the refresh that follows is
// refused too.
//
// This is what a revoked session looks like from the client's side, and it is
// the only way to reach L-A6 end to end: a session that dies *between* the login
// and the first authorised call. Without it the expiry path can only be reached
// by deleting credential.json mid-test, which tests the deletion rather than the
// server's refusal.
func (s *Server) RefuseSessionsAfterLogin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refuseSessionsAt = true
}

// RevokeSessions makes the stand-in forget every token it has already issued, so
// the next authorised call is refused and the refresh that follows is refused
// too.
//
// WHY THIS IS NOT RefuseSessionsAfterLogin. That one makes the platform
// distrust tokens from that moment on; this one makes it forget tokens it
// already handed out. They are different facts and they reach different code:
// the first is only reachable at a login boundary, the second is reachable while
// a window sits open - which is when a real session dies (another device signed
// in, an operator revoked it, the account was removed). The balance refresh is
// the call that happens at that moment, so a nail that cannot revoke a live
// session cannot reach the branch at all.
func (s *Server) RevokeSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.access = map[string]string{}
	s.refresh = map[string]string{}
}

// FailBootstrap makes every bootstrap attempt fail with the given status and
// business code, which is how the transport and upstream failure tiers are
// produced (本地API契约 §2.13 的四档失败面).
//
// THE GATE RULE, WHICH ALL FOUR Fail* SWITCHES SHARE (2026-09-15): a switch is
// armed when its STATUS is non-zero, and the code is optional metadata. The four
// used to disagree — this one, FailLogout and FailLedger tested the CODE, while
// FailCompletions tested the STATUS — so the same call meant two different
// things depending on the family member: FailCompletions(502, "") produced a
// 502, and FailBootstrap(502, "") was a silent no-op that left the fixture
// healthy. A hand walkthrough is exactly where that bites, because there is no
// compile error and no failing test to say the injection did not happen.
//
// STATUS IS THE RIGHT GATE. The contract's envelope always carries a code, so
// "code non-empty" reads as "the platform always sends one" — but that is not
// true of the tier the client already models: catalogTransport is a proxy's 502
// with no business code in it (需求基线 B4). Keying on the code made that case
// inexpressible: no call could produce a code-less refusal. Status is also the
// only field that means "there is a refusal at all"; a code without a status is
// not a response.
//
// No call site depended on the old rule: every one in the tree passes a non-zero
// status AND a non-empty code (the sole exception is FailLedger(0, "") in
// credits_test.go, an explicit reset that reads the same either way).
func (s *Server) FailBootstrap(status int, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bootstrapCount = 0 // start counting from the injection
	s.bootstrapStatus = status
	s.bootstrapCode = code
}

// FailLogout makes every logout attempt fail with the given status and business
// code.
//
// WHY IT EXISTS (V-54 / PR-2f). The client now revokes its session on the
// platform, and the interesting half of that is what happens when the revoke does
// NOT succeed: the user must still be signed out locally, and the answer must say
// the platform session was not revoked (PQ29 option 1). That branch cannot be
// reached with RefuseSessionsAfterLogin - that switch only affects tokens issued
// after it is armed, and arming it before the login makes the runtime's own
// post-login catalog fetch fail first (so the nail would be about a different
// failure). This is the same reasoning FailBootstrap is built on: an injection
// switch beside the code that models the failure, off unless a test asks.
func (s *Server) FailLogout(status int, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logoutStatus = status
	s.logoutCode = code
}

// FailCompletions makes every gateway call answer with the given status and code
// instead of a completion.
//
// WHY IT EXISTS (L-C4c / PR-5d3). The requirement is that a user out of credit sees
// one localized sentence, and the platform decides that with a 402 — the client is
// explicitly forbidden from predicting it (PQ8). Reaching that branch needs a platform
// that refuses a turn, and no other switch produces one: RefuseSessionsAfterLogin
// fails the authorisation call, which lands on the session-expiry path instead, and
// FailLedger moves a number the turn does not consult.
//
// WHY IT IS BEHIND A SWITCH. A gateway that always refuses is not a healthy fixture,
// so it is off unless a test asks — the same rule as every other knob here (纪律 4).
func (s *Server) FailCompletions(status int, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completionStatus = status
	s.completionCode = code
}

// SetBalance changes what the ledger reports (中台交付包 §5.5).
//
// It is a real state change, not a fault injection: the point of L-C4a is that
// the number on screen follows the server, and the only way to assert that is to
// move the server's number and watch. A client that computed anything locally
// would keep showing the old value, which is exactly the failure this switch
// exists to catch.
//
// It also moves the bootstrap fixture's balance, because the platform has one
// ledger and the stand-in should not have two: a test that changed one and read
// the other would be asserting on the stand-in's bookkeeping rather than on the
// client's.
func (s *Server) SetBalance(microCredits int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ledgerBalance = &microCredits
}

// Balance reports what the ledger is currently serving, so a test can state the
// expectation without repeating the fixture constant.
func (s *Server) Balance() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.balanceLocked()
}

func (s *Server) balanceLocked() int64 {
	if s.ledgerBalance != nil {
		return *s.ledgerBalance
	}
	return fixtureBalanceMicroCredits
}

// FailLedger makes every ledger read fail with the given status and business
// code, which is how the "keep the old value" branch is reached
// (本地API契约 §2.15: a failed read has no verdict in it).
func (s *Server) FailLedger(status int, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ledgerStatus = status
	s.ledgerCode = code
}

// OmitLedgerBalance answers with an otherwise successful ledger response that
// carries no balanceMicroCredits field.
//
// It models a platform that changed its mind about the field, or a proxy that
// stripped it - an answer this client cannot use. The distinction it protects is
// "absent" versus "zero": a client that decoded into an int64 would read this as
// 0 and wipe a real balance off the screen, telling the user they are out of
// credits without having spent anything (需求基线 E9 rule 2).
func (s *Server) OmitLedgerBalance() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ledgerNoBalance = true
}

// LedgerReads reports how many ledger reads the stand-in has served, so "the
// balance is refreshed once per trigger" can be asserted instead of assumed.
func (s *Server) LedgerReads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ledgerCount
}

// TamperPolicy changes one byte of the signed policy after signing it, leaving
// the signature over the original. It is an intermediary rewriting the payload,
// not a corrupt signature: the arrival is parseable, and only the signature says
// anything is wrong.
func (s *Server) TamperPolicy() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tamperPolicy = true
}

// SetDictionary changes the complete snapshot served by the signed dictionary
// endpoint. The input is copied so tests cannot mutate the fixture concurrently.
func (s *Server) SetDictionary(version string, words []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dictionaryVersion = version
	s.dictionaryWords = append([]string(nil), words...)
}

// TamperDictionary changes one signed payload byte after signing.
func (s *Server) TamperDictionary() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tamperDictionary = true
}

// SetDictionaryKeyID makes the next dictionary name a particular signing key.
func (s *Server) SetDictionaryKeyID(keyID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dictionaryKeyID = keyID
}

// SetDictionaryDigest overrides the signed final-snapshot checksum.
func (s *Server) SetDictionaryDigest(digest string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dictionaryDigest = digest
}

// FailDictionary makes the dictionary endpoint return an error envelope.
func (s *Server) FailDictionary(status int, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dictionaryStatus = status
	s.dictionaryCode = code
}

// DictionaryReads reports how many conditional dictionary requests arrived.
func (s *Server) DictionaryReads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dictionaryCount
}

// OmitPolicy answers bootstrap without a policy envelope, which is how the
// "no catalog in this build" degradation is produced without breaking the
// account half of the response.
func (s *Server) OmitPolicy() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.omitPolicy = true
}

// SetCatalogVersion changes the version the next fetch carries. Serving an older
// one after a newer has been cached is the downgrade attack (中台交付包 §4.3 末表).
func (s *Server) SetCatalogVersion(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalogVersion = version
}

// SetPolicyAudience addresses the next policy to another product's audience,
// which is what a policy for the wrong build looks like.
func (s *Server) SetPolicyAudience(audience string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policyAudience = audience
}

// SetCatalogTTL makes the fixture policy claim a freshness window of ttlSec
// seconds. It exists to reach the absurd end of the range: a platform that sends
// a value large enough to overflow the client's arithmetic must not make a
// perfectly good catalog read as long expired.
func (s *Server) SetCatalogTTL(ttlSec int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalogTTLSec = ttlSec
}

// SetModelIneligible marks one catalog model eligible=false, which is how a
// platform takes a model away: the entry is still IN the catalog, it is just no
// longer offered (L-C7 / PR-5e).
//
// WHY IT IS NOT SetModelAbsent. Removing the entry would produce a different
// situation with a different client answer: an absent model is one the catalog
// never mentions, while an ineligible one is present and withheld. The client
// projects both the same way (internal/productruntime projectCatalog keeps only
// eligible && transport == gateway), but only the second goes through the
// projection, so only the second asserts that the filter is what turns a
// withdrawal into the "pick another model" prompt.
//
// id == "" restores the healthy catalog. The ids are the fixture's own
// (`buding-*`), which are fixed ASCII data keys rather than brand copy
// (开发规范 §3.1 规则 2).
func (s *Server) SetModelIneligible(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ineligibleModel = id
}

// BootstrapCount reports how many bootstrap attempts the stand-in has seen,
// including the ones it failed. Tests assert on this to prove that a refusal did
// not quietly turn into a second fetch (or that a cache read did not turn into
// one at all).
func (s *Server) BootstrapCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bootstrapCount
}

// CatalogRefreshCount reports how many conditional-refresh attempts arrived.
// Unlike BootstrapCount it is not reset by a fault injection: the assertions it
// serves are "exactly one refresh happened" and "no refresh happened", and both
// need the count to survive the switch that caused them.
func (s *Server) CatalogRefreshCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.catalogRefreshCount
}

// SpellUnchangedInBody makes the refresh endpoint say "nothing new" with
// `{"unchanged": true}` instead of a bare 304.
//
// Both spellings are legal in the contract (中台交付包 §4.3) and the client has
// to recognise either, which is only testable if the fixture can produce either.
// A fixture with one spelling would let a client that only understood that one
// pass, and the contract explicitly lets the platform pick.
// SpellUnchangedInBody answers a conditional catalog refresh with
// {"data":{"unchanged":true}} instead of 304, which is the other spelling of
// "nothing new" the contract allows (中台交付包 §4.3).
func (s *Server) SpellUnchangedInBody() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unchangedInBody = true
}

// RequestToolCall makes the stand-in's gateway ask the client to run a tool:
// the model-side half of C10 (需求基线), and the only way L-C3c can be judged end
// to end. Off by default - see the field comment for why this is gated when the
// reasoning trace is not.
//
// The call is emitted on the STREAMING completions path only. That is the path
// the product uses (the gateway sender implements ToolStreamingSender, so the
// agent loop streams every turn); the non-streaming branch exists for curl and
// leaves this switch alone.
//
// It is emitted once per turn: as soon as the request carries a role:"tool"
// message, the stand-in answers normally, so the loop terminates instead of
// asking for the same tool forever.
func (s *Server) RequestToolCall(name, argsJSON string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolCallName = name
	s.toolCallArgs = argsJSON
	// A fixed id rather than a counter: nails assert that the result comes back
	// linked to the call, and a stable id makes that assertion readable. It is
	// the fixture's own label, not a guess at the platform's format.
	s.toolCallID = "call_standin_1"
}

// ToolCallCount reports how many tool-call deltas the stand-in has emitted, so
// "the switch is off" and "it asked once" are both checkable.
func (s *Server) ToolCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emittedCalls
}

// LastToolCallID is the id carried by the tool call the stand-in emitted.
func (s *Server) LastToolCallID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.toolCallID
}

// LastToolNames reports the tool names the last completions request
// advertised. A client that stopped offering its tools would leave the model
// unable to call one, and "no tool call happened" would then be
// indistinguishable from "none was ever possible".
func (s *Server) LastToolNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lastToolNames...)
}

// LastToolResults reports the role:"tool" messages the last request carried,
// keyed by the call they answer (tool_call_id).
func (s *Server) LastToolResults() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.lastToolCalls))
	for _, r := range s.lastToolCalls {
		out[r.CallID] = r.Content
	}
	return out
}

// New builds a stand-in seeded with the fixture codes above.
func New(opts ...Option) *Server {
	s := &Server{
		codes: map[string]codeFixture{
			FixtureActivationCode:           {boxCode: FixtureBoxCode},
			FixtureSecondActivationCode:     {boxCode: FixtureBoxCode},
			FixtureBoundPhoneActivationCode: {boxCode: FixtureOtherBoxCode, phone: FixtureBoundPhone},
		},
		used:     map[string]string{},
		accounts: map[string]*accountState{},
		smsSent:  map[string]string{},
		access:   map[string]string{},
		refresh:  map[string]string{},
		feedback: map[string]feedbackRecord{},
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Option customises a Server.
type Option func(*Server)

// WithClock swaps the clock used to stamp activation records.
func WithClock(now func() time.Time) Option {
	return func(s *Server) { s.now = now }
}

// Handler returns the platform-facing routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+"/v1/auth/sms/send", s.handleSendSMS)
	mux.HandleFunc("POST "+"/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST "+"/v1/auth/refresh", s.handleRefresh)
	// The revoke route (中台交付包 §4.1 第 4 条). It was missing until V-54: the
	// contract had carried `/v1/auth/logout` from the start, but no client path
	// called it and the stand-in did not serve it, so "logout revokes the session"
	// was unverifiable from both ends. Serving it is not an invention - the
	// endpoint is the platform's (开发规范 §6.4.6 discipline 4), and the stand-in's
	// job is to mirror what the platform will do.
	mux.HandleFunc("POST "+"/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET "+"/v1/client/bootstrap", s.handleBootstrap)
	mux.HandleFunc("GET "+"/v1/catalog/models", s.handleCatalogModels)
	mux.HandleFunc("GET "+"/v1/dictionaries/sensitive", s.handleSensitiveDictionary)
	// The ledger read (中台交付包 §4.1 第 9 条). It was missing until L-C4a
	// because the balance had no reader at all: no code path called the endpoint,
	// and the projection it feeds was written only by the login handler handing
	// back the value it had just read off disk (V-28).
	mux.HandleFunc("GET "+"/v1/credits/ledger", s.handleCreditsLedger)
	mux.HandleFunc("POST "+"/v1/feedback", s.handleFeedback)
	// The built-in gateway half (需求基线 C1). It lives on the same stand-in as
	// the control plane because the developer profile points both hosts at this
	// one process (internal/productprofile/profiles/developer.json), and a
	// manual walkthrough needs the same single process to answer the turn it
	// just authorized. Before PR-5a this route did not exist, so the desktop
	// build could be signed in and still have nothing to talk to.
	mux.HandleFunc("POST "+"/v1/chat/completions", s.handleCompletions)
	return mux
}

// CompletionCount reports how many turns the stand-in gateway served. It counts
// attempts, including refused ones, so a test can tell "the turn was refused
// before dialing" from "the turn dialed and was refused here" - which is the
// distinction 需求基线 B4 rests on.
func (s *Server) CompletionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completionCount
}

// RefreshCount reports how many refresh calls the stand-in served. A test uses
// it to prove concurrent rejections collapse into one refresh rather than a
// storm (需求基线 C2 规则 4).
func (s *Server) RefreshCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshCount
}

// ExpireAccessTokens rejects every access token currently issued, leaving the
// refresh tokens valid. It is how a test produces the "access token expired,
// refresh works" path.
func (s *Server) ExpireAccessTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.access = map[string]string{}
}

// ClearBoxCode removes the box code from an account's activation record,
// standing in for a record written before the box code existed. The client must
// surface it as absent, not fail (需求基线 E5).
func (s *Server) ClearBoxCode(phone string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if canonical, ok := productphone.Normalize(phone); ok {
		phone = canonical
	}
	if acct := s.accounts[phone]; acct != nil {
		acct.activation.BoxCode = ""
	}
}

func (s *Server) handleSendSMS(w http.ResponseWriter, r *http.Request) {
	var req productclient.SendSMSRequest
	if !decode(w, r, &req) {
		return
	}
	phone, ok := productphone.Normalize(req.Phone)
	if !ok {
		writeError(w, http.StatusBadRequest, productclient.CodeInvalidPhone, "phone")
		return
	}
	s.mu.Lock()
	s.smsSent[phone] = FixtureSMSCode
	s.mu.Unlock()
	writeData(w, http.StatusOK, productclient.SendSMSData{CooldownSec: 60, ExpiresInSec: 600})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req productclient.LoginRequest
	if !decode(w, r, &req) {
		return
	}
	phone, ok := productphone.Normalize(req.Phone)
	if !ok {
		writeError(w, http.StatusBadRequest, productclient.CodeInvalidPhone, "phone")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	req.Phone = phone
	sent, asked := s.smsSent[req.Phone]
	if !asked {
		writeError(w, http.StatusBadRequest, productclient.CodeCodeNotSent, "code")
		return
	}
	if req.Code != sent {
		writeError(w, http.StatusBadRequest, productclient.CodeInvalidCode, "code")
		return
	}

	acct := s.accounts[req.Phone]
	activating := req.ActivationCode != "" || req.BoxCode != ""

	switch {
	case activating:
		if code := s.checkActivation(req); code != "" {
			status := http.StatusBadRequest
			masked := ""
			if code == productclient.CodePhoneMismatch {
				status = http.StatusForbidden
				// The number the code was issued to, so the UI can name it. The
				// client has no way to know it: the directory may be brand new.
				masked = maskPhone(s.codes[req.ActivationCode].phone)
			}
			writeErrorMasked(w, status, code, activationField(code), masked)
			return
		}
	case acct == nil:
		// Nothing to sign in as, and no activation offered. 中台交付包 §4.2 lists
		// activation_required among the codes a login must expect, and this is that
		// case: the phone holds no usable authorization. It used to answer
		// activation_invalid on the belief that the registry did not name this
		// case - it does, and the difference is user-visible: the client renders
		// "go activate" for this code instead of pointing at a field of an
		// activation form it is not even showing (V-45).
		//
		// No field: the answer is about the account, not about one of the two
		// credentials, which is why 本地API契约 §3 keeps it business-level.
		writeError(w, http.StatusForbidden, productclient.CodeActivationRequired, "")
		return
	}

	if acct == nil {
		acct = s.createAccount(req)
	}

	access := s.issueToken("at", req.Phone)
	refresh := s.issueToken("rt", req.Phone)

	activation := acct.activation
	writeData(w, http.StatusOK, productclient.LoginData{
		AccessToken:             access,
		RefreshToken:            refresh,
		AccessTokenExpiresInSec: fixtureAccessTokenTTL,
		Account: productclient.Account{
			ID: acct.id, PhoneMasked: acct.phoneMasked, Nickname: acct.nickname,
		},
		Activation: &activation,
	})
}

// checkActivation returns the error code for a bad activation attempt, or "".
// The three failures stay distinct on purpose: a user who mistypes one digit
// must be told which digit class is wrong (需求基线 E1 规则 2).
func (s *Server) checkActivation(req productclient.LoginRequest) string {
	fixture, known := s.codes[req.ActivationCode]
	if !known {
		return productclient.CodeActivationInvalid
	}
	if _, taken := s.used[req.ActivationCode]; taken {
		return productclient.CodeActivationCodeUsed
	}
	if fixture.phone != "" && fixture.phone != req.Phone {
		return productclient.CodePhoneMismatch
	}
	if !s.knownBoxCode(req.BoxCode) {
		return productclient.CodeBoxCodeUnknown
	}
	if fixture.boxCode != req.BoxCode {
		return productclient.CodeBoxCodeMismatch
	}
	return ""
}

func (s *Server) knownBoxCode(boxCode string) bool {
	for _, fixture := range s.codes {
		if fixture.boxCode == boxCode {
			return true
		}
	}
	return false
}

func activationField(code string) string {
	switch code {
	case productclient.CodeBoxCodeUnknown, productclient.CodeBoxCodeMismatch:
		return "boxCode"
	default:
		return "activationCode"
	}
}

func (s *Server) createAccount(req productclient.LoginRequest) *accountState {
	s.seq++
	nickname := req.Nickname
	if nickname == "" {
		nickname = maskPhone(req.Phone)
	}
	activatedAt := s.now().UTC()
	acct := &accountState{
		id:          fmt.Sprintf("acct_%03d", s.seq),
		phone:       req.Phone,
		phoneMasked: maskPhone(req.Phone),
		nickname:    nickname,
		activation: productclient.Activation{
			Status:      "active",
			ActivatedAt: activatedAt.Format(time.RFC3339),
			ExpiresAt:   activatedAt.AddDate(1, 0, 0).Format(time.RFC3339),
			BoxCode:     s.codes[req.ActivationCode].boxCode,
		},
	}
	s.accounts[req.Phone] = acct
	s.used[req.ActivationCode] = acct.id
	return acct
}

// Authorised reports whether the request carries an access token this stand-in
// issued.
//
// WHY AN ACCESSOR. The hand-run process (cmd/productstub) gained an optional
// upstream that forwards a model turn to a real provider, and it must refuse an
// unauthenticated turn before dialling anything - otherwise the process is an
// open proxy on loopback that spends the operator's credits. The token table is
// the fixture's, so the predicate belongs here rather than being re-derived
// from the bearer format in the dev tool. It exposes an existing fact; it adds
// no platform behaviour.
func (s *Server) Authorised(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.access[bearer(r)]
	return ok
}

func (s *Server) issueToken(prefix, phone string) string {
	s.seq++
	token := fmt.Sprintf("%s_%d", prefix, s.seq)
	if s.refuseSessionsAt {
		// Handed out but not recorded: the client gets a token that looks valid
		// and every authorised call - including the refresh - is refused. See
		// Server.RefuseSessionsAfterLogin.
		return token
	}
	if prefix == "at" {
		s.access[token] = phone
	} else {
		s.refresh[token] = phone
	}
	return token
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req productclient.RefreshRequest
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.refreshCount++
	phone, ok := s.refresh[req.RefreshToken]
	if !ok {
		writeError(w, http.StatusUnauthorized, productclient.CodeUnauthorized, "")
		return
	}
	// Rotation is mandatory: the old refresh token stops working here.
	delete(s.refresh, req.RefreshToken)
	writeData(w, http.StatusOK, productclient.RefreshData{
		AccessToken:             s.issueToken("at", phone),
		RefreshToken:            s.issueToken("rt", phone),
		AccessTokenExpiresInSec: fixtureAccessTokenTTL,
	})
}

// handleLogout revokes the session behind the presented bearer.
//
// The contract says "revoke the current refresh token / session, without
// deleting the activation binding, the ledger, or data that has to be kept"
// (中台交付包 §4.1 #4). Both halves are modelled here and they are different
// acts: the ACCESS token stops being accepted (the session is over), and every
// refresh token issued to the same phone stops buying a new one (the copy that
// carries the credential file can no longer resurrect this session - E1 rule 7).
//
// Revoking the refresh token rather than only the presented access token is the
// point of the whole endpoint: the threat is a copied data/ directory, and until
// V-54 the copy outlived the logout because nothing told the platform.
//
// The activation record, the bound number and the accounts map are deliberately
// untouched. Logging out is not un-activating (需求基线 E7), and the stand-in
// keeps that distinction visible because the client's second-login form depends
// on it.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)

	s.mu.Lock()
	defer s.mu.Unlock()

	// ARMED-BY-STATUS: see the gate rule on FailBootstrap.
	if s.logoutStatus != 0 {
		writeError(w, s.logoutStatus, s.logoutCode, "")
		return
	}
	phone, ok := s.access[token]
	if !ok {
		writeError(w, http.StatusUnauthorized, productclient.CodeUnauthorized, "")
		return
	}
	// EVERY token of the session, not just the one presented. The contract says
	// "revoke the current refresh token / SESSION" (中台交付包 §4.1 #4), and the
	// distinction is load-bearing: a beaten path that removed only the presented
	// access token leaves the older access tokens of the same session accepted
	// until they expire, so a copy that captured an earlier pair keeps working and
	// "logged out" is not true yet. This nail found exactly that while it was being
	// written (the copy signed in again after a lapse-driven logout).
	for accessToken, owner := range s.access {
		if owner == phone {
			delete(s.access, accessToken)
		}
	}
	for refreshToken, owner := range s.refresh {
		if owner == phone {
			delete(s.refresh, refreshToken)
		}
	}
	// `data: null` and nothing else. 中台交付包 §4.1 #4 specifies no response body
	// for this endpoint ("revoke the current refresh token / session"), and the
	// client reads only the status - so the stand-in must not invent a field here.
	// An earlier draft returned `{"revoked": true}`, which would have made the
	// stand-in a second authority for a fact the contract does not define
	// (开发规范 §6.4.6 discipline 4: the stand-in only does what the contract can
	// state, and an invention drifts the two apart silently).
	writeData(w, http.StatusOK, nil)
}

// handleFeedback mirrors the small protected contract used by the desktop
// feedback form. It holds only enough in-memory data to prove idempotency.
func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	var req productclient.FeedbackRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Category != "bug" && req.Category != "suggestion" && req.Category != "other" || strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, productclient.CodeInvalidRequest, "")
		return
	}
	key := r.Header.Get(productclient.HeaderIdempotencyKey)
	if key == "" {
		writeError(w, http.StatusBadRequest, productclient.CodeInvalidRequest, "")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	phone, ok := s.access[bearer(r)]
	if !ok {
		writeError(w, http.StatusUnauthorized, productclient.CodeUnauthorized, "")
		return
	}
	compoundKey := phone + "\x00" + key
	if existing, ok := s.feedback[compoundKey]; ok {
		if existing.category != req.Category || existing.content != strings.TrimSpace(req.Content) {
			writeError(w, http.StatusConflict, productclient.CodeIdempotencyConflict, "")
			return
		}
		writeData(w, http.StatusOK, existing.receipt)
		return
	}
	s.seq++
	receipt := productclient.FeedbackData{
		FeedbackID: fmt.Sprintf("fb_%03d", s.seq),
		AcceptedAt: s.now().UTC().Format(time.RFC3339),
	}
	s.feedback[compoundKey] = feedbackRecord{category: req.Category, content: strings.TrimSpace(req.Content), receipt: receipt}
	writeData(w, http.StatusOK, receipt)
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bootstrapCount++
	// ARMED-BY-STATUS: see the gate rule on FailBootstrap.
	if s.bootstrapStatus != 0 {
		writeError(w, s.bootstrapStatus, s.bootstrapCode, "")
		return
	}

	phone, ok := s.access[token]
	if !ok {
		writeError(w, http.StatusUnauthorized, productclient.CodeUnauthorized, "")
		return
	}
	acct := s.accounts[phone]
	if acct == nil {
		writeError(w, http.StatusUnauthorized, productclient.CodeUnauthorized, "")
		return
	}
	activation := acct.activation
	// The same number the ledger serves: the platform has one ledger, and a
	// stand-in with two would let a test assert on its own bookkeeping instead of
	// on the client (see SetBalance).
	balance := s.balanceLocked()
	data := bootstrapResponse{
		BootstrapData: productclient.BootstrapData{
			Account: productclient.Account{
				ID: acct.id, PhoneMasked: acct.phoneMasked, Nickname: acct.nickname,
			},
			Activation: &activation,
			Credits:    productclient.Balance{BalanceMicroCredits: &balance},
		},
	}
	if !s.omitPolicy {
		envelope, err := s.policyEnvelope()
		if err != nil {
			writeError(w, http.StatusInternalServerError, productclient.CodeInternalError, "")
			return
		}
		data.PolicyEnvelope = envelope
	}
	writeData(w, http.StatusOK, data)
}

// handleCreditsLedger serves the balance read (中台交付包 §4.1 第 9 条, §5.5).
//
// The shape mirrors the platform: the balance sits inline in `data` beside
// entries[]. A realistic entry is always included even though this client models
// none, so "an unread field does not disturb the read" is exercised by every
// call rather than by a test that has to remember to ask for it.
//
// The failure switches sit beside the code that models them and are off by
// default, like FailBootstrap and FailLogout: a stand-in broken by default would
// make every other test depend on the fault it is not testing.
func (s *Server) handleCreditsLedger(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ledgerCount++
	// ARMED-BY-STATUS: see the gate rule on FailBootstrap.
	if s.ledgerStatus != 0 {
		writeError(w, s.ledgerStatus, s.ledgerCode, "")
		return
	}
	if _, ok := s.access[token]; !ok {
		writeError(w, http.StatusUnauthorized, productclient.CodeUnauthorized, "")
		return
	}

	entries := []any{map[string]any{
		"ledgerId":           "led_standin_1",
		"clientRequestId":    "48ed8953-73a3-4e5e-b4a2-0dc7b0ea0360",
		"modelId":            "standin-model",
		"pricingVersion":     "2026-09-a",
		"state":              "settled",
		"chargeMicroCredits": 100,
		"createdAt":          "2026-09-10T08:00:00Z",
	}}
	if s.ledgerNoBalance {
		// A successful envelope with the one field the client needs left out. It
		// cannot be written with the typed DTO, because carrying that field is the
		// DTO's whole job.
		writeData(w, http.StatusOK, map[string]any{"entries": entries})
		return
	}
	writeData(w, http.StatusOK, map[string]any{
		"balanceMicroCredits": s.balanceLocked(),
		"entries":             entries,
	})
}

// handleCatalogModels serves the conditional catalog refresh (中台交付包 §4.1 第 6
// 条, §4.3).
//
// It answers "nothing new" in whichever spelling the test selected, because the
// contract lets the platform choose and the client owes both. A 304 has no body
// at all, which is why the count and the branch both live here rather than in a
// shared helper: the two spellings differ in exactly this one place.
func (s *Server) handleCatalogModels(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.catalogRefreshCount++
	if _, ok := s.access[bearer(r)]; !ok {
		writeError(w, http.StatusUnauthorized, productclient.CodeUnauthorized, "")
		return
	}
	if known := r.URL.Query().Get("knownVersion"); known != "" && known == s.version() {
		if s.unchangedInBody {
			writeData(w, http.StatusOK, map[string]any{"unchanged": true})
			return
		}
		w.WriteHeader(http.StatusNotModified)
		return
	}
	envelope, err := s.policyEnvelope()
	if err != nil {
		writeError(w, http.StatusInternalServerError, productclient.CodeInternalError, "")
		return
	}
	// The same flattened shape bootstrap uses, so a signed catalog has one
	// spelling on the wire (中台交付包 §4.3).
	writeData(w, http.StatusOK, struct {
		productclient.PolicyEnvelope
		Unchanged bool `json:"unchanged"`
	}{PolicyEnvelope: envelope})
}

func (s *Server) handleSensitiveDictionary(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dictionaryCount++
	if s.dictionaryStatus != 0 {
		writeError(w, s.dictionaryStatus, s.dictionaryCode, "")
		return
	}
	if _, ok := s.access[bearer(r)]; !ok {
		writeError(w, http.StatusUnauthorized, productclient.CodeUnauthorized, "")
		return
	}
	version := s.dictionaryVersion
	if version == "" {
		version = FixtureDictionaryVersion
	}
	if r.URL.Query().Get("version") == version {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	words := s.dictionaryWords
	if words == nil {
		words = []string{"server-fixture-word"}
	}
	keyID := s.dictionaryKeyID
	if keyID == "" {
		keyID = FixtureSigningKeyID
	}
	envelope, err := signedDictionary(s.now(), version, keyID, words, s.dictionaryDigest, s.tamperDictionary)
	if err != nil {
		writeError(w, http.StatusInternalServerError, productclient.CodeInternalError, "")
		return
	}
	writeData(w, http.StatusOK, envelope)
}

// handleCompletions is the stand-in gateway: an OpenAI-protocol chat endpoint
// that streams, so the whole turn path can be walked by hand.
//
// WHY THE STAND-IN SERVES IT. The gateway is reached through the same host as
// the control plane in the developer profile, so "the gateway" and "the
// platform" are one process during development. Making the fixture answer turns
// is what turns PR-5a from a set of unit assertions into something a person can
// click: sign in, pick a model, send a message.
//
// WHY IT REQUIRES THE TOKEN. C2 keeps the access token in memory and rebuilds
// the sender from it each turn. A fixture that accepted anonymous turns would
// pass even if the sender never presented the token, so the fixture refuses -
// the same reason the control plane does. The token is validated against the
// live session table, so an expired or rotated-away token is refused here too.
//
// WHY THE REPLY NAMES THE MODEL. The session binds a selection to a composite
// id (`buding-gateway::buding-privacy-1`) while the gateway is handed the bare
// catalog id. That difference is invisible on screen unless something says it
// out loud, and it is exactly what PR-4c0 and PR-4d had to get right - so a
// hand test should be able to read it back rather than trust it.
func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			// Present on the role:"tool" message that carries a result back
			// (internal/provider/openai serialises it from the tool_result
			// block's ToolUseID).
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
		// The tool schemas the client advertised this turn. Decoded for the same
		// reason the request is recorded at all: a fixture that proves a model
		// asked for a tool, without proving the client offered one, cannot tell
		// "the tool loop works" from "the client stopped sending its tools".
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		// A pointer so "absent" and "present but empty" stay distinguishable: PQ27
		// makes the absent form the one an "off" level produces, and a client that
		// sent a default instead would be indistinguishable without this.
		//
		// Unknown fields are ignored rather than rejected on purpose - the same
		// requirement the contract now puts on the real gateway (§5.2), because the
		// client reuses the generic OpenAI stack and its fields change over time.
		ReasoningEffort *string `json:"reasoning_effort"`
	}
	// Decoded before the token check so a malformed body is reported as such
	// rather than as an auth failure: a hand test chasing the wrong error is
	// worse than a slightly lenient order.
	if !decode(w, r, &req) {
		return
	}

	names := make([]string, 0, len(req.Tools))
	for _, t := range req.Tools {
		names = append(names, t.Function.Name)
	}
	var results []toolResultSeen
	returned := false
	for _, m := range req.Messages {
		if m.Role != "tool" {
			continue
		}
		returned = true
		results = append(results, toolResultSeen{CallID: m.ToolCallID, Content: m.Content})
	}

	s.mu.Lock()
	s.completionCount++
	s.lastEffort = req.ReasoningEffort
	s.lastToolNames = names
	s.lastToolCalls = results
	// One call per turn, and no repeat: once a tool result is in the request the
	// loop has run the tool, so the stand-in answers normally instead of asking
	// for the same tool forever (which would spin the loop to its turn cap).
	askForTool := s.toolCallName != "" && !returned
	toolName, toolArgs, toolID := s.toolCallName, s.toolCallArgs, s.toolCallID
	_, authorised := s.access[bearer(r)]
	refuseStatus, refuseCode := s.completionStatus, s.completionCode
	s.mu.Unlock()
	if !authorised {
		writeError(w, http.StatusUnauthorized, productclient.CodeUnauthorized, "")
		return
	}
	// After the authorisation check, because that is the platform's own order
	// (§5.4: 资格/余额/模型校验 come after the request is authenticated) — and before
	// any output, so a refused turn is a plain envelope rather than a stream that dies
	// halfway.
	if refuseStatus != 0 {
		writeError(w, refuseStatus, refuseCode, "")
		return
	}

	reply := fmt.Sprintf("[stand-in gateway] model=%s, %d message(s) received", req.Model, len(req.Messages))
	// A trace, and deliberately one that does NOT name the model: the reply above
	// is asserted to contain the bare catalog id by several nails, so a trace
	// that carried the same text would let "the trace leaked into the content
	// channel" pass unnoticed.
	//
	// Emitted unconditionally rather than behind a switch: reasoning is normal
	// platform behaviour, not a fault, and the client-side gate (show_reasoning)
	// is what decides whether the user sees it. Gating it here as well would make
	// the two indistinguishable - a build that dropped the trace and a build that
	// was never sent one would look the same.
	const trace = "[stand-in gateway] weighing the request"
	if !req.Stream {
		// The tool-call switch is deliberately ignored on this branch: the
		// product streams every turn (the gateway sender implements
		// ToolStreamingSender, so the agent loop never takes the buffered path),
		// and this branch exists so a person can curl the fixture. Answering a
		// curl with a tool call would ask for a tool nobody is there to run.
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-standin",
			"object":  "chat.completion",
			"created": s.now().Unix(),
			"model":   req.Model,
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": reply, "reasoning_content": trace},
				"finish_reason": "stop",
			}},
			"usage": standinUsage(),
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	chunk := func(delta map[string]any, finish any) {
		body, err := json.Marshal(map[string]any{
			"id":      "chatcmpl-standin",
			"object":  "chat.completion.chunk",
			"created": s.now().Unix(),
			"model":   req.Model,
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         delta,
				"finish_reason": finish,
			}},
		})
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", body)
		if flusher != nil {
			flusher.Flush()
		}
	}

	// The usage frame: an empty choices array, which is where an OpenAI-protocol
	// server puts it when stream_options.include_usage is set — and the branch
	// internal/provider/openai reads before it looks at any choice. Emitted on
	// every streamed turn, before [DONE], unconditionally; see standinUsage.
	usageChunk := func() {
		body, err := json.Marshal(map[string]any{
			"id":      "chatcmpl-standin",
			"object":  "chat.completion.chunk",
			"created": s.now().Unix(),
			"model":   req.Model,
			"choices": []any{},
			"usage":   standinUsage(),
		})
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", body)
		if flusher != nil {
			flusher.Flush()
		}
	}

	chunk(map[string]any{"role": "assistant", "content": ""}, nil)
	// The trace goes first, as a real reasoning model emits it, and in its own
	// delta: a gateway that merged it into the content would make the two blocks
	// indistinguishable at the only layer that can tell them apart (C10).
	chunk(map[string]any{"reasoning_content": trace}, nil)

	if askForTool {
		// The arguments are cut into three pieces on purpose. A real gateway
		// streams function arguments as JSON fragments that the client
		// concatenates by index (the pitfall CLAUDE.md records), and a
		// single-chunk call would leave that aggregation unexercised at the only
		// layer a person can watch it work.
		for i, part := range splitInThree(toolArgs) {
			call := map[string]any{"index": 0, "type": "function"}
			// id and name arrive with the first fragment, as OpenAI sends them.
			if i == 0 {
				call["id"] = toolID
				call["function"] = map[string]any{"name": toolName, "arguments": part}
			} else {
				call["function"] = map[string]any{"arguments": part}
			}
			chunk(map[string]any{"tool_calls": []any{call}}, nil)
		}
		// finish_reason is "tool_calls" here, not "stop": the OpenAI spelling the
		// provider adapter normalises to "tool_use" before the agent loop sees it.
		chunk(map[string]any{}, "tool_calls")
		usageChunk()
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		s.mu.Lock()
		s.emittedCalls++
		s.mu.Unlock()
		return
	}
	// Deliberately framed in several chunks: concatenating chunked content is
	// part of the provider's aggregator, and a single-chunk reply would leave
	// that path unexercised in the one place a person can see it working.
	const chunkRunes = 8
	runes := []rune(reply)
	for i := 0; i < len(runes); i += chunkRunes {
		end := i + chunkRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunk(map[string]any{"content": string(runes[i:end])}, nil)
	}
	chunk(map[string]any{}, "stop")
	usageChunk()
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// usageChunk writes the usage frame the way an OpenAI-protocol server does when
// the client asked for stream_options.include_usage: one frame with an EMPTY
// choices array, after the finish_reason and before [DONE].
//
// The shape matters to the client as much as the numbers do.
// internal/provider/openai reads chunk-level usage before it checks whether there
// are any choices at all, so an empty choices array is the branch this
// exercises — and it is the branch a real gateway takes, which is why emitting
// usage attached to a content chunk instead would leave the production path
// unexercised at the one layer a person can watch.
//
// Emitted unconditionally, without reading stream_options back: missing usage is
// the failure this fixture exists to remove (the turn summary read "0 tokens"),
// and a fixture that only emitted it when asked would reproduce the condition it
// was built to catch.
func standinUsage() map[string]any {
	return map[string]any{
		"prompt_tokens":     StandinPromptTokens,
		"completion_tokens": StandinCompletionTokens,
		"total_tokens":      StandinPromptTokens + StandinCompletionTokens,
	}
}

// splitInThree cuts a string into three pieces, the way a real gateway streams
// function arguments as JSON fragments. Rune-wise so each piece is valid UTF-8
// on the wire even though the client concatenates the fragments before parsing
// them - a mid-rune split would be legal for the client but would make a
// packet-level recording of the fixture look corrupt for no reason.
func splitInThree(s string) []string {
	if s == "" {
		return []string{""}
	}
	r := []rune(s)
	n := (len(r) + 2) / 3
	var out []string
	for i := 0; i < len(r); i += n {
		end := i + n
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[i:end]))
	}
	return out
}

// policyEnvelope signs the fixture policy with the current fault switches
// applied. The caller holds s.mu.
func (s *Server) policyEnvelope() (productclient.PolicyEnvelope, error) {
	audience := s.policyAudience
	if audience == "" {
		audience = FixturePolicyAudience
	}
	return signedPolicyFor(s.now(), s.version(), audience, s.tamperPolicy, s.catalogTTLSec, s.ineligibleModel)
}

// Policy returns the envelope the next answer will carry, with every armed
// switch applied.
//
// WHY AN ACCESSOR RATHER THAN A HANDLER CALL. The switch it was added for is
// SetModelIneligible, and the only other way to read the result is a signed-in
// bootstrap — which signs in, mints a catalog and then needs the very model list
// under test to check anything. This answers the same question directly, and it
// matches the accessors the rest of this package already exposes for exactly
// this reason (Balance, LedgerReads, BootstrapCount).
func (s *Server) Policy() (productclient.PolicyEnvelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policyEnvelope()
}

// version is the catalog version the next answer carries. The caller holds s.mu.
//
// It exists as its own method because two callers need to agree on it — the
// envelope's contents and the conditional comparison in handleCatalogModels — and
// a second copy of the default is how a conditional request starts comparing
// against a version the platform never signed.
func (s *Server) version() string {
	if s.catalogVersion != "" {
		return s.catalogVersion
	}
	return FixturePolicyVersion
}

// bootstrapResponse is the account summary plus the signed policy envelope, as
// one `data` object (中台交付包 §4.3). Both embedded structs carry JSON tags, so
// their fields are flattened side by side.
type bootstrapResponse struct {
	productclient.BootstrapData
	productclient.PolicyEnvelope
}

func bearer(r *http.Request) string {
	const prefix = "Bearer "
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	return strings.TrimPrefix(value, prefix)
}

func maskPhone(phone string) string {
	if len(phone) < 7 {
		return phone
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, productclient.CodeInvalidRequest, "")
		return false
	}
	return true
}

func writeData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	// The signed policy travels in `data`, and the signature covers the exact
	// byte sequence the platform serialised. encoding/json escapes `&`, `<` and
	// `>` by default, which rewrites those bytes and breaks the signature for
	// any policy that contains one - a model named "Tools & Agents" is enough.
	// See 中台交付包 §4.3: serialise once, sign those bytes, embed those bytes.
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{
		"data":       data,
		"requestId":  "req_standin",
		"serverTime": time.Now().UTC().Format(time.RFC3339),
	})
}

// writeErrorMasked is writeError plus the optional masked phone number that
// phone_mismatch carries (本地API契约 §2.3).
func writeErrorMasked(w http.ResponseWriter, status int, code, field, phoneMasked string) {
	body := map[string]any{"code": code}
	if field != "" {
		body["field"] = field
	}
	if phoneMasked != "" {
		body["phoneMasked"] = phoneMasked
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, field string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	body := map[string]any{"code": code}
	if field != "" {
		body["field"] = field
	}
	_ = json.NewEncoder(w).Encode(body)
}
