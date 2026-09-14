package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

// codedFailure is what the gateway's 402 now arrives as: an error that names the
// control plane's code (internal/provider/openai's HTTPError → agent.CodedError).
type codedFailure struct {
	code string
	msg  string
}

func (e codedFailure) Error() string     { return e.msg }
func (e codedFailure) ErrorCode() string { return e.code }

// runFailingTurn drives one turn against the given sender through the real turn
// path and returns the turn_error event the browser would receive.
func runFailingTurn(t *testing.T, sender agent.Sender) map[string]any {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("OCTO_DATA_ROOT", tmp)
	t.Setenv("USERPROFILE", tmp)

	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	srv.sender = sender
	srv.initWS()
	srv.turnRunning = make(map[string]bool)
	srv.steerQueues = make(map[string][]queuedTurn)
	srv.sessionAgents = make(map[string]*agent.Agent)

	sess := agent.NewSession("stub-model", "")
	sess.Title = "fixed title"
	if err := sess.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	conn := &wsConn{hub: srv.wsHub, send: make(chan []byte, 256), subscribed: map[string]struct{}{}}
	srv.wsHub.subscribe(conn, sess.ID)

	srv.doAgentTurn(sess, "a prompt that will fail", nil, nil)

	var seen []map[string]any
	waitFor(t, func() bool {
		seen = append(seen, drainConn(t, conn)...)
		return findTurnError(seen) != nil
	})
	return findTurnError(seen)
}

// TestTurnErrorCarriesTheControlPlaneCode is G3's nail on the wire: the code has to
// reach the browser, because it is the ONE thing the frontend keys its copy on
// (需求基线 C8 rule 1 — the client must not render `message`). Until this field
// existed, the server knew the code and the user got a sentence: the exact split
// V-55 records, where a Chinese sentence owned by the frontend could not be
// selected for a refusal the server had already named.
func TestTurnErrorCarriesTheControlPlaneCode(t *testing.T) {
	ev := runFailingTurn(t, failingSender{err: codedFailure{
		code: "insufficient_credits",
		msg:  "HTTP 402: balance too low",
	}})

	if got := ev["code"]; got != "insufficient_credits" {
		t.Errorf("turn_error code = %v, want %q — without it the bubble shows the gateway's sentence instead of 余额不足", got, "insufficient_credits")
	}
	// The sentence stays on the wire as the fallback for a build with no UI and for
	// codes the frontend does not know; it is the code, not the sentence, that the
	// UI is allowed to render.
	if msg, _ := ev["error"].(string); !strings.Contains(msg, "balance too low") {
		t.Errorf("turn_error error = %q, want the sender's sentence to survive as the fallback", msg)
	}
}

// TestTurnErrorOmitsTheCodeWhenThereIsNone is the other half, and it is a field
// PRESENCE assertion rather than a value one: `${"code": ""}` would make every
// uncoded failure look like a failure whose copy is missing, and those want
// different fallbacks. Absent means "no code"; present-and-empty is a fact that
// should never be produced.
func TestTurnErrorOmitsTheCodeWhenThereIsNone(t *testing.T) {
	ev := runFailingTurn(t, failingSender{err: errors.New("upstream exploded")})

	if got, present := ev["code"]; present {
		t.Errorf("turn_error carries code = %v for an error that names none; the key must be ABSENT, so the frontend can tell `no code` from `a code with no copy`", got)
	}
	if msg, _ := ev["error"].(string); !strings.Contains(msg, "upstream exploded") {
		t.Errorf("turn_error error = %q, want the sender's sentence", msg)
	}
}

// TestTheWithdrawnModelRefusalNamesItsCode closes V-55's localization gap at its
// source. The refusal fires when the catalog in hand does not offer the session's
// model (PR-5e), and until now the only thing it could tell the browser was an
// English sentence — while the frontend already owned the right Chinese one
// (session.model_withdrawn). The sentence stays (it is asserted in
// gateway_catalog_guard_test.go and is the no-UI backstop); the code is what lets
// the UI pick the sentence the requirement prescribes.
func TestTheWithdrawnModelRefusalNamesItsCode(t *testing.T) {
	offers, _ := offeredCatalog("buding-cloud-pro")
	srv, _ := gatewayServerWithCatalog(t, offers)

	sender, _ := srv.senderForSession(catalogBoundSession("buding-cloud-retired"))
	_, err := sender.SendMessages(context.Background(), "buding-cloud-retired", "", nil, 0)
	if err == nil {
		t.Fatal("SendMessages returned nil for a model the catalog no longer lists")
	}
	if got := agent.ErrorCodeOf(err); got != "model_withdrawn" {
		t.Errorf("code = %q, want %q (the spelling the frontend's mapping table keys on, and the one 中台交付包 §3.2 registers as a client-side fail-closed code)", got, "model_withdrawn")
	}
	if !strings.Contains(err.Error(), "buding-cloud-retired") {
		t.Errorf("refusal %q must still name the model, so a log with no UI can tell which one to replace", err.Error())
	}
}
