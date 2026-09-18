package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/pii"
)

const personalInfoFixture = "13800138000"

type personalInfoRecordingSender struct {
	mu        sync.Mutex
	userTexts []string
}

func (s *personalInfoRecordingSender) SendMessages(_ context.Context, _, _ string, messages []agent.Message, _ int) (agent.Reply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userTexts = append(s.userTexts, lastUserText(messages))
	return agent.Reply{Content: "stub reply"}, nil
}

func (s *personalInfoRecordingSender) firstUserText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.userTexts) == 0 {
		return ""
	}
	return s.userTexts[0]
}

func TestPersonalInfoRESTTurnMasksBeforeModelAndPersistence(t *testing.T) {
	dataRoot(t)
	sender := &countingSender{}
	srv := newFilteredServer(t, sender)
	sess := savedSession(t, "privacy turn")

	rec := postTurn(t, srv, sess, "电话 "+personalInfoFixture)
	if rec.Code != http.StatusOK {
		t.Fatalf("turn status = %d", rec.Code)
	}
	if sender.lastUserText != "电话 <手机号>" {
		t.Fatal("model did not receive the masked text")
	}
	if raw := sessionBytes(t, sess); strings.Contains(raw, personalInfoFixture) {
		t.Fatal("persisted session contains source text")
	}
	loaded, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lastUserText(loaded.Messages) != "电话 <手机号>" {
		t.Fatal("persisted session did not contain the masked value")
	}
}

func TestPersonalInfoDisabledPreservesText(t *testing.T) {
	dataRoot(t)
	sender := &countingSender{}
	srv := newFilteredServer(t, sender)
	sess := agent.NewSession("stub-model", "")
	sess.ProtectionPolicy.PersonalInfoProtection = false
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}

	rec := postTurn(t, srv, sess, "电话 "+personalInfoFixture)
	if rec.Code != http.StatusOK {
		t.Fatalf("turn status = %d", rec.Code)
	}
	if sender.lastUserText != "电话 "+personalInfoFixture {
		t.Fatal("disabled protection changed user text")
	}
}

func TestPersonalInfoFailureHasNoTurnSideEffects(t *testing.T) {
	dataRoot(t)
	sender := &countingSender{}
	srv := newFilteredServer(t, sender)
	srv.personalInfoTransform = func(string) (pii.Result, error) {
		return pii.Result{}, errors.New("injected transform failure")
	}
	sess := savedSession(t, "privacy failure")
	before := sessionBytes(t, sess)

	rec := postTurn(t, srv, sess, "电话 "+personalInfoFixture)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"privacy_transform_failed"`) {
		t.Fatalf("failure response = %d %s", rec.Code, rec.Body.String())
	}
	if sender.calls != 0 {
		t.Fatal("model was called after transform failure")
	}
	if after := sessionBytes(t, sess); after != before {
		t.Fatal("session changed after transform failure")
	}
}

func TestContentSafetyStopsBeforePersonalInfoTransform(t *testing.T) {
	dataRoot(t)
	srv := gatedServer(t, &countingSender{}, true)
	calls := 0
	srv.personalInfoTransform = func(text string) (pii.Result, error) {
		calls++
		return pii.New().Transform(text)
	}
	sess := savedSession(t, "pipeline order")

	rec := postTurn(t, srv, sess, "我要开发票")
	if rec.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("content-safety refusal status=%d transform_calls=%d", rec.Code, calls)
	}
}

func TestQueuedMessageStoresOnlyMaskedText(t *testing.T) {
	dataRoot(t)
	srv := newFilteredServer(t, &countingSender{})
	sess := savedSession(t, "privacy queue")
	sess.LockProtectionPolicy()
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	srv.turnRunning[sess.ID] = true

	srv.handleWSUserMessage(nil, &wsMsgUserMessage{
		SessionID: sess.ID,
		Content:   []byte(`"电话 13800138000"`),
		Queue:     true,
	})
	queued := srv.drainSteer(sess.ID)
	if len(queued) != 1 || queued[0].item.Text != "电话 <手机号>" {
		t.Fatal("queued text was not masked before entering the queue")
	}
}

func TestPersonalInfoWebSocketEventContainsOnlyAggregateMetadata(t *testing.T) {
	dataRoot(t)
	srv := newFilteredServer(t, &countingSender{})
	sess := savedSession(t, "privacy event")
	sess.LockProtectionPolicy()
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	srv.turnRunning[sess.ID] = true
	conn := subscribeConn(srv, sess.ID)

	srv.handleWSUserMessage(conn, &wsMsgUserMessage{
		SessionID: sess.ID,
		Content:   []byte(`"电话 13800138000"`),
		Queue:     true,
	})
	ev := nextEventOfType(t, conn, "privacy_applied")
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), personalInfoFixture) || ev["session_id"] != sess.ID || ev["count"] != float64(1) || ev["rule_version"] != pii.RuleVersion {
		t.Fatal("privacy event did not contain only the expected aggregate metadata")
	}
	categories, ok := ev["categories"].([]any)
	if !ok || len(categories) != 1 || categories[0] != "cn_mobile" {
		t.Fatal("privacy event categories do not match the applied rule")
	}
}

func TestIdleWebSocketMessageUsesOneAuthoritativeTransform(t *testing.T) {
	dataRoot(t)
	srv := newFilteredServer(t, &countingSender{})
	sess := savedSession(t, "privacy authoritative")
	calls := 0
	srv.personalInfoTransform = func(text string) (pii.Result, error) {
		calls++
		return pii.New().Transform(text)
	}

	srv.handleWSUserMessage(nil, &wsMsgUserMessage{
		SessionID: sess.ID,
		Content:   []byte(`"电话 13800138000"`),
	})
	if calls != 1 {
		t.Fatalf("idle WebSocket transform calls = %d, want one authoritative pass", calls)
	}
	waitFor(t, func() bool {
		mu := srv.sessionTurnLock(sess.ID)
		mu.Lock()
		defer mu.Unlock()
		return !srv.turnRunning[sess.ID]
	})
}

func TestEditedMessageIsMaskedBeforeRerunAndPersistence(t *testing.T) {
	dataRoot(t)
	sender := &personalInfoRecordingSender{}
	srv := newFilteredServer(t, sender)
	sess := agent.NewSession("stub-model", "")
	sess.Messages = []agent.Message{
		{Role: agent.RoleUser, Content: "旧问题"},
		{Role: agent.RoleAssistant, Content: "旧回答"},
	}
	sess.LockProtectionPolicy()
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}

	w := doJSON(t, srv, http.MethodPost, "/api/sessions/"+sess.ID+"/edit_message",
		`{"message_index":0,"new_content":"联系 13800138000"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("edit status = %d", w.Code)
	}
	var loaded *agent.Session
	waitFor(t, func() bool {
		var err error
		loaded, err = agent.LoadSession(sess.ID)
		return err == nil && len(loaded.Messages) == 2 &&
			lastUserText(loaded.Messages) == "联系 <手机号>" &&
			loaded.Messages[1].Role == agent.RoleAssistant && loaded.Messages[1].Content == "stub reply"
	})
	if sender.firstUserText() != "联系 <手机号>" || lastUserText(loaded.Messages) != "联系 <手机号>" {
		t.Fatal("edited text was not masked before the rerun and persistence")
	}
	if strings.Contains(sessionBytes(t, sess), personalInfoFixture) {
		t.Fatal("edited session contains source text")
	}
}

func TestGoalObjectiveIsMaskedBeforePersistence(t *testing.T) {
	dataRoot(t)
	srv := newFilteredServer(t, &countingSender{})
	srv.goalsEnabled.Store(true)
	sess := savedSession(t, "privacy goal")

	w := doJSON(t, srv, http.MethodPut, "/api/sessions/"+sess.ID+"/goal",
		`{"objective":"联系 13800138000"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("goal update status = %d", w.Code)
	}
	loaded, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	goal, ok := loaded.GoalSnapshot()
	if !ok || goal.Objective != "联系 <手机号>" {
		t.Fatal("goal objective was not masked before persistence")
	}
	if strings.Contains(sessionBytes(t, sess), personalInfoFixture) {
		t.Fatal("goal persistence contains source text")
	}
}
