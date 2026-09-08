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

// Seed a logged-in, activated account with a known credit balance so the
// input-gate tests can assert a blocked message is never charged.
func seedAccount(t *testing.T, srv *Server, inputCheck bool) {
	t.Helper()
	if err := srv.productState.Mutate(func(st *productstate.State) error {
		st.Account = &productstate.Account{Phone: "13800001234", PhoneMasked: "138****1234", Nickname: "用户1234", Token: "local-tok"}
		st.Activation = &productstate.Activation{Activated: true, ActivatedAt: time.Now(), ExpiresAt: time.Now().AddDate(1, 0, 0)}
		st.Credits = productstate.Credits{Balance: 1280, MonthKey: "2026-09"}
		st.Prefs.InputSensitiveCheck = inputCheck
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func postJSON(t *testing.T, srv *Server, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withWindowToken(req, token)
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)
	return w
}

// TestSensitiveCheckHit exercises the check endpoint the frontend calls before
// sending: a hit reports hit=true and the masked text.
func TestSensitiveCheckHit(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	w := postJSON(t, srv, "/api/product/sensitive/check", `{"text":"增值税发票管理"}`, "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Hit    bool   `json:"hit"`
		Masked string `json:"masked"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Hit || resp.Masked != "增值税***管理" {
		t.Fatalf("resp = %+v, want hit true / masked 增值税***管理", resp)
	}
}

func TestSensitiveCheckMiss(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	w := postJSON(t, srv, "/api/product/sensitive/check", `{"text":"你好世界"}`, "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Hit bool `json:"hit"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Hit {
		t.Fatalf("hit = true, want false")
	}
}

func TestSensitiveCheckGated(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	w := postJSON(t, srv, "/api/product/sensitive/check", `{"text":"你好"}`, "tok")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a not-logged-in window", w.Code)
	}
}

// TestCreateChatInputSensitiveBlocksWithoutCharge locks the P8 §3.6 pipeline:
// with the switch on, a sensitive message is refused (400 input_sensitive) and
// the credit balance is untouched.
func TestCreateChatInputSensitiveBlocksWithoutCharge(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	seedAccount(t, srv, true)

	w := postJSON(t, srv, "/api/chat", `{"message":"这里有个发票"}`, "tok")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["code"] != "input_sensitive" {
		t.Fatalf("code = %v, want input_sensitive", resp["code"])
	}
	if snap := srv.productState.Snapshot().Credits; snap.Balance != 1280 || snap.MonthUsed != 0 {
		t.Fatalf("credits = %+v, want untouched balance 1280 monthUsed 0", snap)
	}
}

// TestCreateChatInputSensitiveOffSendsOriginal locks the other half of the
// switch: with input check off, the message goes through (and is charged).
// Output filtering still applies — that path is asserted in the app package.
func TestCreateChatInputSensitiveOffSendsOriginal(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	seedAccount(t, srv, false)

	w := postJSON(t, srv, "/api/chat", `{"message":"这里有个发票"}`, "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if snap := srv.productState.Snapshot().Credits; snap.Balance != 1279 || snap.MonthUsed != 1 {
		t.Fatalf("credits = %+v, want balance 1279 monthUsed 1", snap)
	}
}

// TestNicknameSensitiveRealEngine proves the P4 stub is now backed by the real
// engine: a nickname containing a built-in word is refused without a manual
// stub of productstate.Sensitive.
func TestNicknameSensitiveRealEngine(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	w := putJSON(t, srv, "/api/product/nickname", `{"nickname":"用户赌博"}`, "tok")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "nickname_sensitive" {
		t.Fatalf("code = %q, want nickname_sensitive", body["code"])
	}
	if got := srv.productState.Snapshot().Account.Nickname; got != "用户1234" {
		t.Fatalf("store nickname = %q after refusal, want unchanged 用户1234", got)
	}
}
