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
	// The factory grouping carries the three mode ids and NO model ids: the
	// models are a catalog projection (P0-04). A stock install must not list a
	// model it cannot select.
	for _, id := range []string{"privacy", "smart", "default"} {
		m, ok := byID[id]
		if !ok {
			t.Fatalf("factory list is missing mode %q", id)
		}
		if len(m.Models) != 0 {
			t.Errorf("mode %q lists hardcoded models %v, want none", id, m.Models)
		}
		if m.DefaultModel != "" {
			t.Errorf("mode %q has hardcoded defaultModel %q, want empty", id, m.DefaultModel)
		}
	}

	// A missing chat-modes.json must NOT be seeded: startup never creates a
	// user-editable file (开发规范 §3.9.1). The handler serves the factory
	// grouping straight from memory.
	path, _ := datapath.Join("chat-modes.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("chat-modes.json was created at %s (stat err = %v), want no file", path, err)
	}
}

// The mode→model grouping is user-editable config; each model id is resolved
// against config.yml's endpoint system so the selector gets the composite id
// it needs to bind a session. This drives both files explicitly — the factory
// grouping no longer carries model ids of its own.
func TestGetChatModesResolvesCompositeIDs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", tmp)
	cfgPath, _ := datapath.Join("config.yml")
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o700)
	if err := os.WriteFile(cfgPath, []byte("endpoints:\n  - id: buding\n    provider: local\n    models:\n      - model: buding-local-general\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	modesPath, _ := datapath.Join("chat-modes.json")
	modes := `{"schemaVersion":1,"modes":[{"id":"privacy","models":["buding-local-general","not-configured"],"defaultModel":"buding-local-general"}]}`
	if err := os.WriteFile(modesPath, []byte(modes), 0o600); err != nil {
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
	got := map[string]string{}
	for _, m := range privacy.Models {
		got[m.ID] = m.CompositeID
	}
	if got["buding-local-general"] != "buding::buding-local-general" {
		t.Errorf("composite id for buding-local-general = %q, want buding::buding-local-general", got["buding-local-general"])
	}
	// An id with no matching endpoint stays listed but unselectable.
	if id, ok := got["not-configured"]; !ok || id != "" {
		t.Errorf("composite id for not-configured = %q (present=%v), want empty but listed", id, ok)
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
