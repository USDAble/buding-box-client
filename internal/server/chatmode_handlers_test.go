package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/productstate"
)

func TestGetChatModesFactoryList(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", tmp)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	w := doJSON(t, srv, http.MethodGet, "/api/product/chat-modes", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp chatModesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, w.Body.String())
	}
	if resp.Fallback {
		t.Fatal("fallback = true on a fresh data root, want false")
	}
	if len(resp.Modes) != 3 {
		t.Fatalf("len(modes) = %d, want 3", len(resp.Modes))
	}
	byID := map[string]chatModeJSON{}
	for _, m := range resp.Modes {
		byID[m.ID] = m
	}
	privacy := byID["privacy"]
	for _, m := range privacy.Models {
		if m.ID == "buding-cloud-plus" || m.ID == "buding-cloud-pro" {
			t.Errorf("privacy mode contains %q, want local-only", m.ID)
		}
	}
	smart := byID["smart"]
	for _, m := range smart.Models {
		if m.ID == "buding-local-general" || m.ID == "buding-local-fast" {
			t.Errorf("smart mode contains %q, want cloud-only", m.ID)
		}
	}
	if byID["default"].DefaultModel != "buding-cloud-plus" {
		t.Errorf("default mode defaultModel = %q, want buding-cloud-plus", byID["default"].DefaultModel)
	}

	// First GET seeds chat-modes.json so the user can edit it (需求 §5.6 规则 1).
	path, _ := datapath.Join("chat-modes.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("chat-modes.json not written: %v", err)
	}
}

func TestGetChatModesResolvesCompositeIDs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", tmp)
	// Seed a config.yml endpoint exposing one of the factory models so the
	// handler resolves its composite id.
	cfgPath, _ := datapath.Join("config.yml")
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o700)
	if err := os.WriteFile(cfgPath, []byte("endpoints:\n  - id: buding\n    provider: local\n    models:\n      - model: buding-local-general\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	w := doJSON(t, srv, http.MethodGet, "/api/product/chat-modes", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp chatModesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	privacy := resp.Modes[0]
	var got string
	for _, m := range privacy.Models {
		if m.ID == "buding-local-general" {
			got = m.CompositeID
		}
	}
	if got != "buding::buding-local-general" {
		t.Errorf("composite id for buding-local-general = %q, want buding::buding-local-general", got)
	}
}

func TestGetChatModesFallbackOnCorrupt(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", tmp)
	path, _ := datapath.Join("chat-modes.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	original := []byte("{ not json ")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	w := doJSON(t, srv, http.MethodGet, "/api/product/chat-modes", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp chatModesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Fallback {
		t.Fatal("fallback = false, want true (corrupt file)")
	}
	if len(resp.Modes) != 3 {
		t.Fatalf("len(modes) = %d, want 3 (builtin fallback)", len(resp.Modes))
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatalf("user file overwritten: got %q want %q", after, original)
	}
}

func TestSetSessionChatMode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", tmp)

	sess := agent.NewSession("stub-model", "")
	if err := sess.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	patch := func(id, mode string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]string{"mode": mode})
		req := httptest.NewRequest(http.MethodPut, "/api/sessions/"+id+"/chat-mode", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		serveLoopback(srv.mux, w, req)
		return w
	}

	if w := patch(sess.ID, "privacy"); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	got, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatMode != "privacy" {
		t.Fatalf("session ChatMode = %q, want privacy", got.ChatMode)
	}

	if w := patch(sess.ID, "bogus"); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown mode; body=%s", w.Code, w.Body.String())
	}
}

func TestCreateSessionAppliesDefaultMode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", tmp)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	// Simulate the account choosing privacy as the new-session default.
	if err := srv.productState.Mutate(func(st *productstate.State) error {
		st.Prefs.DefaultChatMode = "privacy"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	w := doJSON(t, srv, http.MethodPost, "/api/sessions", `{"name":"hello"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Session sessionItem `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Session.ChatMode != "privacy" {
		t.Fatalf("session chat_mode = %q, want privacy (account default)", resp.Session.ChatMode)
	}

	// A legacy account with no default set falls back to the "default" group.
	// Use a fresh data root so the previous mutation's persisted "privacy"
	// doesn't leak into this second server's store.
	tmp2 := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", tmp2)
	srv2 := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	w2 := doJSON(t, srv2, http.MethodPost, "/api/sessions", `{"name":"world"}`)
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w2.Code, w2.Body.String())
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Session.ChatMode != "default" {
		t.Fatalf("session chat_mode = %q, want default (factory)", resp.Session.ChatMode)
	}
}

func TestCreateSessionExplicitChatMode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", tmp)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	// Set the account default to privacy, then create a session with an
	// explicit chat_mode — the landing-page pick must win over the default.
	if err := srv.productState.Mutate(func(st *productstate.State) error {
		st.Prefs.DefaultChatMode = "privacy"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	w := doJSON(t, srv, http.MethodPost, "/api/sessions", `{"name":"hi","chat_mode":"smart"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Session sessionItem `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Session.ChatMode != "smart" {
		t.Fatalf("session chat_mode = %q, want smart (explicit create-time pick)", resp.Session.ChatMode)
	}
}
