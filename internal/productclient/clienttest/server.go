// Package clienttest is a stand-in for the central platform: a real HTTP
// handler that speaks the same envelopes, error codes and types as
// productclient, so the account lifecycle can be exercised without a network.
//
// It replaces the platform boundary, NOT the local service boundary. The
// frontend's development backend (web/src/dev/devBackend.ts) replaces the
// latter; the two are not interchangeable.
//
// It is a library, not a command: callers wrap Handler with httptest. A runnable
// binary is deliberately not provided yet - it is only needed once the frontend
// has to be driven end to end, which is a later step.
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
	return mux
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
	writeData(w, http.StatusOK, productclient.BootstrapData{
		Account: productclient.Account{
			ID: acct.id, PhoneMasked: acct.phoneMasked, Nickname: acct.nickname,
		},
		Activation: &activation,
		Balance:    productclient.Balance{BalanceMicroCredits: fixtureBalanceMicroCredits},
	})
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
	_ = json.NewEncoder(w).Encode(map[string]any{
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
