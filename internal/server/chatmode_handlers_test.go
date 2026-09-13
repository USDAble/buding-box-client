package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
)

// The route under test is this fork's (V-46): the picker's mode was persisted
// through a path that only the DEV fake backend ever answered, so on a real
// build the first of the two requests a model switch makes was a 404 with a
// plain-text body — which the frontend renders verbatim as "404 Not Found".
// Every nail below therefore goes through the real mux.

func chatModeSession(t *testing.T) *agent.Session {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("OCTO_DATA_ROOT", tmp)
	t.Setenv("USERPROFILE", tmp)

	sess := agent.NewSession("stub-model", "")
	sess.Messages = []agent.Message{agent.NewUserMessage("ping")}
	if err := sess.Save(); err != nil {
		t.Fatalf("save session: %v", err)
	}
	return sess
}

func TestTheChatModeRouteIsRegisteredAndReachable(t *testing.T) {
	sess := chatModeSession(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/chat_mode", `{"chat_mode":"privacy"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH /api/sessions/{id}/chat_mode = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		OK       bool   `json:"ok"`
		ChatMode string `json:"chat_mode"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal %s: %v", w.Body.String(), err)
	}
	if !resp.OK || resp.ChatMode != "privacy" {
		t.Fatalf("response = %s, want ok=true and chat_mode=privacy", w.Body.String())
	}

	got, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatMode != "privacy" {
		t.Fatalf("session on disk has chat_mode %q, want privacy", got.ChatMode)
	}
}

func TestAnUnknownChatModeIsRefused(t *testing.T) {
	sess := chatModeSession(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	if w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/chat_mode", `{"chat_mode":"smart"}`); w.Code != http.StatusOK {
		t.Fatalf("seed PATCH = %d: %s", w.Code, w.Body.String())
	}

	w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/chat_mode", `{"chat_mode":"nonsense"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown mode = %d, want 400: %s", w.Code, w.Body.String())
	}

	// The refusal must not have written the bad value through.
	got, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatMode != "smart" {
		t.Fatalf("chat_mode = %q after a refused write, want the previous value smart", got.ChatMode)
	}
}

func TestAMissingSessionIsNotFoundOnTheChatModeRoute(t *testing.T) {
	chatModeSession(t) // env isolation only
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	w := doJSON(t, srv, http.MethodPatch, "/api/sessions/does-not-exist/chat_mode", `{"chat_mode":"privacy"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing session = %d, want 404: %s", w.Code, w.Body.String())
	}
	// A JSON envelope, not the mux's plain-text "404 page not found": that
	// difference is what tells "this session is gone" apart from "this route
	// does not exist" — the ambiguity that cost a debugging round (V-46).
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Error == "" {
		t.Fatalf("body = %q, want a JSON error envelope", w.Body.String())
	}
}

func TestTheSessionListCarriesTheChatMode(t *testing.T) {
	sess := chatModeSession(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})

	if w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/chat_mode", `{"chat_mode":"privacy"}`); w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d: %s", w.Code, w.Body.String())
	}

	w := doJSON(t, srv, http.MethodGet, "/api/sessions", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions = %d: %s", w.Code, w.Body.String())
	}
	// Decoded into a map so the assertion is on the wire name itself: the
	// frontend reads session.chat_mode, and a struct tag drifting to chatMode
	// (or ChatMode) would silently empty the chip again.
	var resp struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, s := range resp.Sessions {
		if s["id"] != sess.ID {
			continue
		}
		if s["chat_mode"] != "privacy" {
			t.Fatalf("listed chat_mode = %v, want privacy", s["chat_mode"])
		}
		return
	}
	t.Fatalf("session %s not in list", sess.ID)
}

func TestTheSwitchBroadcastsTheNewChatMode(t *testing.T) {
	sess := chatModeSession(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	srv.initWS()

	conn := &wsConn{
		hub:        srv.wsHub,
		send:       make(chan []byte, 256),
		subscribed: map[string]struct{}{},
	}
	srv.wsHub.register <- conn
	srv.wsHub.subscribe(conn, sess.ID)

	if w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/chat_mode", `{"chat_mode":"privacy"}`); w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d: %s", w.Code, w.Body.String())
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case b := <-conn.send:
			var ev map[string]any
			if err := json.Unmarshal(b, &ev); err != nil {
				continue
			}
			if ev["type"] != "session_update" || ev["session_id"] != sess.ID {
				continue
			}
			if ev["chat_mode"] != "privacy" {
				t.Fatalf("session_update chat_mode = %v, want privacy (P10: another window must see which mode this session is in)", ev["chat_mode"])
			}
			return
		case <-deadline:
			t.Fatal("no session_update carrying chat_mode arrived")
		}
	}
}
