package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/config"
	"github.com/open-octo/octo-agent/internal/provider"
	"github.com/open-octo/octo-agent/internal/provider/anthropic"
	"github.com/open-octo/octo-agent/internal/provider/openai"
)

func TestSender_PassesRequestThrough(t *testing.T) {
	fake := &mockProvider{reply: provider.Response{
		Content: "pong", Model: "m", StopReason: "end_turn",
	}}
	a := agent.New(sender{p: fake}, "claude-haiku-4-5-20251001")
	reply, err := a.Turn(context.Background(), "ping")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "pong" {
		t.Errorf("Content = %q, want pong", reply.Content)
	}
	if fake.gotReq.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("model passed through = %q", fake.gotReq.Model)
	}
	if len(fake.gotReq.Messages) != 1 || fake.gotReq.Messages[0].Content != "ping" {
		t.Errorf("messages passed through = %+v", fake.gotReq.Messages)
	}
}

// NewSender must derive a thinking budget from ReasoningEffort when no explicit
// ThinkingBudget is given (the server path), so Anthropic-protocol legacy models
// (Kimi-for-coding, older Claude) actually enable thinking. An explicit budget
// wins.
func TestNewSender_DerivesThinkingBudgetFromEffort(t *testing.T) {
	cases := []struct {
		name          string
		effort        string
		explicitBudg  int
		wantThinkBudg int
	}{
		{"effort max, no explicit budget", "max", 0, 64000},
		{"effort high, no explicit budget", "high", 0, 32768},
		{"effort off → no thinking", "", 0, 0},
		{"explicit budget wins over effort", "low", 50000, 50000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := NewSender(SenderOptions{
				Provider:        "anthropic",
				APIKey:          "test-key",
				ReasoningEffort: c.effort,
				ThinkingBudget:  c.explicitBudg,
			})
			if err != nil {
				t.Fatalf("NewSender: %v", err)
			}
			got := s.(sender).thinkingBudget
			if got != c.wantThinkBudg {
				t.Errorf("thinkingBudget = %d, want %d", got, c.wantThinkBudg)
			}
		})
	}
}

// TestSender_LowEffort_CapsReasoningRegardlessOfOriginal guards the fix for a
// confirmed production failure: a session running reasoning_effort "max" paid
// the model's full reasoning budget for Suggest's throwaway call, reliably
// exceeding its timeout. sender.LowEffort() must always cap to "low" — never
// inherit whatever the session was actually configured with — and must leave
// the original sender (and its caller-visible reasoningEffort/thinkingBudget)
// untouched, since it's a value receiver returning a modified copy, not a
// mutation.
func TestSender_LowEffort_CapsReasoningRegardlessOfOriginal(t *testing.T) {
	for _, original := range []string{"", "low", "medium", "high", "xhigh", "max"} {
		t.Run("from "+original, func(t *testing.T) {
			fake := &mockProvider{reply: provider.Response{Content: "ok"}}
			s := sender{p: fake, reasoningEffort: original, thinkingBudget: AnthropicThinkingBudget(original)}

			low := s.LowEffort()
			if _, err := low.SendMessages(context.Background(), "m", "", []agent.Message{agent.NewUserMessage("hi")}, 0); err != nil {
				t.Fatalf("SendMessages: %v", err)
			}
			if fake.gotReq.ReasoningEffort != "low" {
				t.Errorf("ReasoningEffort sent = %q, want %q", fake.gotReq.ReasoningEffort, "low")
			}
			if want := AnthropicThinkingBudget("low"); fake.gotReq.ThinkingBudget != want {
				t.Errorf("ThinkingBudget sent = %d, want %d", fake.gotReq.ThinkingBudget, want)
			}

			// The original sender must be unaffected (value receiver).
			if s.reasoningEffort != original {
				t.Errorf("original sender's reasoningEffort mutated: got %q, want %q", s.reasoningEffort, original)
			}
		})
	}
}

// TestSender_NoReasoning_DisablesReasoningRegardlessOfOriginal guards
// GenerateTitle's separate requirement: title generation needs no reasoning at
// all, so NoReasoning() must always disable it outright regardless of the
// session's configured effort, without mutating the original sender.
func TestSender_NoReasoning_DisablesReasoningRegardlessOfOriginal(t *testing.T) {
	for _, original := range []string{"", "low", "medium", "high", "xhigh", "max"} {
		t.Run("from "+original, func(t *testing.T) {
			fake := &mockProvider{reply: provider.Response{Content: "ok"}}
			s := sender{p: fake, reasoningEffort: original, thinkingBudget: AnthropicThinkingBudget(original)}

			no := s.NoReasoning()
			if _, err := no.SendMessages(context.Background(), "m", "", []agent.Message{agent.NewUserMessage("hi")}, 0); err != nil {
				t.Fatalf("SendMessages: %v", err)
			}
			if fake.gotReq.ReasoningEffort != "" {
				t.Errorf("ReasoningEffort sent = %q, want %q (off)", fake.gotReq.ReasoningEffort, "")
			}
			if fake.gotReq.ThinkingBudget != 0 {
				t.Errorf("ThinkingBudget sent = %d, want %d", fake.gotReq.ThinkingBudget, 0)
			}

			// The original sender must be unaffected (value receiver).
			if s.reasoningEffort != original {
				t.Errorf("original sender's reasoningEffort mutated: got %q, want %q", s.reasoningEffort, original)
			}
		})
	}
}

// TestSender_ImplementsLowEffortSender is a compile-time-adjacent guard: if
// sender's method set ever drifts (e.g. LowEffort renamed or given a pointer
// receiver that breaks the value-type assertion agent.go relies on),
// GenerateTitle/Suggest would silently stop capping effort with no test
// failure elsewhere to catch it.
func TestSender_ImplementsLowEffortSender(t *testing.T) {
	var _ agent.LowEffortSender = sender{}
}

func TestSender_NilProvider(t *testing.T) {
	s := sender{p: nil}
	if _, err := s.SendMessages(context.Background(), "m", "", nil, 0); err == nil {
		t.Error("expected error for nil provider")
	}
}

func TestSender_ProviderError_Surfaces(t *testing.T) {
	fake := &mockProvider{err: errors.New("upstream boom")}
	s := sender{p: fake}
	_, err := s.SendMessages(context.Background(), "m", "", []agent.Message{agent.NewUserMessage("hi")}, 0)
	if err == nil || !strings.Contains(err.Error(), "upstream boom") {
		t.Errorf("expected upstream error, got: %v", err)
	}
}

// mockProvider implements provider.Provider for tests.
type mockProvider struct {
	reply  provider.Response
	err    error
	gotReq provider.Request
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Send(_ context.Context, req provider.Request) (provider.Response, error) {
	m.gotReq = req
	return m.reply, m.err
}

// streamingMockProvider also implements provider.StreamingProvider so we can
// verify sender.StreamMessages picks the streaming path when the underlying
// provider supports it.
type streamingMockProvider struct {
	mockProvider
	deltas       []string
	thinkDeltas  []string
	streamReply  provider.Response
	streamCalled bool
	// onThinkingSet records whether the caller wired an OnThinking callback —
	// sender drops it when reasoning display is off.
	onThinkingSet bool
}

func (m *streamingMockProvider) SendStream(_ context.Context, req provider.Request, cb provider.StreamCallbacks) (provider.Response, error) {
	m.streamCalled = true
	m.gotReq = req
	m.onThinkingSet = cb.OnThinking != nil
	for _, d := range m.thinkDeltas {
		if cb.OnThinking != nil {
			cb.OnThinking(d)
		}
	}
	for _, d := range m.deltas {
		if cb.OnText != nil {
			cb.OnText(d)
		}
	}
	if m.err != nil {
		return provider.Response{}, m.err
	}
	return m.streamReply, nil
}

func TestSender_ReasoningSink_Gating(t *testing.T) {
	cases := []struct {
		name          string
		showReasoning bool
		onThinking    func(string)
		wantForwarded bool // whether the provider received a non-nil OnThinking
	}{
		{"on + handler → forwarded", true, func(string) {}, true},
		{"off → dropped", false, func(string) {}, false},
		{"on but nil handler → nil", true, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &streamingMockProvider{
				thinkDeltas: []string{"reasoning"},
				streamReply: provider.Response{Content: "ok", StopReason: "end_turn"},
			}
			s := sender{p: fake, showReasoning: c.showReasoning}
			_, err := s.StreamMessages(
				context.Background(), "m", "",
				[]agent.Message{agent.NewUserMessage("hi")}, 0,
				func(string) {},
				c.onThinking,
			)
			if err != nil {
				t.Fatal(err)
			}
			if fake.onThinkingSet != c.wantForwarded {
				t.Errorf("provider OnThinking set = %v, want %v", fake.onThinkingSet, c.wantForwarded)
			}
		})
	}
}

func TestSender_StreamingPathPreferred(t *testing.T) {
	fake := &streamingMockProvider{
		deltas:      []string{"hi ", "there"},
		streamReply: provider.Response{Content: "hi there", Model: "m", StopReason: "end_turn"},
	}
	s := sender{p: fake}

	var got []string
	reply, err := s.StreamMessages(
		context.Background(), "m", "",
		[]agent.Message{agent.NewUserMessage("hi")}, 0,
		func(d string) { got = append(got, d) },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !fake.streamCalled {
		t.Error("expected SendStream to be called when provider supports it")
	}
	if reply.Content != "hi there" {
		t.Errorf("Content = %q", reply.Content)
	}
	if len(got) != 2 || got[0] != "hi " || got[1] != "there" {
		t.Errorf("chunks = %v", got)
	}
}

func TestSender_StreamingFallback_NonStreamingProvider(t *testing.T) {
	// mockProvider only implements provider.Provider — not StreamingProvider.
	// sender.StreamMessages must fall back to Send and synthesise a single
	// onChunk call with the full content.
	fake := &mockProvider{reply: provider.Response{Content: "buffered"}}
	s := sender{p: fake}

	var got []string
	reply, err := s.StreamMessages(
		context.Background(), "m", "",
		[]agent.Message{agent.NewUserMessage("hi")}, 0,
		func(d string) { got = append(got, d) },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "buffered" {
		t.Errorf("Content = %q", reply.Content)
	}
	if len(got) != 1 || got[0] != "buffered" {
		t.Errorf("fallback should emit one chunk with full content; got %v", got)
	}
}

// TestBuildClient_PassesHeadersThrough verifies that the headers argument
// reaches the constructed provider client for both the anthropic and openai
// protocol branches.
func TestBuildClient_PassesHeadersThrough(t *testing.T) {
	headers := map[string]string{"X-Tenant-Id": "tenant-42"}

	p, err := buildClient(ProviderAnthropic, "test-key", "", "", headers, nil)
	if err != nil {
		t.Fatalf("buildClient(anthropic): %v", err)
	}
	ac, ok := p.(*anthropic.Client)
	if !ok {
		t.Fatalf("buildClient(anthropic) returned %T, want *anthropic.Client", p)
	}
	if ac.Headers["X-Tenant-Id"] != "tenant-42" {
		t.Errorf("anthropic client Headers = %+v, want X-Tenant-Id=tenant-42", ac.Headers)
	}

	p, err = buildClient(ProviderOpenAI, "test-key", "", "", headers, nil)
	if err != nil {
		t.Fatalf("buildClient(openai): %v", err)
	}
	oc, ok := p.(*openai.Client)
	if !ok {
		t.Fatalf("buildClient(openai) returned %T, want *openai.Client", p)
	}
	if oc.Headers["X-Tenant-Id"] != "tenant-42" {
		t.Errorf("openai client Headers = %+v, want X-Tenant-Id=tenant-42", oc.Headers)
	}
}

// TestEntryConnectionOverrides_MatchingProviderAppliesBoth verifies that when
// the resolved provider matches the config entry's own provider, every
// connection setting is carried through.
func TestEntryConnectionOverrides_MatchingProviderAppliesBoth(t *testing.T) {
	entry := config.ModelEntry{
		Provider:       "custom",
		Protocol:       "anthropic",
		Headers:        map[string]string{"X-Tenant-Id": "abc"},
		RPM:            8,
		MaxConcurrency: 2,
	}
	conn := EntryConnectionOverrides("custom", entry)
	if conn.Protocol != "anthropic" {
		t.Errorf("protocol = %q, want anthropic", conn.Protocol)
	}
	if conn.Headers["X-Tenant-Id"] != "abc" {
		t.Errorf("headers = %+v, want X-Tenant-Id=abc", conn.Headers)
	}
	if conn.RPM != 8 || conn.MaxConcurrency != 2 {
		t.Errorf("rpm/max_concurrency = %d/%d, want 8/2", conn.RPM, conn.MaxConcurrency)
	}
}

// TestEntryConnectionOverrides_MismatchedProviderClearsBoth is the regression
// test for the bug caught in review: a resolved provider that differs from
// the config entry's own provider (e.g. a --provider flag or OCTO_PROVIDER
// env var picked a different vendor than the config's default entry, while
// still reusing that entry's API key) must NOT carry the entry's Protocol or
// Headers through — those belong to a different endpoint, and Headers in
// particular may override Authorization/x-api-key, so leaking it would send
// that value to the wrong provider's API. The rate limits go the same way:
// they bound a quota the mismatched vendor doesn't share.
func TestEntryConnectionOverrides_MismatchedProviderClearsBoth(t *testing.T) {
	entry := config.ModelEntry{
		Provider:       "custom",
		Protocol:       "anthropic",
		Headers:        map[string]string{"Authorization": "should-not-leak"},
		RPM:            8,
		MaxConcurrency: 2,
	}
	conn := EntryConnectionOverrides("openai", entry)
	if conn.Protocol != "" {
		t.Errorf("protocol = %q, want empty (provider mismatch)", conn.Protocol)
	}
	if conn.Headers != nil {
		t.Errorf("headers = %+v, want nil (provider mismatch must not leak entry's headers)", conn.Headers)
	}
	if conn.RPM != 0 || conn.MaxConcurrency != 0 {
		t.Errorf("rpm/max_concurrency = %d/%d, want 0/0 (provider mismatch)", conn.RPM, conn.MaxConcurrency)
	}
}

func TestNewSender_UnknownProvider(t *testing.T) {
	if _, err := NewSender(SenderOptions{Provider: "nope", APIKey: "k"}); err == nil {
		t.Error("expected error for unknown provider")
	}
}

// OCTO-FORK: 未知流字段的报告必须有人装（`待解决问题.md` D-002 第 3 条 / `需求基线` V-90）
// — see dev-docs-usdable/需求/20260911/中台接口清单.md §6.2
//
// The gateway installs this callback, so a stream chunk carrying a field this
// build does not model (the 中台 gateway's `retract`, whose semantics are still
// unpinned - 待解决问题.md D-002 / 需求基线 V-90) must leave a trace instead of
// being dropped in silence. Before the wiring these tests pin, the report existed
// at the provider layer but no caller installed it, so the user kept reading
// withdrawn content with nothing anywhere saying so.
//
// The stream still SUCCEEDS in both cases: this is a report, not a refusal.
// Failing would be inventing a protocol for an event nobody has specified yet.
func TestSender_ReportsUnmodelledChunkFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `data: {"id":"c1","choices":[{"index":0,"delta":{"content":"withdrawn text"}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"retract":{"reason":"safety_blocked"}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	// The same shape GatewayEndpoint.Sender builds: custom endpoint, openai
	// protocol. A sender built any other way would not prove the gateway path.
	s, err := NewSender(SenderOptions{
		Provider: ProviderCustom,
		Protocol: "openai",
		APIKey:   "token",
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	logged := captureLog(t)
	streamer, ok := s.(sender)
	if !ok {
		t.Fatalf("NewSender returned %T, want the in-package sender", s)
	}
	reply, err := streamer.StreamMessages(
		context.Background(), "buding-cloud-pro", "",
		[]agent.Message{agent.NewUserMessage("hi")}, 0,
		func(string) {}, nil,
	)
	if err != nil {
		t.Fatalf("StreamMessages: %v", err)
	}
	if reply.Content != "withdrawn text" {
		t.Errorf("Content = %q, want the text that streamed before the unknown field", reply.Content)
	}
	if !strings.Contains(logged(), "retract") {
		t.Errorf("log = %q, want it to name the unmodelled field %q: an unknown top-level field was dropped in silence", logged(), "retract")
	}
}

// The reverse nail: a stream real OpenAI would send must not produce the report.
// Without this, a build that logged on every turn would pass the test above.
func TestSender_StandardStreamLogsNoUnmodelledFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","created":1767225600,"model":"m","choices":[{"index":0,"delta":{"content":"hi"}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","created":1767225600,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	s, err := NewSender(SenderOptions{
		Provider: ProviderCustom,
		Protocol: "openai",
		APIKey:   "token",
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	logged := captureLog(t)
	streamer, ok := s.(sender)
	if !ok {
		t.Fatalf("NewSender returned %T, want the in-package sender", s)
	}
	if _, err := streamer.StreamMessages(
		context.Background(), "buding-cloud-pro", "",
		[]agent.Message{agent.NewUserMessage("hi")}, 0,
		func(string) {}, nil,
	); err != nil {
		t.Fatalf("StreamMessages: %v", err)
	}
	if got := logged(); got != "" {
		t.Errorf("log = %q, want nothing: every key in that chunk is a standard one", got)
	}
}

// The tools path is the one the agent loop actually streams through, so the
// report has to be installed there too — a wiring that covered only the
// tool-less convenience method would leave the product path silent.
func TestSender_ReportsUnmodelledChunkFields_WithTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `data: {"id":"c1","choices":[{"index":0,"delta":{"content":"withdrawn text"}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"retract":{"reason":"safety_blocked"}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	s, err := NewSender(SenderOptions{
		Provider: ProviderCustom,
		Protocol: "openai",
		APIKey:   "token",
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	streamer, ok := s.(sender)
	if !ok {
		t.Fatalf("NewSender returned %T, want the in-package sender", s)
	}

	logged := captureLog(t)
	if _, err := streamer.StreamMessagesWithTools(
		context.Background(), "buding-cloud-pro", "",
		[]agent.Message{agent.NewUserMessage("hi")}, 0,
		[]agent.ToolDefinition{{Name: "terminal"}},
		func(string) {}, func(string, string, string) {}, nil,
	); err != nil {
		t.Fatalf("StreamMessagesWithTools: %v", err)
	}
	if !strings.Contains(logged(), "retract") {
		t.Errorf("log = %q, want it to name the unmodelled field %q on the tools path too", logged(), "retract")
	}
}

// captureLog redirects the standard logger into a buffer and returns a reader of
// what has been written so far. A file-level logger rather than a field on
// sender because reportUnmodelledChunkFields is package-level: the callback is
// installed by the two streaming call sites, not held per sender.
func captureLog(t *testing.T) func() string {
	t.Helper()
	var buf strings.Builder
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return func() string { return buf.String() }
}

func TestTestConnection_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", auth)
		}
		bodyBytes, _ := io.ReadAll(r.Body)
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			MaxTokens int `json:"max_tokens"`
		}
		_ = json.Unmarshal(bodyBytes, &req)
		if req.Model != "gpt-4o-mini" {
			t.Errorf("model = %q, want gpt-4o-mini", req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hi" {
			t.Errorf("messages = %+v, want single user 'hi'", req.Messages)
		}
		if req.MaxTokens != 1 {
			t.Errorf("max_tokens = %d, want 1", req.MaxTokens)
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := TestConnection(ctx, ProviderOpenAI, "test-key", srv.URL, "gpt-4o-mini", ""); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
}

func TestTestConnection_AuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := TestConnection(ctx, ProviderOpenAI, "bad-key", srv.URL, "gpt-4o-mini", "")
	if err == nil {
		t.Fatal("expected error for bad key")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention 401: %v", err)
	}
}

func TestTestConnection_InvalidModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"model not found","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := TestConnection(ctx, ProviderOpenAI, "test-key", srv.URL, "unknown-model", "")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestTestConnection_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response to trigger timeout
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := TestConnection(ctx, ProviderOpenAI, "test-key", srv.URL, "gpt-4o-mini", "")
	if err == nil {
		t.Fatal("expected error for timeout")
	}
}

func TestTestConnection_EmptyKey(t *testing.T) {
	ctx := context.Background()
	if err := TestConnection(ctx, ProviderOpenAI, "", "http://localhost", "x", ""); err == nil {
		t.Error("expected error for empty key")
	}
}

func TestTestConnection_UnknownProvider(t *testing.T) {
	ctx := context.Background()
	if err := TestConnection(ctx, "nope", "k", "http://localhost", "x", ""); err == nil {
		t.Error("expected error for unknown provider")
	}
}
