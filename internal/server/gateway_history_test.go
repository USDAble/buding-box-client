package server

import (
	"reflect"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

func TestGatewayHistorySnapshotIsFixedPerTurnAndRebuiltAfterModelSwitch(t *testing.T) {
	srv, seen := gatewayTuningServer(t)
	sess := boundSession(testGatewayPrefix+"old-model", "")
	sess.Messages = []agent.Message{
		{Role: agent.RoleAssistant, Blocks: []agent.ContentBlock{{Type: "tool_use", ID: "previous", Name: "read_file"}, {Type: "tool_use", ID: "previous", Name: "read_file"}}},
		{Role: agent.RoleUser, Blocks: []agent.ContentBlock{{Type: "tool_result", ToolUseID: "previous", Result: "old result"}}},
	}
	srv.senderForSession(sess)
	if len(*seen) != 1 || !reflect.DeepEqual((*seen)[0].ClientHistoryToolCallIDs, []string{"previous"}) {
		t.Fatalf("initial snapshot: %+v", *seen)
	}
	// Tool output with images and steering input are user-role messages too.
	// Neither can move the snapshot boundary within this running turn.
	sess.Messages = append(sess.Messages,
		agent.Message{Role: agent.RoleAssistant, Blocks: []agent.ContentBlock{{Type: "tool_use", ID: "current", Name: "screenshot"}}},
		agent.Message{Role: agent.RoleUser, Blocks: []agent.ContentBlock{{Type: "tool_result", ToolUseID: "current", Result: "screenshot"}, {Type: "image"}}},
		agent.Message{Role: agent.RoleUser, Content: "please continue"},
	)
	if !reflect.DeepEqual((*seen)[0].ClientHistoryToolCallIDs, []string{"previous"}) {
		t.Fatal("running turn snapshot changed with messages")
	}
	sess.Model = testGatewayPrefix + "new-model"
	srv.senderForSession(sess)
	if len(*seen) != 2 || (*seen)[1].ClientModelID != "new-model" || !reflect.DeepEqual((*seen)[1].ClientHistoryToolCallIDs, []string{"previous", "current"}) {
		t.Fatalf("next model/turn snapshot: %+v", *seen)
	}
	if !reflect.DeepEqual((*seen)[0].ClientHistoryToolCallIDs, []string{"previous"}) {
		t.Fatal("next turn mutated the previous snapshot")
	}
}

func TestGatewayHistoryDoesNotInventIDsFromUserMessages(t *testing.T) {
	got := historyToolCallIDs([]agent.Message{
		{Role: agent.RoleUser, Blocks: []agent.ContentBlock{{Type: "tool_result", ToolUseID: "orphan"}, {Type: "tool_use", ID: "user-input"}}},
		{Role: agent.RoleAssistant, Blocks: []agent.ContentBlock{{Type: "tool_use"}, {Type: "text", ID: "not-a-call"}}},
	})
	if len(got) != 0 {
		t.Fatalf("invented history IDs: %v", got)
	}
}
