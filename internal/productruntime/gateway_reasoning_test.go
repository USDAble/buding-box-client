package productruntime

import (
	"context"
	"encoding/json"
	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayReasoningPreservesEveryChoice(t *testing.T) {
	for _, effort := range []string{"default", "off", "low", "high", "max"} {
		t.Run(effort, func(t *testing.T) {
			var received map[string]any
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(stubCompletion))
			}))
			defer endpoint.Close()
			sender, err := (GatewayEndpoint{Host: endpoint.URL, Tokens: signedIn("fixture"), ReasoningForModel: func(model string) string {
				if model != "actual-model" {
					t.Errorf("preference looked up for %q", model)
				}
				return effort
			}}).Sender(app.ReasoningTuning{ClientModelID: "actual-model", ReasoningEffort: "medium"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = sender.SendMessages(context.Background(), "actual-model", "", []agent.Message{{Role: "user", Content: "hello"}}, 16)
			if err != nil {
				t.Fatal(err)
			}
			if received["reasoning_effort"] != effort {
				t.Fatalf("received %v, want %s", received["reasoning_effort"], effort)
			}
			if _, ok := received["thinking"]; ok {
				t.Fatal("gateway hop must not guess provider thinking dialect")
			}
		})
	}
}
