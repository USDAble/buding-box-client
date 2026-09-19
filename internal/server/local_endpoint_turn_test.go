package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/config"
)

// OCTO-FORK: prove the development-only local-model path as one real chat
// flow: endpoint CRUD -> composite session binding -> OpenAI-compatible SSE ->
// persisted reply. This guards the test profile promised by the privacy plan.
func TestLocalEndpointCRUDCanServeAStreamingTurn(t *testing.T) {
	setTestHome(t)
	t.Setenv("CUSTOM_API_KEY", "")

	var upstreamCalls atomic.Int32
	var streamingTurnCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("local endpoint request = %s %s, want POST /v1/chat/completions", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want no credential for a keyless local endpoint", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read local endpoint request: %v", err)
			return
		}
		var request struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode local endpoint request: %v", err)
			return
		}
		if request.Model != "local-chat" {
			t.Errorf("request model = %q, want local-chat", request.Model)
		}
		if !request.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"local-suggestion","object":"chat.completion","model":"local-chat","choices":[{"index":0,"message":{"role":"assistant","content":"continue locally"},"finish_reason":"stop"}]}`)
			return
		}
		streamingTurnCalls.Add(1)
		if !strings.Contains(string(body), "hello from the local model") {
			t.Errorf("streaming request did not carry the user turn: %s", body)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"data: {\"id\":\"local-1\",\"object\":\"chat.completion.chunk\",\"model\":\"local-chat\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"+
				"data: {\"id\":\"local-1\",\"object\":\"chat.completion.chunk\",\"model\":\"local-chat\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"local \"}}]}\n\n"+
				"data: {\"id\":\"local-1\",\"object\":\"chat.completion.chunk\",\"model\":\"local-chat\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"reply\"}}]}\n\n"+
				"data: {\"id\":\"local-1\",\"object\":\"chat.completion.chunk\",\"model\":\"local-chat\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
				"data: [DONE]\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	gatewayCalls := 0
	srv := mustServer(t, Config{
		Addr:               "127.0.0.1:0",
		Tools:              false,
		GatewayModelPrefix: "buding-gateway::",
		GatewaySender: func(app.ReasoningTuning) (agent.Sender, error) {
			gatewayCalls++
			return &stubSender{}, nil
		},
	})
	srv.initWS()
	srv.turnRunning = make(map[string]bool)
	srv.steerQueues = make(map[string][]queuedTurn)
	srv.sessionAgents = make(map[string]*agent.Agent)

	createBody, err := json.Marshal(map[string]any{
		"id":       "local-e2e",
		"name":     "Local E2E",
		"provider": "custom",
		"base_url": upstream.URL + "/v1",
		"protocol": "openai",
		"models": []map[string]any{{
			"model":        "local-chat",
			"confidential": false,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	created := doJSON(t, srv, http.MethodPost, "/api/config/endpoints", string(createBody))
	if created.Code != http.StatusCreated {
		t.Fatalf("create local endpoint = %d: %s", created.Code, created.Body.String())
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load persisted endpoint: %v", err)
	}
	entry, ok := cfg.EntryByModel("local-e2e::local-chat")
	if !ok || entry.BaseURL != upstream.URL+"/v1" {
		t.Fatalf("persisted entry = (%+v, %v), want the local endpoint URL", entry, ok)
	}

	createdSession := doJSON(t, srv, http.MethodPost, "/api/sessions",
		`{"name":"Local E2E Chat","model":"local-e2e::local-chat"}`)
	if createdSession.Code != http.StatusOK {
		t.Fatalf("create local-model session = %d: %s", createdSession.Code, createdSession.Body.String())
	}
	var sessionResponse struct {
		Session struct {
			ID      string `json:"id"`
			ModelID string `json:"model_id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(createdSession.Body.Bytes(), &sessionResponse); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if sessionResponse.Session.ModelID != "local-e2e::local-chat" {
		t.Fatalf("session model_id = %q, want local-e2e::local-chat", sessionResponse.Session.ModelID)
	}

	sess, err := agent.LoadSession(sessionResponse.Session.ID)
	if err != nil {
		t.Fatalf("load local-model session: %v", err)
	}
	conn := subscribeConn(srv, sess.ID)
	srv.doAgentTurn(sess, "hello from the local model", nil, nil)
	var textDeltas []string
	for _, event := range turnEvents(t, conn, sess.ID) {
		if event["type"] == "text_delta" {
			if delta, ok := event["text"].(string); ok {
				textDeltas = append(textDeltas, delta)
			}
		}
	}
	if got := strings.Join(textDeltas, "|"); got != "local |reply" {
		t.Fatalf("browser text deltas = %q, want both upstream SSE chunks preserved", got)
	}
	drainForEvent(t, conn, func(event map[string]any) bool {
		return event["type"] == "next_message_suggestion"
	})
	if streamingTurnCalls.Load() != 1 {
		t.Fatalf("streaming local turn calls = %d, want 1", streamingTurnCalls.Load())
	}
	if upstreamCalls.Load() != 2 {
		t.Fatalf("local endpoint calls = %d, want the turn plus its follow-up suggestion", upstreamCalls.Load())
	}
	if gatewayCalls != 0 {
		t.Fatalf("gateway calls = %d, want 0 for a local-model session", gatewayCalls)
	}

	persisted, err := agent.LoadSession(sessionResponse.Session.ID)
	if err != nil {
		t.Fatalf("reload local-model session: %v", err)
	}
	if len(persisted.Messages) != 2 ||
		persisted.Messages[0].Content != "hello from the local model" ||
		persisted.Messages[1].Content != "local reply" {
		t.Fatalf("persisted messages = %+v, want the user turn and streamed reply", persisted.Messages)
	}
}
