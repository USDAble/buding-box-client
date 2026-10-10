package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
)

func TestHistoryReplyDurationUsesPersistedTurnTimes(t *testing.T) {
	setTestHome(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	sess := agent.NewSession("stub-model", "")
	sess.Messages = []agent.Message{
		{Role: agent.RoleUser, Content: "first", CreatedAt: start},
		{Role: agent.RoleAssistant, CreatedAt: start.Add(time.Second), Blocks: []agent.ContentBlock{{Type: "tool_use", Name: "terminal", ID: "tool"}}},
		{Role: agent.RoleUser, CreatedAt: start.Add(10 * time.Second), Blocks: []agent.ContentBlock{{Type: "tool_result", ToolUseID: "tool", Result: "done"}}},
		{Role: agent.RoleAssistant, Content: "first answer", CreatedAt: start.Add(65 * time.Second)},
		{Role: agent.RoleUser, Content: "second", CreatedAt: start.Add(time.Hour)},
		{Role: agent.RoleAssistant, Content: "second answer", CreatedAt: start.Add(2*time.Hour + 5*time.Minute)},
		{Role: agent.RoleUser, Content: "legacy"},
		{Role: agent.RoleAssistant, Content: "legacy answer"},
		{Role: agent.RoleUser, Content: "invalid clock", CreatedAt: start.Add(3 * time.Hour)},
		{Role: agent.RoleAssistant, Content: "invalid clock answer", CreatedAt: start},
	}
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	// Reopening must restore the same durations, not measure time since now.
	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID+"/messages", nil)
		req.SetPathValue("id", sess.ID)
		rec := httptest.NewRecorder()
		srv.handleGetSessionMessages(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Body.String())
		}
		var response struct {
			Events []map[string]any `json:"events"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		var replies []map[string]any
		for _, ev := range response.Events {
			if ev["type"] == "assistant_message" {
				replies = append(replies, ev)
			}
		}
		if len(replies) != 4 || replies[0]["duration_ms"] != float64(65_000) || replies[1]["duration_ms"] != float64(3_900_000) {
			t.Fatalf("incorrect per-turn durations: %+v", replies)
		}
		for _, reply := range replies[2:] {
			if _, ok := reply["duration_ms"]; ok {
				t.Fatal("unmeasured or negative duration was invented")
			}
		}
	}
}
