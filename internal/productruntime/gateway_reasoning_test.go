package productruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

func TestGatewayReasoningPreservesEveryChoice(t *testing.T) {
	for _, effort := range []string{"default", "low", "medium", "high"} {
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
			sender, err := (GatewayEndpoint{Host: endpoint.URL, Tokens: signedIn("fixture"), ReasoningForModel: func(model string) (string, error) {
				if model != "actual-model" {
					t.Errorf("preference looked up for %q", model)
				}
				return effort, nil
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

type expiryToolExecutor func() (agent.ToolResult, error)

func (run expiryToolExecutor) Execute(context.Context, string, map[string]any) (agent.ToolResult, error) {
	return run()
}

func TestGatewayToolContinuationRenewsCatalogAfterLongTool(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()
	entry, _ := f.store.Load()
	policy, _ := entry.Envelope.DecodePolicy()
	model := policy.Catalog.Models[1].ID
	f.platform.RequestToolCall("read_file", `{"path":"notes.txt"}`)
	gateway := GatewayEndpoint{Host: f.baseURL, Tokens: signedIn(f.login.AccessToken), ReasoningForModel: f.rt.ModelReasoning, ValidateReasoning: f.rt.ValidateModelReasoning}
	sender, err := gateway.Sender(app.ReasoningTuning{ClientModelID: model})
	if err != nil {
		t.Fatal(err)
	}
	executions := 0
	executor := expiryToolExecutor(func() (agent.ToolResult, error) {
		executions++
		// Advance the cached lease as if this tool outlasted its TTL, without
		// sleeping in a test or changing the model/effort captured by the turn.
		entry.CatalogVersion = "2026-09-13.9"
		entry.ExpiresAt = time.Now().Add(-time.Second)
		if err := f.store.Put(entry); err != nil {
			return agent.ToolResult{}, err
		}
		f.platform.SetCatalogVersion("2026-09-13.10")
		return agent.ToolResult{Text: "file result"}, nil
	})
	a := agent.New(sender, model)
	if _, err := a.RunStream(context.Background(), "read it", toolDefs(), executor, nil); err != nil {
		t.Fatalf("continuation after TTL: %v", err)
	}
	if executions != 1 || f.platform.CatalogInitialCount() != 2 {
		t.Fatalf("tools=%d catalog requests=%d", executions, f.platform.CatalogInitialCount())
	}
}

func TestGatewayToolLoopKeepsChoiceAndNextTurnReadsNewPreference(t *testing.T) {
	standin := clienttest.New()
	var selected atomic.Value
	selected.Store("high")
	var received []string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			body, _ := io.ReadAll(r.Body)
			var input map[string]any
			if err := json.Unmarshal(body, &input); err != nil {
				t.Error(err)
			}
			received = append(received, input["reasoning_effort"].(string))
			selected.Store("low") // an edit after round one affects only the next user turn.
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		standin.Handler().ServeHTTP(w, r)
	}))
	defer endpoint.Close()
	token := signInAgainstStandIn(t, endpoint.URL)
	standin.RequestToolCall("read_file", `{"path":"notes.txt"}`)
	gateway := GatewayEndpoint{Host: endpoint.URL, Tokens: signedIn(token), ReasoningForModel: func(string) (string, error) { return selected.Load().(string), nil }, ValidateReasoning: func(_ context.Context, model, effort string) error {
		if model != nailsModel || (effort != "high" && effort != "low") {
			t.Errorf("invalid snapshot %s %s", model, effort)
		}
		return nil
	}}
	sender, err := gateway.Sender(app.ReasoningTuning{ClientModelID: nailsModel})
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(sender, nailsModel)
	exec := &recordingExecutor{text: "file result"}
	if _, err := a.RunStream(context.Background(), "read it", toolDefs(), exec, nil); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 || !reflect.DeepEqual(received, []string{"high", "high"}) {
		t.Fatalf("calls=%d efforts=%v", exec.count(), received)
	}
	next, err := gateway.Sender(app.ReasoningTuning{ClientModelID: nailsModel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.SendMessages(context.Background(), nailsModel, "", []agent.Message{{Role: "user", Content: "hello"}}, 16); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(received, []string{"high", "high", "low"}) {
		t.Fatal(received)
	}
	gateway.ValidateReasoning = func(context.Context, string, string) error { return reasoningSelectionError{} }
	blocked, err := gateway.Sender(app.ReasoningTuning{ClientModelID: nailsModel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = blocked.SendMessages(context.Background(), nailsModel, "", nil, 16); agent.ErrorCodeOf(err) != "reasoning_selection_required" {
		t.Fatal(err)
	}
	if len(received) != 3 {
		t.Fatal("invalid choice reached network")
	}
	gateway.ReasoningForModel = func(string) (string, error) { return "high", reasoningSelectionError{} }
	if _, err = gateway.Sender(app.ReasoningTuning{ClientModelID: nailsModel}); agent.ErrorCodeOf(err) != "reasoning_selection_required" {
		t.Fatal(err)
	}
}
