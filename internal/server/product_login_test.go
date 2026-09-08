package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/productstate"
)

func sendCodeReq(t *testing.T, srv *Server, phone string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/product/send-code", bytes.NewBufferString(`{"phone":`+jsonString(phone)+`}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)
	return w
}

func loginReq(t *testing.T, srv *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/product/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)
	return w
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q is not JSON: %v", w.Body.String(), err)
	}
	return m
}

func TestProductSendCodeInvalidPhone(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	w := sendCodeReq(t, srv, "23800001234")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if body := decodeBody(t, w); body["code"] != "invalid_phone" {
		t.Fatalf("code = %v, want invalid_phone", body["code"])
	}
}

func TestProductSendCodeCooldown(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	w := sendCodeReq(t, srv, "138 0000 1234")
	if w.Code != http.StatusOK {
		t.Fatalf("first send status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if body := decodeBody(t, w); int(body["cooldownSec"].(float64)) != 60 {
		t.Fatalf("cooldownSec = %v, want 60", body["cooldownSec"])
	}

	// Immediate resend hits the cooldown.
	w = sendCodeReq(t, srv, "13800001234")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("resend status = %d, want 429", w.Code)
	}
}

func TestProductLoginCodeNotSent(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	w := loginReq(t, srv, map[string]any{"phone": "13800001234", "code": "123456", "nickname": "用户1234", "activationCode": "BUDING-DEMO-0001"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if body := decodeBody(t, w); body["code"] != "code_not_sent" {
		t.Fatalf("code = %v, want code_not_sent", body["code"])
	}
}

func TestProductLoginCodeInvalid(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	sendCodeReq(t, srv, "13800001234")

	w := loginReq(t, srv, map[string]any{"phone": "13800001234", "code": "654321", "nickname": "用户1234", "activationCode": "BUDING-DEMO-0001"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if body := decodeBody(t, w); body["code"] != "code_invalid" {
		t.Fatalf("code = %v, want code_invalid", body["code"])
	}
}

func TestProductLoginCodeExpired(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	sendCodeReq(t, srv, "13800001234")

	// Age the session past the 10-minute TTL.
	srv.loginCodesMu.Lock()
	srv.loginCodes["13800001234"].sentAt = time.Now().Add(-11 * time.Minute)
	srv.loginCodesMu.Unlock()

	w := loginReq(t, srv, map[string]any{"phone": "13800001234", "code": "123456", "nickname": "用户1234", "activationCode": "BUDING-DEMO-0001"})
	if body := decodeBody(t, w); body["code"] != "code_invalid" {
		t.Fatalf("code = %v, want code_invalid", body["code"])
	}
}

func TestProductLoginFirstActivation(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	sendCodeReq(t, srv, "+86 138 0000 1234")

	w := loginReq(t, srv, map[string]any{"phone": "13800001234", "code": "123456", "nickname": "用户1234", "activationCode": " buding-demo-0001 "})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	state := body["state"].(map[string]any)
	if state["activated"] != true || state["loggedIn"] != true {
		t.Fatalf("state = %v, want activated+loggedIn", state)
	}

	snap := srv.productState.Snapshot()
	if !snap.Activated() || snap.Account == nil {
		t.Fatal("state must record activation and account")
	}
	if snap.Account.Phone != "13800001234" || snap.Account.PhoneMasked != "138****1234" {
		t.Fatalf("account phone = %q / %q, want normalized + masked", snap.Account.Phone, snap.Account.PhoneMasked)
	}
	if snap.Account.Token == "" {
		t.Fatal("account token must be minted")
	}
}

func TestProductLoginActivationCodeWrong(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	sendCodeReq(t, srv, "13800001234")

	// Inner spaces are NOT ignored — only case and leading/trailing space are.
	w := loginReq(t, srv, map[string]any{"phone": "13800001234", "code": "123456", "nickname": "用户1234", "activationCode": "BUDING DEMO 0001"})
	if body := decodeBody(t, w); body["code"] != "activation_invalid" {
		t.Fatalf("code = %v, want activation_invalid", body["code"])
	}
}

func TestProductLoginSecondLoginPhoneMismatch(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	// Activate + bind phone 13800001234.
	sendCodeReq(t, srv, "13800001234")
	if w := loginReq(t, srv, map[string]any{"phone": "13800001234", "code": "123456", "nickname": "用户1234", "activationCode": "BUDING-DEMO-0001"}); w.Code != http.StatusOK {
		t.Fatalf("first login status = %d (%s)", w.Code, w.Body.String())
	}

	// Second login (no activationCode) with a different number.
	sendCodeReq(t, srv, "13900009999")
	w := loginReq(t, srv, map[string]any{"phone": "13900009999", "code": "123456", "nickname": "用户1234"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	body := decodeBody(t, w)
	if body["code"] != "phone_mismatch" || body["phoneMasked"] != "138****1234" {
		t.Fatalf("body = %v, want phone_mismatch + masked", body)
	}
}

func TestProductLoginFormatErrorsAllReturned(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	// Every field wrong: phone, code, nickname, and a missing activationCode
	// (first activation). All four must be reported together.
	w := loginReq(t, srv, map[string]any{"phone": "123", "code": "12", "nickname": "a", "activationCode": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	body := decodeBody(t, w)
	fe, ok := body["fieldErrors"].(map[string]any)
	if !ok {
		t.Fatalf("fieldErrors = %v", body["fieldErrors"])
	}
	for _, field := range []string{"phone", "code", "nickname", "activationCode"} {
		if _, present := fe[field]; !present {
			t.Fatalf("fieldErrors missing %q: %v", field, fe)
		}
	}
}

func TestProductLoginNicknameSensitive(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	sendCodeReq(t, srv, "13800001234")

	orig := productstate.Sensitive
	defer func() { productstate.Sensitive = orig }()
	productstate.Sensitive = func(v string) bool { return v == "坏词坏词" }

	w := loginReq(t, srv, map[string]any{"phone": "13800001234", "code": "123456", "nickname": "坏词坏词", "activationCode": "BUDING-DEMO-0001"})
	body := decodeBody(t, w)
	fe := body["fieldErrors"].(map[string]any)
	if fe["nickname"] != "nickname_sensitive" {
		t.Fatalf("nickname error = %v, want nickname_sensitive", fe["nickname"])
	}
}

// TestProductLoginExemptFromGate pins that send-code and login stay reachable
// while the window is NOT logged in (they are the login flow itself, so the
// gate must not block them).
func TestProductLoginExemptFromGate(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	// Not logged in, but the window still reaches send-code with its token.
	req := withWindowToken(httptest.NewRequest(http.MethodPost, "/api/product/send-code", bytes.NewBufferString(`{"phone":"13800001234"}`)), "tok")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("send-code status = %d, want 200 (exempt)", w.Code)
	}
}
