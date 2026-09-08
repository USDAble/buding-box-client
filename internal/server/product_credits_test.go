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

// TestCreateChatConsumesCredit locks the P6 wiring: a successful chat creation
// deducts one credit and hands the fresh balance back in the response (and
// persists it in the store). See P6-入口隐藏与积分.md §3.5.
func TestCreateChatConsumesCredit(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	if err := srv.productState.Mutate(func(st *productstate.State) error {
		st.Account = &productstate.Account{Phone: "13800001234", PhoneMasked: "138****1234", Nickname: "用户1234", Token: "local-tok"}
		st.Activation = &productstate.Activation{Activated: true, ActivatedAt: time.Now(), ExpiresAt: time.Now().AddDate(1, 0, 0)}
		st.Credits = productstate.Credits{Balance: 1280, MonthKey: "2026-09"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewReader([]byte(`{"message":"hello"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/chat", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}

	var resp createChatResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	if resp.Credits.Balance != 1279 || resp.Credits.MonthUsed != 1 {
		t.Fatalf("response credits = %+v, want balance 1279 monthUsed 1", resp.Credits)
	}
	if snap := srv.productState.Snapshot().Credits; snap.Balance != 1279 || snap.MonthUsed != 1 {
		t.Fatalf("store credits = %+v, want balance 1279 monthUsed 1", snap)
	}
}

// TestCreateChatZeroBalanceStillSends locks the §5.4.4 rule that 0 points does
// not block sending: the balance stays 0, monthUsed keeps climbing.
func TestCreateChatZeroBalanceStillSends(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	if err := srv.productState.Mutate(func(st *productstate.State) error {
		st.Account = &productstate.Account{Phone: "13800001234", PhoneMasked: "138****1234", Nickname: "用户1234", Token: "local-tok"}
		st.Credits = productstate.Credits{Balance: 0, MonthUsed: 5, MonthKey: "2026-09"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewReader([]byte(`{"message":"still sending"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/chat", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}

	var resp createChatResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	if resp.Credits.Balance != 0 {
		t.Fatalf("balance = %d, want 0 (0 分仍可发)", resp.Credits.Balance)
	}
	if resp.Credits.MonthUsed != 6 {
		t.Fatalf("monthUsed = %d, want 6", resp.Credits.MonthUsed)
	}
}
