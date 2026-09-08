package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productgate"
	"github.com/open-octo/octo-agent/internal/productstate"
)

func productTestEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("OCTO_DATA_ROOT", tmp)
	t.Setenv("USERPROFILE", tmp)
}

func withWindowToken(r *http.Request, tok string) *http.Request {
	if tok != "" {
		r.Header.Set(productgate.HeaderWindowToken, tok)
	}
	return r
}

func login(t *testing.T, srv *Server) {
	t.Helper()
	if err := srv.productState.Mutate(func(st *productstate.State) error {
		st.Account = &productstate.Account{Phone: "13800001234", PhoneMasked: "138****1234", Nickname: "用户1234"}
		return nil
	}); err != nil {
		t.Fatalf("login Mutate = %v", err)
	}
}

// TestProductGateBlocksUnloggedWindow is the core gate assertion: a request
// that carries the window token but is not logged in gets 403 product_gate.
func TestProductGateBlocksUnloggedWindow(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	req := withWindowToken(httptest.NewRequest(http.MethodGet, "/api/sessions", nil), "tok")
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "product_gate" || body["reason"] != "not_logged_in" {
		t.Fatalf("body = %v, want product_gate/not_logged_in", body)
	}
}

// TestProductGatePassesNoToken pins the deliberate CLI boundary: a request
// WITHOUT the window token is not the window, so it passes even when not
// logged in (需求 §9.8). This looks like a bug and is the exact behaviour a
// later maintainer might "fix" — lock it in with a test.
func TestProductGatePassesNoToken(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil) // no token
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (CLI / loopback peer passes)", w.Code)
	}
}

// TestProductGatePassesLoggedInWindow: a logged-in window carrying its token
// passes the gate and reaches the handler.
func TestProductGatePassesLoggedInWindow(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	req := withWindowToken(httptest.NewRequest(http.MethodGet, "/api/sessions", nil), "tok")
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// TestMachineGateOutranksProductGate: a non-loopback request that also lacks
// the access key must fail at the OUTER machine gate with 401, never reach the
// product gate's 403 — the two are stacked, machine first.
func TestMachineGateOutranksProductGate(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	req := withWindowToken(httptest.NewRequest(http.MethodGet, "/api/sessions", nil), "tok")
	req.RemoteAddr = "10.0.0.5:12345" // not loopback
	req.Host = "10.0.0.5:8080"
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (machine gate must run first)", w.Code)
	}
}

// TestProductStateExempt: the state lookup is exempt from the product gate so
// the frontend can route between the login gate and the main UI before login.
func TestProductStateExempt(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	req := withWindowToken(httptest.NewRequest(http.MethodGet, "/api/product/state", nil), "tok")
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (state is exempt)", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["loggedIn"] != false {
		t.Fatalf("loggedIn = %v, want false", body["loggedIn"])
	}
}

// TestProductLocaleExemptAndPersists: language can switch before login and the
// value lands in prefs.
func TestProductLocaleExemptAndPersists(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	req := withWindowToken(httptest.NewRequest(http.MethodPut, "/api/product/locale", strings.NewReader(`{"locale":"zh"}`)), "tok")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (locale is exempt)", w.Code)
	}
	if got := srv.productState.Snapshot().Prefs.Locale; got != "zh" {
		t.Fatalf("locale = %q, want zh", got)
	}
}

// TestProductLogoutGatedAndClearsAccount: logout is gated (only a logged-in
// window reaches it) and clears the account while keeping activation/credits.
func TestProductLogoutGatedAndClearsAccount(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	// Not logged in yet: logout must 403 through the gate.
	req := withWindowToken(httptest.NewRequest(http.MethodPost, "/api/product/logout", nil), "tok")
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unlogged logout status = %d, want 403", w.Code)
	}

	// Log in with an activation + credits, then log out.
	if err := srv.productState.Mutate(func(st *productstate.State) error {
		st.Account = &productstate.Account{Phone: "13800001234"}
		st.Activation = &productstate.Activation{Activated: true}
		st.Credits = productstate.Credits{Balance: 1280, MonthUsed: 1}
		return nil
	}); err != nil {
		t.Fatalf("Mutate = %v", err)
	}

	req = withWindowToken(httptest.NewRequest(http.MethodPost, "/api/product/logout", nil), "tok")
	w = httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", w.Code)
	}

	snap := srv.productState.Snapshot()
	if snap.Account != nil {
		t.Fatal("logout must clear the account")
	}
	if !snap.Activated() {
		t.Fatal("logout must keep activation")
	}
	if snap.Credits.Balance != 1280 || snap.Credits.MonthUsed != 1 {
		t.Fatalf("logout must keep credits: %+v", snap.Credits)
	}
}
