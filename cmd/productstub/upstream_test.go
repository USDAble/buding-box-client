package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// These tests pin the optional real-model upstream added on 2026-09-14.
//
// WHY THE DEV TOOL AND NOT THE LIBRARY. clienttest mirrors the platform
// contract, and every fixture in it is deterministic on purpose: a nail that
// asserted a reply text could not survive a fixture that sometimes talked to a
// real model. Forwarding is therefore NOT a property of the stand-in - it is a
// property of the hand-run process, which is the only consumer that wants a
// real conversation. The library gains one accessor (Authorised) and nothing
// else.
//
// The three properties that matter, in order of how expensive they are to get
// wrong:
//
//  1. OFF BY DEFAULT. With no upstream configured, the completions route must
//     behave exactly as before and make NO outbound request. A dev tool that
//     silently acquired a network dependency would make every hand walkthrough
//     unexplainable.
//  2. THE PLATFORM TOKEN NEVER LEAVES. The client authenticates to the gateway
//     with the platform's access token (GatewayEndpoint passes it as APIKey).
//     That token must be replaced with the provider's key before the request
//     goes out - forwarding it would hand a third party a live session token,
//     which is a worse version of the leak V-35 was about (需求基线 B4).
//  3. AUTHORISATION IS STILL ENFORCED. The upstream must not be reachable
//     without a token the stand-in issued, or the process becomes a local open
//     proxy that spends the user's provider credits.

// signIn runs the real two-step sign-in the desktop build runs and returns the
// access token it issued. A hand-planted token would test the map lookup rather
// than the path the product takes.
func signIn(t *testing.T, h http.Handler) string {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/sms/send",
		strings.NewReader(`{"phone":"13800001234","purpose":"login"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("sms/send = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(fmt.Sprintf(
			`{"phone":"13800001234","code":%q,"nickname":"tester","activationCode":%q,"boxCode":%q}`,
			clienttest.FixtureSMSCode, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode))))
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("login body is not the documented envelope: %v", err)
	}
	if envelope.Data.AccessToken == "" {
		t.Fatalf("login issued no accessToken: %s", rec.Body.String())
	}
	return envelope.Data.AccessToken
}

// fakeProvider is a real HTTP endpoint that records what arrived and answers
// with a stream of its own, so "the bytes the client sees came from upstream"
// is checkable rather than inferred.
type fakeProvider struct {
	srv *httptest.Server

	mu      sync.Mutex
	hits    int
	auth    string
	model   string
	stream  bool
	lastRaw map[string]any
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	p := &fakeProvider{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		p.mu.Lock()
		p.hits++
		p.auth = r.Header.Get("Authorization")
		if m, ok := body["model"].(string); ok {
			p.model = m
		}
		if s, ok := body["stream"].(bool); ok {
			p.stream = s
		}
		p.lastRaw = body
		p.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		for _, part := range []string{"[real provider] ", "hello ", "from DeepSeek"} {
			frame := map[string]any{
				"id": "chatcmpl-real", "object": "chat.completion.chunk",
				"choices": []any{map[string]any{
					"index": 0, "delta": map[string]any{"content": part}, "finish_reason": nil,
				}},
			}
			buf, _ := json.Marshal(frame)
			fmt.Fprintf(w, "data: %s\n\n", buf)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeProvider) snapshot() (hits int, auth, model string, stream bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits, p.auth, p.model, p.stream
}

// turnBody is a streaming completions request shaped like the one the gateway
// sender sends: the catalog id in the model field, because that is what the
// product passes (L-C3a strips the endpoint prefix and forwards the bare id).
func turnBody() string {
	return `{"model":"buding-privacy-1","stream":true,"messages":[{"role":"user","content":"hi"}]}`
}

func postCompletions(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(turnBody()))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// 1. OFF BY DEFAULT. This is the nail that keeps every existing walkthrough
// honest: with no -upstream, nothing leaves the machine and the canned reply is
// still the one the automated nails assert on.
func TestWithoutAnUpstreamNothingLeavesTheMachine(t *testing.T) {
	provider := newFakeProvider(t)
	stub := clienttest.New()
	token := signIn(t, stub.Handler())

	h := withUpstreamGateway(stub.Handler(), stub, gatewayFlags{})
	rec := postCompletions(t, h, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("turn = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "[stand-in gateway]") {
		t.Fatalf("body = %q, want the canned fixture reply", rec.Body.String())
	}
	if hits, _, _, _ := provider.snapshot(); hits != 0 {
		t.Fatalf("upstream hits = %d, want 0 (an unconfigured proxy must not dial anything)", hits)
	}
}

// 2. THE TOKEN IS REPLACED. The client signed its turn with the platform access
// token; the provider must never see it.
func TestThePlatformTokenNeverReachesTheProvider(t *testing.T) {
	provider := newFakeProvider(t)
	stub := clienttest.New()
	token := signIn(t, stub.Handler())

	h := withUpstreamGateway(stub.Handler(), stub, gatewayFlags{
		baseURL: provider.srv.URL, apiKey: "sk-provider-key", model: "deepseek-chat",
	})
	rec := postCompletions(t, h, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("turn = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	hits, auth, model, stream := provider.snapshot()
	if hits != 1 {
		t.Fatalf("upstream hits = %d, want exactly 1", hits)
	}
	if auth == "Bearer "+token {
		t.Fatalf("upstream received the platform token %q: the provider must get its own key", token)
	}
	if auth != "Bearer sk-provider-key" {
		t.Fatalf("upstream Authorization = %q, want the configured provider key", auth)
	}
	if model != "deepseek-chat" {
		t.Fatalf("upstream model = %q, want the rewritten real model id (the catalog id is a fixture)", model)
	}
	if !stream {
		t.Fatalf("stream = false, want the client's streaming flag preserved")
	}
}

// The client must receive what the provider sent - otherwise the hand
// walkthrough would be showing the fixture's text and calling it a real model.
func TestTheReplyComesFromTheProvider(t *testing.T) {
	provider := newFakeProvider(t)
	stub := clienttest.New()
	token := signIn(t, stub.Handler())

	h := withUpstreamGateway(stub.Handler(), stub, gatewayFlags{
		baseURL: provider.srv.URL, apiKey: "sk-provider-key", model: "deepseek-chat",
	})
	rec := postCompletions(t, h, token)

	body := rec.Body.String()
	if !strings.Contains(body, "[real provider]") || !strings.Contains(body, "from DeepSeek") {
		t.Fatalf("body = %q, want the provider's chunks passed through", body)
	}
	if strings.Contains(body, "[stand-in gateway]") {
		t.Fatalf("body = %q, want no fixture text mixed in", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want the provider's SSE type preserved", ct)
	}
}

// 3. AUTHORISATION IS STILL ENFORCED, and the provider is not dialled at all
// when it fails. Without this the process is an open proxy on loopback that
// spends the operator's credits.
func TestAnUnauthorisedTurnDoesNotReachTheProvider(t *testing.T) {
	provider := newFakeProvider(t)
	stub := clienttest.New()
	signIn(t, stub.Handler()) // issue a token, then decline to send it

	h := withUpstreamGateway(stub.Handler(), stub, gatewayFlags{
		baseURL: provider.srv.URL, apiKey: "sk-provider-key", model: "deepseek-chat",
	})
	rec := postCompletions(t, h, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("turn without a token = %d, want 401", rec.Code)
	}
	if hits, _, _, _ := provider.snapshot(); hits != 0 {
		t.Fatalf("upstream hits = %d, want 0 (refused before any dial)", hits)
	}
}

// A provider failure must reach the operator as the provider's own words. A
// 400 from DeepSeek naming the model is the answer to "why is nothing
// happening", so it is passed through rather than replaced with a generic
// message.
func TestAProviderFailureIsPassedThroughWithItsStatus(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Model Not Exist"}}`))
	}))
	t.Cleanup(failing.Close)

	stub := clienttest.New()
	token := signIn(t, stub.Handler())
	h := withUpstreamGateway(stub.Handler(), stub, gatewayFlags{
		baseURL: failing.URL, apiKey: "sk-provider-key",
	})
	rec := postCompletions(t, h, token)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("turn = %d, want 400 from the provider", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Model Not Exist") {
		t.Fatalf("body = %q, want the provider's message", rec.Body.String())
	}
}

// The base URL takes both shapes the rest of the codebase tolerates: the
// provider's own client trims a trailing /v1 before appending the path, so an
// operator who copies "https://api.deepseek.com/v1" must not get /v1/v1.
func TestTheBaseURLAcceptsEitherShape(t *testing.T) {
	for _, base := range []string{
		"https://api.deepseek.com",
		"https://api.deepseek.com/v1",
		"https://api.deepseek.com/v1/",
		"https://api.deepseek.com/",
	} {
		got := completionsURL(base)
		if !strings.HasSuffix(got, "/v1/chat/completions") {
			t.Errorf("completionsURL(%q) = %q, want it to end with /v1/chat/completions", base, got)
		}
		if strings.Contains(got, "/v1/v1") {
			t.Errorf("completionsURL(%q) = %q, want no doubled version segment", base, got)
		}
	}
}

// Asking the fixture whether a token is live is the fixture's business: it owns
// the token table. A dev tool that guessed would have to duplicate the rule.
func TestAuthorisedReportsWhichTokensTheFixtureIssued(t *testing.T) {
	stub := clienttest.New()
	token := signIn(t, stub.Handler())

	withToken := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	withToken.Header.Set("Authorization", "Bearer "+token)
	if !stub.Authorised(withToken) {
		t.Fatalf("Authorised = false for a token the fixture just issued")
	}

	bare := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if stub.Authorised(bare) {
		t.Fatalf("Authorised = true without a token")
	}
}

// -tool and -upstream are mutually exclusive: -tool makes the fixture invent a
// tool call, while -upstream hands the turn to a real model that decides for
// itself. Silently honouring one would make the walkthrough demonstrate
// something the operator did not ask for.
func TestToolAndUpstreamFlagsConflict(t *testing.T) {
	err := validateFlags("terminal", "https://api.deepseek.com/v1")
	if err == nil {
		t.Fatalf("validateFlags = nil, want a refusal that names the conflict")
	}
	if !strings.Contains(err.Error(), "-tool") || !strings.Contains(err.Error(), "-upstream") {
		t.Fatalf("error = %q, want both flags named", err.Error())
	}
	if err := validateFlags("", "https://api.deepseek.com/v1"); err != nil {
		t.Fatalf("validateFlags with only -upstream = %v, want nil", err)
	}
	if err := validateFlags("terminal", ""); err != nil {
		t.Fatalf("validateFlags with only -tool = %v, want nil", err)
	}
}

// The banner must state whether the turn will reach a real model, and warn when
// the fixture's own model ids would be handed to a real provider - that 400 is
// otherwise the least explicable failure in the whole walkthrough.
func TestTheBannerSaysWhereTurnsGo(t *testing.T) {
	lines := describeGateway(gatewayFlags{baseURL: "https://api.deepseek.com/v1", model: "deepseek-chat"})
	if !strings.Contains(lines, "https://api.deepseek.com/v1") {
		t.Fatalf("banner = %q, want the upstream named", lines)
	}
	if !strings.Contains(lines, "deepseek-chat") {
		t.Fatalf("banner = %q, want the model rewrite named", lines)
	}

	passthrough := describeGateway(gatewayFlags{baseURL: "https://api.deepseek.com/v1"})
	if !strings.Contains(passthrough, "buding-") {
		t.Fatalf("banner = %q, want a warning that fixture ids will be rejected without -model", passthrough)
	}

	off := describeGateway(gatewayFlags{})
	if !strings.Contains(off, "OFF") && !strings.Contains(off, "canned") {
		t.Fatalf("banner = %q, want the off state stated", off)
	}
}
