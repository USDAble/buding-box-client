package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

// These nails cover the half of 需求 D1 that a frontend cannot be trusted with:
// the message is refused where the turn STARTS, so a `curl` that skips the
// composer is refused too.
//
// WHAT IS BEING TESTED HERE, AND WHAT IS FURTHER UP. The judgement ("is this text
// hit, and is the switch on") belongs to internal/productruntime - the switch is a
// user preference and the engine is a process-wide instance, and this package
// must not own either. It is nailed in check_route_test.go and on the mount road
// there. This file nails the CARRIER: that every turn entry point consults the
// verdict before it broadcasts, persists, or sends anything. The gate here is
// therefore a stub built the way the assembly builds it, which also keeps this
// test from being a second copy of the judgement.
//
// WHY THE CARRIER IS THE RISKY HALF. The archived implementation checked inside
// the WS handler, and the tempting modern shape - a decorator around the sender -
// would be wrong for a reason no judgement test can see: the WS path broadcasts
// the user message (ws_handlers.go:1396) and persists it ("Persist the user
// message right away") BEFORE buildAgent, so a refusal at send time leaves a
// question on screen and in the transcript that was never asked. The nails below
// assert ORDER, not merely the refusal: nothing sent, nothing stored.

// gatedServer is newFilteredServer plus a gate assembled the way
// cmd/octo-desktop assembles it: the switch and the engine are decided outside
// this package and only the verdict is injected.
func gatedServer(t *testing.T, sender agent.Sender, switchOn bool) *Server {
	t.Helper()
	srv := newFilteredServer(t, sender)
	engine := srv.sensitiveEngine
	srv.cfg.SensitiveInputGate = func(text string) (string, bool) {
		if !switchOn {
			return "", false
		}
		res := engine.Filter(text)
		if !res.Matched() {
			return "", false
		}
		return res.Text, true
	}
	return srv
}

// subscribeConn attaches a connection to the hub so a test can read what the
// browser would have received.
//
// Reading it takes nextEvent, not a drain: the hub routes broadcasts on its own
// goroutine (broadcast hands the event to wsHub.events), so nothing is on the
// channel at the instant the handler returns. A drain-and-hope here is a race
// that passes while the machine is idle and fails in a full-package run - which
// is exactly how this helper first behaved.
func subscribeConn(srv *Server, sid string) *wsConn {
	conn := &wsConn{hub: srv.wsHub, send: make(chan []byte, 256), subscribed: map[string]struct{}{}}
	srv.wsHub.subscribe(conn, sid)
	return conn
}

// sessionBytes reads the session file raw. "Nothing was stored" is a claim about
// bytes: a decoded message list cannot tell an absent entry from one whose text
// was emptied.
func sessionBytes(t *testing.T, sess *agent.Session) string {
	t.Helper()
	p, err := sess.SavePath()
	if err != nil {
		t.Fatalf("SavePath: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	return string(b)
}

// postTurn drives one REST turn to completion. This path is synchronous - it
// calls runTurn and writes the answer before returning - so the nails that need
// the turn to actually have run use it rather than the WS road, whose turn is a
// goroutine.
func postTurn(t *testing.T, srv *Server, sess *agent.Session, message string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat/"+sess.ID+"/turn", strings.NewReader(string(body)))
	req.SetPathValue("id", sess.ID)
	rec := httptest.NewRecorder()
	srv.handleTurn(rec, req)
	return rec
}

// decodeBodyOrFail decodes a JSON body, so a test can assert the envelope.
func decodeBodyOrFail(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON (%v): %.200s", err, rec.Body.String())
	}
	return body
}

// TestASensitiveMessageIsRefusedBeforeTheTurnStarts is D1's "命中则本轮不发、框内打码"
// on the un-bypassable road: the browser gets the masked text back so it can
// substitute the input box, and the turn never begins.
//
// The event's SHAPE is asserted rather than its presence alone: the frontend
// matches on `session_id` (ChatView.svelte:912 ignores an event for another
// session) and substitutes `text` verbatim, so an event carrying the raw word
// would put the word back into the input box.
func TestASensitiveMessageIsRefusedBeforeTheTurnStarts(t *testing.T) {
	dataRoot(t)
	sender := &countingSender{}
	srv := gatedServer(t, sender, true)
	sess := savedSession(t, "输入门")

	conn := subscribeConn(srv, sess.ID)
	srv.handleWSUserMessage(conn, &wsMsgUserMessage{
		SessionID: sess.ID,
		Content:   json.RawMessage(`"我要开发票"`),
	})

	ev := nextEvent(t, conn)
	if ev["type"] != "input_sensitive" {
		t.Fatalf("first event = %v, want input_sensitive; the browser would not know the message was refused", ev["type"])
	}
	if got := ev["session_id"]; got != sess.ID {
		t.Errorf("session_id = %v, want %q (the browser ignores events for other sessions)", got, sess.ID)
	}
	text, _ := ev["text"].(string)
	if !strings.Contains(text, "***") {
		t.Errorf("text = %q, want the masked form the composer substitutes", text)
	}
	if strings.Contains(text, "发票") {
		t.Errorf("text = %q still carries the word - it would be put back in the input box", text)
	}
}

// TestARefusedMessageIsNeverSentAndNeverStored is the other two thirds of the
// same claim, and the reason this is a pre-turn seam rather than a sender
// decorator:
//
//   - the sender was never called, which is what "本轮不计费" means when the
//     platform bills per request (this build has no credit counter of its own -
//     PR-5d1 made the platform ledger the only writer);
//   - the message is not in the session file, so a refresh cannot replay a
//     question that was never asked. A "write then roll back" implementation
//     satisfies the first half and fails here.
func TestARefusedMessageIsNeverSentAndNeverStored(t *testing.T) {
	dataRoot(t)
	sender := &countingSender{}
	srv := gatedServer(t, sender, true)
	sess := savedSession(t, "不入库")

	conn := subscribeConn(srv, sess.ID)
	srv.handleWSUserMessage(conn, &wsMsgUserMessage{
		SessionID: sess.ID,
		Content:   json.RawMessage(`"我要开发票"`),
	})

	if sender.calls != 0 {
		t.Errorf("the model was called %d time(s); a refused message must cost nothing", sender.calls)
	}
	if raw := sessionBytes(t, sess); strings.Contains(raw, "发票") {
		t.Errorf("the refused message is on disk, so a refresh would replay it:\n%s", raw)
	}
}

// TestTheSwitchOffLetsTheMessageThrough is D1 rule 2's carrier half - the switch
// decides, and this package does not second-guess it. The engine is installed and
// the word still hits, so a server that consulted the engine itself (ignoring the
// gate's answer) would refuse here.
//
// The second half is the other rule of the same requirement (D1 rule 3): the
// switch governs INPUT detection and nothing else. The stub's answer carries a
// hit word, so "turning the input check off" must still leave the model's words
// masked on the way out (G2) - a gate implemented as "the switch also disables
// filtering" would pass every refusal nail above and fail here.
func TestTheSwitchOffLetsTheMessageThrough(t *testing.T) {
	dataRoot(t)
	sender := &countingSender{replyText: "回答里带发票两个字"}
	srv := gatedServer(t, sender, false)
	sess := savedSession(t, "开关关闭")

	rec := postTurn(t, srv, sess, "我要开发票")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %.200s)", rec.Code, rec.Body.String())
	}
	if sender.calls != 1 {
		t.Errorf("the model was called %d time(s), want 1 - the message should have been sent as-is", sender.calls)
	}
	if sender.lastUserText != "我要开发票" {
		t.Errorf("the model was asked %q, want the message as typed", sender.lastUserText)
	}
	reply := rec.Body.String()
	if strings.Contains(reply, "发票") {
		t.Errorf("the answer was not masked once the input check was off: %s", reply)
	}
	if !strings.Contains(reply, "***") {
		t.Errorf("the answer carries no mask, so output masking died with the input switch: %s", reply)
	}
}

// TestANilGateRefusesNothing pins the CLI's shape (`octo serve` has no product
// assembly, so nothing is injected). nil must mean "no gate", never "refuse
// everything" - the same reasoning as sensitiveEngineOr, except that here the
// zero value must not turn a feature ON either.
func TestANilGateRefusesNothing(t *testing.T) {
	dataRoot(t)
	sender := &countingSender{}
	srv := gatedServer(t, sender, true)
	srv.cfg.SensitiveInputGate = nil
	sess := savedSession(t, "无注入")

	rec := postTurn(t, srv, sess, "我要开发票")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %.200s)", rec.Code, rec.Body.String())
	}
	if sender.calls != 1 {
		t.Errorf("the model was called %d time(s), want 1", sender.calls)
	}
}

// TestARestTurnRefusesASensitiveMessage covers the REST road, which the desktop
// does not use but an API caller does - and `curl` is the threat model D1 names.
// The refusal is a 400 carrying the same two facts as the WS event.
func TestARestTurnRefusesASensitiveMessage(t *testing.T) {
	dataRoot(t)
	sender := &countingSender{}
	srv := gatedServer(t, sender, true)
	sess := savedSession(t, "rest")
	before := sessionBytes(t, sess)

	rec := postTurn(t, srv, sess, "我要开发票")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %.200s)", rec.Code, rec.Body.String())
	}
	body := decodeBodyOrFail(t, rec)
	if body["code"] != "input_sensitive" {
		t.Errorf("code = %v, want input_sensitive", body["code"])
	}
	text, _ := body["text"].(string)
	if !strings.Contains(text, "***") || strings.Contains(text, "发票") {
		t.Errorf("text = %q, want the masked form", text)
	}
	if sender.calls != 0 {
		t.Errorf("the model was called %d time(s); a refused message must cost nothing", sender.calls)
	}
	if after := sessionBytes(t, sess); after != before {
		t.Errorf("the session file changed on a refused turn:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestARestCreatedChatRefusesBeforeTheSessionExists is the other REST entry
// point, and the sharper assertion of the two: handleCreateChat mints the session
// itself, so a gate placed one line too late leaves a session file behind for a
// message that was refused - and nothing about the response would look wrong.
func TestARestCreatedChatRefusesBeforeTheSessionExists(t *testing.T) {
	root := dataRoot(t)
	sender := &countingSender{}
	srv := gatedServer(t, sender, true)

	req := httptest.NewRequest(http.MethodPost, "/api/chat",
		strings.NewReader(`{"message":"我要开发票"}`))
	rec := httptest.NewRecorder()
	srv.handleCreateChat(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %.200s)", rec.Code, rec.Body.String())
	}
	if body := decodeBodyOrFail(t, rec); body["code"] != "input_sensitive" {
		t.Errorf("code = %v, want input_sensitive", body["code"])
	}
	if sender.calls != 0 {
		t.Errorf("the model was called %d time(s); nothing should have been sent", sender.calls)
	}

	sessions, err := filepath.Glob(filepath.Join(root, "sessions", "*.json"))
	if err != nil {
		t.Fatalf("glob sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("a refused create-chat left %d session file(s) behind: %v", len(sessions), sessions)
	}
}

// TestAnUnmatchedMessageReachesTheModelByteForByte is the reverse nail from the
// other side of the gate. 需求 D1 replaces the text only on a hit, so wiring the
// gate in as "always take its output" would rewrite every message - trimming a
// pasted trailing newline, say - while every refusal nail above stayed green.
// The assertion is on what the model was asked, not on what was stored, so it
// cannot be confounded by the agent's own message handling.
func TestAnUnmatchedMessageReachesTheModelByteForByte(t *testing.T) {
	dataRoot(t)
	const text = "今天天气不错 \t好\n"
	sender := &countingSender{}
	srv := gatedServer(t, sender, true)
	sess := savedSession(t, "未命中")

	rec := postTurn(t, srv, sess, text)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %.200s)", rec.Code, rec.Body.String())
	}
	if sender.calls != 1 {
		t.Fatalf("the model was called %d time(s), want 1", sender.calls)
	}
	if sender.lastUserText != text {
		t.Fatalf("the model was asked %q, want %q byte for byte", sender.lastUserText, text)
	}
}
