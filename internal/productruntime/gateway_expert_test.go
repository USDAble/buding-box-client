package productruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
)

func TestGatewayCarriesSelectedExpertPublication(t *testing.T) {
	var id, version, session string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id = r.Header.Get("X-Buding-Expert-ID")
		version = r.Header.Get("X-Buding-Expert-Version")
		session = r.Header.Get("X-Buding-Local-Session")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stubCompletion))
	}))
	defer server.Close()
	sender, err := (GatewayEndpoint{Host: server.URL, Tokens: signedIn("fixture")}).Sender(app.ReasoningTuning{ClientSessionID: "task", ClientAgentID: "platform:selected:7"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sender.SendMessages(context.Background(), "model", "", []agent.Message{{Role: "user", Content: "hello"}}, 16)
	if err != nil {
		t.Fatal(err)
	}
	if id != "selected" || version != "7" || session != "task" {
		t.Fatalf("headers = %q/%q/%q", id, version, session)
	}
}
