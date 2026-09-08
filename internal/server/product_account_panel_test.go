package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productstate"
)

// The P5 account-panel endpoints: PUT /api/product/nickname and PUT
// /api/product/prefs. Both are product-gated, so the not-logged-in window
// case is asserted here as well as the happy paths.

func putJSON(t *testing.T, srv *Server, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req = withWindowToken(req, token)
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)
	return w
}

func decodeState(t *testing.T, w *httptest.ResponseRecorder) productstate.PublicState {
	t.Helper()
	var resp struct {
		State productstate.PublicState `json:"state"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode state: %v (body %s)", err, w.Body.String())
	}
	return resp.State
}

func TestProductNicknameOK(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	w := putJSON(t, srv, "/api/product/nickname", `{"nickname":"新昵称123"}`, "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	state := decodeState(t, w)
	if state.Account == nil || state.Account.Nickname != "新昵称123" {
		t.Fatalf("nickname = %v, want 新昵称123", state.Account)
	}
	// The returned state is the persisted one, not a copy cooked for the reply.
	if got := srv.productState.Snapshot().Account.Nickname; got != "新昵称123" {
		t.Fatalf("store nickname = %q, want 新昵称123", got)
	}
}

func TestProductNicknameRejectsBadFormatWithoutWriting(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	w := putJSON(t, srv, "/api/product/nickname", `{"nickname":"a"}`, "tok")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "nickname_format" {
		t.Fatalf("code = %q, want nickname_format", body["code"])
	}
	// A refused nickname must not have been written (需求 §5.4.2: validation
	// failure never leaves the state file modified).
	if got := srv.productState.Snapshot().Account.Nickname; got != "用户1234" {
		t.Fatalf("store nickname = %q after refusal, want unchanged 用户1234", got)
	}
}

func TestProductNicknameRejectsSensitiveWithoutWriting(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	// productstate.Sensitive is a package var the sensitive engine (P7/P8)
	// swaps in; stub it on for this test to prove the wiring, then restore.
	orig := productstate.Sensitive
	productstate.Sensitive = func(v string) bool { return strings.Contains(v, "赌博") }
	defer func() { productstate.Sensitive = orig }()

	w := putJSON(t, srv, "/api/product/nickname", `{"nickname":"用户赌博"}`, "tok")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
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

func TestProductNicknameGated(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	w := putJSON(t, srv, "/api/product/nickname", `{"nickname":"新昵称"}`, "tok")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a not-logged-in window", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "product_gate" {
		t.Fatalf("error = %q, want product_gate", body["error"])
	}
}

func TestProductPrefsUpdate(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	// Update both at once.
	w := putJSON(t, srv, "/api/product/prefs", `{"locale":"en","defaultChatMode":"privacy"}`, "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	state := decodeState(t, w)
	if state.Prefs.Locale != "en" || state.Prefs.DefaultChatMode != "privacy" {
		t.Fatalf("prefs = %+v, want locale en / privacy", state.Prefs)
	}

	// Partial update leaves the untouched field alone.
	w = putJSON(t, srv, "/api/product/prefs", `{"defaultChatMode":"smart"}`, "tok")
	state = decodeState(t, w)
	if state.Prefs.Locale != "en" || state.Prefs.DefaultChatMode != "smart" {
		t.Fatalf("prefs = %+v, want locale en kept / smart", state.Prefs)
	}
}

func TestProductPrefsRejectsUnknownValues(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	for _, body := range []string{
		`{"locale":"fr"}`,
		`{"defaultChatMode":"turbo"}`,
	} {
		w := putJSON(t, srv, "/api/product/prefs", body, "tok")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, w.Code)
		}
		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp["code"] != "invalid_value" {
			t.Fatalf("body %s: code = %q, want invalid_value", body, resp["code"])
		}
	}
	// Nothing leaked into the store.
	st := srv.productState.Snapshot()
	if st.Prefs.Locale != "" || st.Prefs.DefaultChatMode != "" {
		t.Fatalf("prefs = %+v, want untouched defaults", st.Prefs)
	}
}

func TestProductPrefsGated(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	w := putJSON(t, srv, "/api/product/prefs", `{"defaultChatMode":"privacy"}`, "tok")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a not-logged-in window", w.Code)
	}
}

// TestActivationSurfacesPanelState pins the P5 DTO extension: after the
// activation login the panel's rows have something to render — activation
// timestamps (license), the seeded "trial" machine code (plan) and the seeded
// points balance (credits). P4's login test asserted the login itself; this
// one asserts what P5 reads back.
func TestActivationSurfacesPanelState(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	sendCodeReq(t, srv, "13800001234")

	w := loginReq(t, srv, map[string]any{
		"phone": "13800001234", "code": "123456", "nickname": "用户1234",
		"activationCode": "BUDING-DEMO-0001",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	state := decodeState(t, w)
	if !state.Activated || state.Activation == nil {
		t.Fatalf("activation = %+v, want activated with timestamps", state.Activation)
	}
	if state.Activation.ExpiresAt.IsZero() || state.Activation.ActivatedAt.IsZero() {
		t.Fatalf("activation timestamps not set: %+v", state.Activation)
	}
	if state.Plan.Name != "trial" {
		t.Fatalf("plan.name = %q, want trial", state.Plan.Name)
	}
	if state.Credits.Balance != 1280 {
		t.Fatalf("credits.balance = %d, want 1280", state.Credits.Balance)
	}
}
