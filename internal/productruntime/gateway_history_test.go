package productruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
)

func TestGatewayHistoryHeaderKeepsTurnBoundaryAcrossToolResultsAndSteering(t *testing.T) {
	var histories []string
	var models []string
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		histories = append(histories, r.Header.Get("X-Buding-History-Tool-Calls"))
		var request map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		var model string
		_ = json.Unmarshal(request["model"], &model)
		models = append(models, model)
		bodies = append(bodies, string(request["messages"]))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stubCompletion))
	}))
	defer server.Close()
	gateway := GatewayEndpoint{Host: server.URL, Tokens: signedIn("fixture")}
	ids := []string{"previous"}
	sender, err := gateway.Sender(app.ReasoningTuning{ClientModelID: "model-a", ClientHistoryToolCallIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	ids[0] = "changed-after-construction"
	if _, err := sender.SendMessages(context.Background(), "model-a", "", []agent.Message{{Role: agent.RoleUser, Content: "new request"}}, 16); err != nil {
		t.Fatal(err)
	}
	continuation := []agent.Message{
		{Role: agent.RoleAssistant, Blocks: []agent.ContentBlock{{Type: "tool_use", ID: "current", Name: "screenshot", Input: map[string]any{}}}},
		{Role: agent.RoleUser, Blocks: []agent.ContentBlock{{Type: "tool_result", ToolUseID: "current", Result: "image output"}, {Type: "image", Image: &agent.ImageData{MIMEType: "image/png", Data: []byte("image")}}}},
		{Role: agent.RoleUser, Content: "continue with the image"},
	}
	if _, err := sender.SendMessages(context.Background(), "model-a", "", continuation, 16); err != nil {
		t.Fatal(err)
	}
	next, err := gateway.Sender(app.ReasoningTuning{ClientModelID: "model-b", ClientHistoryToolCallIDs: []string{"previous", "current"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.SendMessages(context.Background(), "model-b", "", continuation, 16); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(histories, []string{`["previous"]`, `["previous"]`, `["previous","current"]`}) {
		t.Fatalf("history boundary changed: %v", histories)
	}
	if !reflect.DeepEqual(models, []string{"model-a", "model-a", "model-b"}) {
		t.Fatalf("models: %v", models)
	}
	if !strings.Contains(bodies[1], "image_url") || !strings.Contains(bodies[1], "continue with the image") || !strings.Contains(bodies[1], "current") {
		t.Fatalf("tool image/steer history was lost: %s", bodies[1])
	}
}

func TestGatewayHistoryHeaderIsExplicitAndRejectsOversizedSnapshots(t *testing.T) {
	if value, err := gatewayHistoryToolCalls(nil); err != nil || value != "[]" {
		t.Fatalf("empty snapshot: %q %v", value, err)
	}
	for _, ids := range [][]string{make([]string, 1025), {strings.Repeat("x", 64*1024)}} {
		called := false
		gateway := GatewayEndpoint{Host: "https://unused.invalid", Tokens: signedIn("fixture"), Ensure: func(context.Context) error { called = true; return nil }}
		if _, err := gateway.Sender(app.ReasoningTuning{ClientHistoryToolCallIDs: ids}); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("oversized history: %v", err)
		}
		if called {
			t.Fatal("oversized snapshot reached authentication/network")
		}
	}
}
