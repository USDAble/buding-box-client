package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/open-octo/octo-agent/internal/productstate"
)

// Product login: send-code, login, and activation handlers. All three are
// exempt from the product gate (they ARE the login flow); they sit behind the
// machine gate via s.api so a remote peer still needs the access key.
//
// The verification code is fake (需求 §5.3.5): fixed at 123456, valid 10
// minutes, resendable after a 60s cooldown. It lives in memory only — a
// process restart clears it, and the user just taps "send" again.

const (
	// fixedCode is the fake verification code. Deliberately a server constant,
	// never sent to the frontend (需求 §5.3.1: don't print the answer in the
	// input).
	fixedCode = "123456"

	// activationCode is the one-time activation code. ASCII identifier per
	// 需求 §5.2.4「标识符例外」 — a fixed data key that does NOT follow the
	// English brand name, so it must never be rewritten to a brand literal
	// (e.g. PUDDING-*) when the product is renamed. See
	// dev-docs-usdable/开发规范.md §3.2.
	activationCode = "BUDING-DEMO-0001"

	codeTTL      = 10 * time.Minute
	codeCooldown = 60 * time.Second

	// initialCredits is the fake points balance seeded on first activation
	// (需求 §5.4.4: 1280 balance, 0 used this month). P6 later deducts from it.
	initialCredits = 1280
)

// loginCodeSession is the in-memory verification-code record keyed by the
// normalized phone. sentAt bounds validity (10 min); cooldown blocks a resend
// until it passes.
type loginCodeSession struct {
	phone    string
	sentAt   time.Time
	cooldown time.Time
}

// loginRequest is the shared body for POST /api/product/login. activationCode
// is required only on first activation and ignored on second login.
type loginRequest struct {
	Phone          string `json:"phone"`
	Code           string `json:"code"`
	Nickname       string `json:"nickname"`
	ActivationCode string `json:"activationCode"`
}

// handleProductSendCode accepts a phone, normalizes it, and registers a code
// session (subject to the resend cooldown). Exempt from the product gate.
func (s *Server) handleProductSendCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}
	phone, ok := productstate.NormalizePhone(req.Phone)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"field": "phone", "code": "invalid_phone"})
		return
	}

	s.loginCodesMu.Lock()
	now := time.Now()
	if cur, exists := s.loginCodes[phone]; exists && now.Before(cur.cooldown) {
		remaining := cur.cooldown.Sub(now)
		s.loginCodesMu.Unlock()
		writeJSON(w, http.StatusTooManyRequests, map[string]int{"retryAfterSec": int(remaining.Seconds()) + 1})
		return
	}
	s.loginCodes[phone] = &loginCodeSession{phone: phone, sentAt: now, cooldown: now.Add(codeCooldown)}
	s.loginCodesMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]int{"cooldownSec": int(codeCooldown.Seconds())})
}

// handleProductLogin runs the two-round login validation (需求 §5.3.3): round
// one collects every format error and returns them all at once; round two
// short-circuits on the first business failure. On success it records the
// activation (first login) and the account, then returns the de-identified
// state for the frontend to route on.
func (s *Server) handleProductLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}

	// Round one — format. Every field is checked independently so the frontend
	// can mark them all at once; a failure on one must not hide another.
	fieldErrors := map[string]string{}
	phone, phoneOK := productstate.NormalizePhone(req.Phone)
	if !phoneOK {
		fieldErrors["phone"] = "invalid_phone"
	}
	if !isSixDigits(req.Code) {
		fieldErrors["code"] = "invalid_code"
	}
	switch err := productstate.ValidateNickname(req.Nickname); {
	case err != nil:
		fieldErrors["nickname"] = "nickname_format"
	case productstate.Sensitive(req.Nickname):
		fieldErrors["nickname"] = "nickname_sensitive"
	}

	snap := s.productState.Snapshot()
	firstActivation := !snap.Activated()
	if firstActivation && strings.TrimSpace(req.ActivationCode) == "" {
		fieldErrors["activationCode"] = "invalid_activation"
	}

	if len(fieldErrors) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"fieldErrors": fieldErrors})
		return
	}

	// Round two — business, short-circuited in the specified order.
	if reason := s.checkCode(phone, req.Code); reason != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": reason})
		return
	}
	if firstActivation {
		if !strings.EqualFold(strings.TrimSpace(req.ActivationCode), activationCode) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "activation_invalid"})
			return
		}
	} else {
		// Second login must be the bound number (需求 §5.3.4); compare against
		// the stored plaintext, and echo only the masked form back.
		if snap.Account == nil || snap.Account.Phone != phone {
			masked := ""
			if snap.Account != nil {
				masked = snap.Account.PhoneMasked
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "phone_mismatch", "phoneMasked": masked})
			return
		}
	}

	token := newAccountToken()
	now := time.Now()
	err := s.productState.Mutate(func(st *productstate.State) error {
		if firstActivation {
			st.Activation = &productstate.Activation{
				Activated:   true,
				Code:        activationCode,
				ActivatedAt: now,
				ExpiresAt:   now.AddDate(1, 0, 0),
			}
			// Seed the fake points balance on first activation (需求 §5.4.4:
			// 1280 balance, 0 used). P6 owns the deduction rules.
			st.Credits = productstate.Credits{Balance: initialCredits}
			// The only plan this phase sells is the seeded trial; the account
			// panel maps the machine code "trial" to the display name (P5 §4.2).
			st.Plan = productstate.Plan{Name: "trial"}
		}
		st.Account = &productstate.Account{
			Phone:       phone,
			PhoneMasked: productstate.MaskPhone(phone),
			Nickname:    req.Nickname,
			Token:       token,
			LastLoginAt: now,
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"state": s.productState.Snapshot().Public()})
}

// checkCode validates the code session for phone, returning the machine-code
// reason ("" on success): code_not_sent when none was requested, code_invalid
// when the code mismatches the fixed code or has expired.
func (s *Server) checkCode(phone, code string) string {
	s.loginCodesMu.Lock()
	defer s.loginCodesMu.Unlock()
	sess, exists := s.loginCodes[phone]
	if !exists {
		return "code_not_sent"
	}
	if time.Since(sess.sentAt) > codeTTL || code != fixedCode {
		return "code_invalid"
	}
	return ""
}

// isSixDigits reports whether s is exactly six ASCII digits (the fake code's
// shape; the code itself is never compared here — checkCode handles that).
func isSixDigits(s string) bool {
	if len(s) != 6 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// newAccountToken mints the fake account token ("local-" + 8 random bytes in
// hex). It is server-side-only fake data (需求 §6): never sent to the frontend,
// no expiry, no real auth meaning.
func newAccountToken() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is essentially unheard of; keep the login usable.
		return fmt.Sprintf("local-%d", time.Now().UnixNano())
	}
	return "local-" + hex.EncodeToString(b)
}
