// Package clienttest is a stand-in for the central platform: a real HTTP
// handler that speaks the same envelopes, error codes and types as
// productclient, so the account lifecycle can be exercised without a network.
//
// It replaces the platform boundary, NOT the local service boundary. The
// frontend's development backend (web/src/dev/devBackend.ts) replaces the
// latter; the two are not interchangeable.
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
)

// Fixture values. The BUDING-DEMO-* and BOX-DEMO-* strings are fixed ASCII data
// keys, not brand copy - they must match the frontend's development backend so
// the two layers describe the same test account (开发规范 §3.1 规则 2).
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
	smsSent  map[string]string // phone -> login code
	access   map[string]string // access token -> phone
	refresh  map[string]string // refresh token -> phone

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
	tamperPolicy     bool    // sign correctly, then change a byte of the payload
	omitPolicy       bool    // answer bootstrap with no envelope at all
	catalogVersion   string  // "" means FixturePolicyVersion
	policyAudience   string  // "" means FixturePolicyAudience
	catalogTTLSec    int     // 0 means FixtureCatalogTTLSec
	refuseSessionsAt bool    // hand out tokens the server will not recognise
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

// FailBootstrap makes every bootstrap attempt fail with the given status and
// business code, which is how the transport and upstream failure tiers are
// produced (本地API契约 §2.13 的四档失败面).
func (s *Server) FailBootstrap(status int, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bootstrapCount = 0 // start counting from the injection
	s.bootstrapStatus = status
	s.bootstrapCode = code
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

// BootstrapCount reports how many bootstrap attempts the stand-in has seen,
// including the ones it failed. Tests assert on this to prove that a refusal did
// not quietly turn into a second fetch (or that a cache read did not turn into
// one at all).
func (s *Server) BootstrapCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bootstrapCount
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
	mux.HandleFunc("GET "+"/v1/client/bootstrap", s.handleBootstrap)
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
	if acct := s.accounts[phone]; acct != nil {
		acct.activation.BoxCode = ""
	}
}

func (s *Server) handleSendSMS(w http.ResponseWriter, r *http.Request) {
	var req productclient.SendSMSRequest
	if !decode(w, r, &req) {
		return
	}
	if !validPhone(req.Phone) {
		writeError(w, http.StatusBadRequest, productclient.CodeInvalidPhone, "phone")
		return
	}
	s.mu.Lock()
	s.smsSent[req.Phone] = FixtureSMSCode
	s.mu.Unlock()
	writeData(w, http.StatusOK, productclient.SendSMSData{CooldownSec: 60, ExpiresInSec: 600})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req productclient.LoginRequest
	if !decode(w, r, &req) {
		return
	}
	if !validPhone(req.Phone) {
		writeError(w, http.StatusBadRequest, productclient.CodeInvalidPhone, "phone")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

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
		// Nothing to sign in as, and no activation offered. The registry does not
		// name this case, so it is reported as the activation failure it is.
		writeError(w, http.StatusBadRequest, productclient.CodeActivationInvalid, "activationCode")
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

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bootstrapCount++
	if s.bootstrapCode != "" {
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
	data := bootstrapResponse{
		BootstrapData: productclient.BootstrapData{
			Account: productclient.Account{
				ID: acct.id, PhoneMasked: acct.phoneMasked, Nickname: acct.nickname,
			},
			Activation: &activation,
			Balance:    productclient.Balance{BalanceMicroCredits: fixtureBalanceMicroCredits},
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
		} `json:"messages"`
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

	s.mu.Lock()
	s.completionCount++
	s.lastEffort = req.ReasoningEffort
	_, authorised := s.access[bearer(r)]
	s.mu.Unlock()
	if !authorised {
		writeError(w, http.StatusUnauthorized, productclient.CodeUnauthorized, "")
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

	chunk(map[string]any{"role": "assistant", "content": ""}, nil)
	// The trace goes first, as a real reasoning model emits it, and in its own
	// delta: a gateway that merged it into the content would make the two blocks
	// indistinguishable at the only layer that can tell them apart (C10).
	chunk(map[string]any{"reasoning_content": trace}, nil)
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
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// policyEnvelope signs the fixture policy with the current fault switches
// applied. The caller holds s.mu.
func (s *Server) policyEnvelope() (productclient.PolicyEnvelope, error) {
	version := s.catalogVersion
	if version == "" {
		version = FixturePolicyVersion
	}
	audience := s.policyAudience
	if audience == "" {
		audience = FixturePolicyAudience
	}
	return signedPolicyFor(s.now(), version, audience, s.tamperPolicy, s.catalogTTLSec)
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

// validPhone accepts the mainland mobile shape the registry's invalid_phone
// covers. It is deliberately shallow: the platform owns real validation.
func validPhone(phone string) bool {
	if len(phone) != 11 || phone[0] != '1' {
		return false
	}
	for _, r := range phone {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
