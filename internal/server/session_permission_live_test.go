package server

import (
	"context"
	"encoding/json"
	"github.com/open-octo/octo-agent/internal/agent"
	"net/http"
	"testing"
	"time"
)

func TestWebPermissionCreationIsAtomicAndKeepsOlderSessions(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Model: "stub-model"})
	for _, mode := range []string{"auto", "interactive", "strict"} {
		response := doJSON(t, srv, http.MethodPost, "/api/sessions", `{"permission_mode":"`+mode+`"}`)
		if response.Code != 200 {
			t.Fatalf("create %s: %d %s", mode, response.Code, response.Body.String())
		}
		var body struct {
			Session sessionItem `json:"session"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		saved, err := agent.LoadSession(body.Session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if saved.PermissionModeValue() != mode {
			t.Fatalf("persisted %q want %q", saved.PermissionModeValue(), mode)
		}
	}
	if w := doJSON(t, srv, http.MethodPost, "/api/sessions", `{"permission_mode":"invalid"}`); w.Code != 400 {
		t.Fatalf("invalid: %d", w.Code)
	}
}

func TestSwitchAutoReleasesOnlyCurrentToolApproval(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())
	srv := mustServer(t, Config{Addr: "127.0.0.1:0"})
	srv.initWS()
	srv.confirmations = make(map[string]chan string)
	srv.pendingConfirms = make(map[string]wsEventRequestConfirmation)
	sess := agent.NewSession("stub-model", "")
	sess.SetPermissionModeValue("interactive")
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	srv.sessionAgentsMu.Lock()
	if srv.liveSessions == nil {
		srv.liveSessions = make(map[string]*agent.Session)
	}
	srv.liveSessions[sess.ID] = sess
	srv.sessionAgentsMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan string, 1)
	go func() {
		result, err := srv.requestConfirmation(ctx, sess.ID, "Allow?", "yes_no_always", confirmDetail{ToolName: "terminal"})
		if err != nil {
			done <- "error"
		} else {
			done <- result
		}
	}()
	waitPendingConfirmation(t, srv, sess.ID)
	w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/permission_mode", `{"permission_mode":"auto"}`)
	if w.Code != 200 {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	select {
	case result := <-done:
		if result != "yes" {
			t.Fatalf("got %q", result)
		}
	case <-time.After(time.Second):
		t.Fatal("pending approval not released")
	}
	saved, err := agent.LoadSession(sess.ID)
	if err != nil || saved.PermissionModeValue() != "auto" {
		t.Fatalf("not persisted: %v", err)
	}
}

func TestConfirmationCancellationAndDuplicateAnswersCannotApprove(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deny-first", true: "cancel-first"}[cancelled], func(t *testing.T) {
			t.Setenv("OCTO_DATA_ROOT", t.TempDir())
			srv := mustServer(t, Config{Addr: "127.0.0.1:0"})
			srv.initWS()
			srv.confirmations = make(map[string]chan string)
			srv.pendingConfirms = make(map[string]wsEventRequestConfirmation)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan string, 1)
			go func() {
				r, e := srv.requestConfirmation(ctx, "pending", "Allow?", "yes_no_always", confirmDetail{ToolName: "terminal"})
				if e != nil {
					done <- "cancelled"
				} else {
					done <- r
				}
			}()
			id := waitPendingConfirmation(t, srv, "pending")
			if cancelled {
				cancel()
			} else {
				srv.handleWSConfirmation(id, "deny")
			}
			srv.handleWSConfirmation(id, "yes")
			select {
			case result := <-done:
				want := "deny"
				if cancelled {
					want = "cancelled"
				}
				if result != want {
					t.Fatalf("got %q want %q", result, want)
				}
			case <-time.After(time.Second):
				t.Fatal("duplicate blocked")
			}
			srv.handleWSConfirmation(id, "yes") // an expired id remains inert
		})
	}
}

func waitPendingConfirmation(t *testing.T, srv *Server, sid string) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		srv.pendingPromptMu.Lock()
		p, ok := srv.pendingConfirms[sid]
		srv.pendingPromptMu.Unlock()
		if ok {
			return p.ConfID
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("approval did not become pending")
	return ""
}

func TestAutoDoesNotAnswerOtherSessionsOrNonToolConfirmations(t *testing.T) {
	srv := mustServer(t, Config{Addr: "127.0.0.1:0"})
	sess := agent.NewSession("stub", "")
	sess.SetPermissionModeValue("auto")
	srv.liveSessions = map[string]*agent.Session{"a": sess}
	a, b := make(chan string, 1), make(chan string, 1)
	srv.confirmations = map[string]chan string{"a-confirm": a, "b-confirm": b}
	srv.pendingConfirms = map[string]wsEventRequestConfirmation{
		"a": {SessionID: "a", ConfID: "a-confirm", Kind: "ok"},
		"b": {SessionID: "b", ConfID: "b-confirm", Kind: "yes_no_always", ToolName: "terminal"},
	}
	srv.resolvePendingPermissionOnAuto("a")
	srv.resolvePendingPermissionOnAuto("b")
	if len(a) != 0 || len(b) != 0 {
		t.Fatal("unrelated confirmation released")
	}
}
