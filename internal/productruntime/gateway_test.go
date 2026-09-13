package productruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// These nails cover the one genuinely new piece of PR-5a (需求基线 C2): the
// gateway sender. Everything else about the turn is internal/provider/openai
// reused as-is, so what is asserted here is exactly the part that is not reused
// — which URL is dialed, which token is presented, and what happens when there
// is no token at all.
//
// They deliberately assert the request as it ARRIVED rather than the options
// struct that produced it: the options are this package's own choice, while the
// wire shape is the contract (中台交付包 §5.2).

// gatewayStub is a stand-in gateway that records what reached it.
type gatewayStub struct {
	mu     sync.Mutex
	path   string
	method string
	auth   string
	accept string
	body   string
	hits   int
	// stream overrides the default body for event-stream requests, so a nail can
	// make the gateway reason without a second stub type.
	stream string
}

// stubCompletion is the smallest valid completion the OpenAI client accepts, so
// a request shape can be asserted without the body being the thing under test.
const stubCompletion = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],` +
	`"usage":{"prompt_tokens":1,"completion_tokens":1}}`

// stubStream is the streaming equivalent: one content delta, then the sentinel.
const stubStream = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
	"data: [DONE]\n\n"

func (g *gatewayStub) start(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read first: the body has to be recorded before the response is written,
		// and several nails assert on the request rather than the reply.
		raw, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		g.path, g.method = r.URL.Path, r.Method
		g.auth, g.accept = r.Header.Get("Authorization"), r.Header.Get("Accept")
		g.body = string(raw)
		g.hits++
		streamBody := g.stream
		g.mu.Unlock()
		// Answer in whichever shape the caller asked for, so one stub serves both
		// paths and the Accept header is observable rather than assumed.
		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			if streamBody == "" {
				streamBody = stubStream
			}
			_, _ = w.Write([]byte(streamBody))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stubCompletion))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// requestBody is the last request's body, for nails about what the client sent
// rather than how it dialled.
func (g *gatewayStub) requestBody() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.body
}

func (g *gatewayStub) seen() (path, method, auth, accept string, hits int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.path, g.method, g.auth, g.accept, g.hits
}

func signedIn(token string) *productclient.CredentialHolder {
	h := &productclient.CredentialHolder{}
	h.Set(productclient.Credentials{AccessToken: token, RefreshToken: "refresh-not-used-here"})
	return h
}

// reasoningOff is the tuning these dial-shape nails pass.
//
// It is the zero value on purpose: it matches what the factory did before
// PR-5b1 wired the preferences in (no effort requested, no trace surfaced), so
// these nails keep asserting only the thing they are about — which URL is
// dialed, which token is presented — while the reasoning half is nailed in
// gateway_reasoning_test.go.
var reasoningOff = app.ReasoningTuning{}

// run dials the gateway once through the sender the factory builds.
func run(t *testing.T, g GatewayEndpoint) error {
	t.Helper()
	sender, err := g.Sender(reasoningOff)
	if err != nil {
		return err
	}
	_, err = sender.SendMessages(context.Background(), "sys", "buding-privacy-1",
		[]agent.Message{{Role: "user", Content: "hi"}}, 16)
	return err
}

// The path is what the provider builds from the host, and it is asserted here
// because the host is the fork's only dial: internal/provider/openai appends
// "/v1/chat/completions" itself, and normalises a trailing /v1 on the base while
// doing it (see TestATrailingV1OnTheHostIsToleratedByTheProvider).
func TestTheGatewayDialstheStandardCompletionsPath(t *testing.T) {
	stub := &gatewayStub{}
	host := stub.start(t)

	if err := run(t, GatewayEndpoint{Host: host, Tokens: signedIn("tok-1")}); err != nil {
		t.Fatalf("SendMessages: %v", err)
	}

	path, method, _, _, _ := stub.seen()
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if path != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions — the provider appends it to the host", path)
	}
}

// C2 规则 1: the access token lives in memory only, and C2 规则 2 builds the
// sender from it. The wire half of that promise is this header.
func TestTheGatewayPresentsTheInMemoryToken(t *testing.T) {
	stub := &gatewayStub{}
	host := stub.start(t)

	if err := run(t, GatewayEndpoint{Host: host, Tokens: signedIn("tok-abc")}); err != nil {
		t.Fatalf("SendMessages: %v", err)
	}

	if _, _, auth, _, _ := stub.seen(); auth != "Bearer tok-abc" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer tok-abc")
	}
}

// runStream drives one turn through the STREAMING path, which is the one the
// product actually uses: the composer renders deltas, and the agent loop reaches
// for StreamMessages whenever the sender offers it.
func runStream(t *testing.T, g GatewayEndpoint) error {
	t.Helper()
	sender, err := g.Sender(reasoningOff)
	if err != nil {
		return err
	}
	streaming, ok := sender.(agent.StreamingSender)
	if !ok {
		t.Fatalf("the gateway sender does not implement agent.StreamingSender; the product's turns are streamed")
	}
	_, err = streaming.StreamMessages(context.Background(), "buding-privacy-1", "sys",
		[]agent.Message{{Role: "user", Content: "hi"}}, 16, func(string) {}, func(string) {})
	return err
}

// C1 规则 5 wants Accept: text/event-stream on the built-in endpoint. The
// provider sets it on the streaming path, so this nail pins that the requirement
// is met by REUSE and that the factory carries no headers of its own. It asserts
// on the streaming path deliberately: SendMessages does not set Accept, and the
// product never calls it for a turn.
func TestTheAcceptHeaderIsTheProvidersNotTheFactories(t *testing.T) {
	stub := &gatewayStub{}
	host := stub.start(t)

	if err := runStream(t, GatewayEndpoint{Host: host, Tokens: signedIn("tok-1")}); err != nil {
		t.Fatalf("StreamMessages: %v", err)
	}

	if _, _, _, accept, _ := stub.seen(); accept != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", accept)
	}
}

// The counter-nail to the one above: base64-of-nothing would be wrong here — the
// point is that the streaming path dials the same URL with the same credential,
// so "the streaming path works" is not a different endpoint in disguise.
func TestTheStreamingPathDialsTheSameUrlAndToken(t *testing.T) {
	stub := &gatewayStub{}
	host := stub.start(t)

	if err := runStream(t, GatewayEndpoint{Host: host, Tokens: signedIn("tok-stream")}); err != nil {
		t.Fatalf("StreamMessages: %v", err)
	}

	path, method, auth, _, _ := stub.seen()
	if method != http.MethodPost || path != "/v1/chat/completions" {
		t.Errorf("%s %s, want POST /v1/chat/completions", method, path)
	}
	if auth != "Bearer tok-stream" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer tok-stream")
	}
}

// C2 规则 2 says the sender is assembled from the CURRENT in-memory token, which
// is what makes a rotating token work without touching internal/app. The
// consequence is that a sender must not be held across turns: this nail rotates
// the token and asserts the next sender presents the new one.
func TestARotatedTokenIsPresentedByTheNextSender(t *testing.T) {
	first := &gatewayStub{}
	second := &gatewayStub{}
	tokens := signedIn("tok-first")

	if err := run(t, GatewayEndpoint{Host: first.start(t), Tokens: tokens}); err != nil {
		t.Fatalf("first turn: %v", err)
	}

	// The platform rotated the access token (a refresh, or a re-login).
	tokens.Set(productclient.Credentials{AccessToken: "tok-second", RefreshToken: "r"})

	// A fresh sender per turn is the design, not an optimisation: the factory is
	// called per turn precisely so it can see the new token.
	if err := run(t, GatewayEndpoint{Host: second.start(t), Tokens: tokens}); err != nil {
		t.Fatalf("second turn: %v", err)
	}

	if _, _, auth, _, _ := first.seen(); auth != "Bearer tok-first" {
		t.Errorf("first turn Authorization = %q, want the token that was current then", auth)
	}
	if _, _, auth, _, _ := second.seen(); auth != "Bearer tok-second" {
		t.Errorf("second turn Authorization = %q, want the rotated token", auth)
	}
}

// Fail-closed (C9, and the same discipline C2 规则 5 applies to a failed
// refresh): with no token there is nothing to present, so the turn must be
// refused locally rather than dialed unauthenticated. Asserting the stub's hit
// count is the difference between "we returned a nicer error" and "nothing left
// the device".
func TestNoTokenRefusesWithoutDialingTheGateway(t *testing.T) {
	stub := &gatewayStub{}
	host := stub.start(t)

	held := &productclient.CredentialHolder{} // signed out
	err := run(t, GatewayEndpoint{Host: host, Tokens: held})
	if err == nil {
		t.Fatal("want an error when no access token is held")
	}
	if _, _, _, _, hits := stub.seen(); hits != 0 {
		t.Errorf("the gateway was dialed %d time(s) without a token", hits)
	}
}

// A nil holder is a wiring mistake, not a signed-out user — and it must fail the
// same way rather than panicking on a nil dereference.
func TestANilHolderIsRefusedNotPanicked(t *testing.T) {
	stub := &gatewayStub{}
	host := stub.start(t)

	if err := run(t, GatewayEndpoint{Host: host}); err == nil {
		t.Fatal("want an error when the holder was never wired")
	}
	if _, _, _, _, hits := stub.seen(); hits != 0 {
		t.Errorf("the gateway was dialed %d time(s) with no holder", hits)
	}
}

// An unconfigured host must be refused before a request is built, so a build
// with a placeholder profile cannot quietly dial a relative URL.
func TestAnEmptyHostIsRefused(t *testing.T) {
	if err := run(t, GatewayEndpoint{Host: "", Tokens: signedIn("tok-1")}); err == nil {
		t.Fatal("want an error when the gateway host is empty")
	}
}

// streamText drives one turn through the streaming path and returns what the
// gateway streamed back, so a nail can assert on the reply rather than only on
// the request. The two callbacks are (chunk, thinking); only the first carries
// assistant text.
func streamText(t *testing.T, g GatewayEndpoint) (string, error) {
	t.Helper()
	sender, err := g.Sender(reasoningOff)
	if err != nil {
		return "", err
	}
	streaming, ok := sender.(agent.StreamingSender)
	if !ok {
		t.Fatalf("the gateway sender does not implement agent.StreamingSender")
	}
	var out strings.Builder
	_, err = streaming.StreamMessages(context.Background(), "buding-privacy-1", "sys",
		[]agent.Message{{Role: "user", Content: "hi"}}, 16,
		func(chunk string) { out.WriteString(chunk) }, func(string) {})
	return out.String(), err
}

// signInAgainstStandIn performs the fixture's login so the token the gateway
// presents is one the stand-in really issued. It talks to the fixture over HTTP
// rather than through productclient on purpose: this nail is about the wire the
// developer's own two-process setup produces, and routing it through the client
// would hide whether the fixture itself answers.
func signInAgainstStandIn(t *testing.T, base string) string {
	t.Helper()
	post := func(path string, payload any) map[string]any {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", path, err)
		}
		resp, err := http.Post(base+path, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("post %s: %v", path, err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("post %s: status %d, body %v", path, resp.StatusCode, out)
		}
		return out
	}

	const phone = "13700000000"
	post("/v1/auth/sms/send", productclient.SendSMSRequest{
		Phone: phone, Purpose: productclient.PurposeLogin, ClientRequestID: "req-gw",
	})
	login := post("/v1/auth/login", productclient.LoginRequest{
		Phone:          phone,
		Code:           clienttest.FixtureSMSCode,
		ActivationCode: clienttest.FixtureActivationCode,
		BoxCode:        clienttest.FixtureBoxCode,
		InstallID:      "22222222-2222-4222-8222-222222222222",
	})

	data, _ := login["data"].(map[string]any)
	token, _ := data["accessToken"].(string)
	if token == "" {
		t.Fatalf("the stand-in issued no access token: %v", login)
	}
	return token
}

// The nail that closes the gap between "the tests pass" and "a person can walk
// it": the sender factory dials the SAME stand-in that `go run ./cmd/productstub`
// serves, and completes a turn.
//
// Why it is separate from the stubs above. Those assert a request shape against
// a handler written for the test, so they prove the client is right about itself.
// This one proves the two processes the developer runs by hand agree with each
// other — the fixture answers the URL the provider builds, issues the token the
// gateway then presents (C2), and streams something the aggregator can
// reassemble. Before PR-5a the fixture served no completions route at all, so a
// hand walkthrough could sign in and then have nothing to talk to.
func TestTheStandInTheDeveloperRunsCanAnswerATurn(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()

	token := signInAgainstStandIn(t, ts.URL)

	// The host is the stand-in's own base URL, written bare exactly as
	// developer.json records it.
	reply, err := streamText(t, GatewayEndpoint{Host: ts.URL, Tokens: signedIn(token)})
	if err != nil {
		t.Fatalf("StreamMessages against the stand-in: %v", err)
	}
	if reply == "" {
		t.Fatal("the stand-in streamed an empty reply")
	}
	if !strings.Contains(reply, "buding-privacy-1") {
		t.Errorf("reply = %q; it should name the bare catalog id the gateway was asked for", reply)
	}
	if got := standin.CompletionCount(); got != 1 {
		t.Errorf("CompletionCount = %d, want 1", got)
	}
}

// A trailing /v1 on the host is TOLERATED, and this nail exists because the
// fork's plan asserted the opposite for a while (V-37, since retracted).
//
// internal/provider/openai.endpointURL normalises it: it strips a trailing "/v1"
// before appending, precisely because OpenAI-compatible gateways disagree about
// which side owns the prefix — Bailian bakes /v1 into the documented base,
// DeepSeek ships a bare host. So `host` and `host/v1` both dial
// /v1/chat/completions, and neither produces the /v1/v1/… that V-37 claimed
// would break the first turn.
//
// The reason this is pinned rather than left to the provider's own tests: the
// fork's profile, its release guard and its comments all stated the stricter
// rule, and a rule that is merely wrong is more expensive than a rule that is
// missing — it gets followed. If upstream ever drops the normalisation, this
// fails and says which claim has to be re-derived instead of leaving a silent
// 404 at the first turn.
func TestATrailingV1OnTheHostIsToleratedByTheProvider(t *testing.T) {
	for _, shape := range []struct {
		name string
		host func(string) string
	}{
		{"bare host", func(base string) string { return base }},
		{"host with a trailing /v1", func(base string) string { return base + "/v1" }},
	} {
		t.Run(shape.name, func(t *testing.T) {
			stub := &gatewayStub{}
			host := shape.host(stub.start(t))

			if err := run(t, GatewayEndpoint{Host: host, Tokens: signedIn("tok-1")}); err != nil {
				t.Fatalf("SendMessages with a %s host: %v", shape.name, err)
			}
			path, _, _, _, _ := stub.seen()
			if path != "/v1/chat/completions" {
				t.Errorf("path = %q, want /v1/chat/completions for a %s host", path, shape.name)
			}
		})
	}
}
