package productruntime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// PR-5d3's end-to-end half for L-C4c: the platform's refusal reaches the agent as a
// CODE, having crossed the real stand-in, the real HTTP stack and the real provider
// adapter.
//
// WHY THE ADAPTER'S OWN NAIL IS NOT ENOUGH. That one feeds the adapter a literal body.
// Here the body is produced by the fixture — i.e. by the platform's contract
// (clienttest mirrors 中台交付包 §3.2) — and travels through GatewayEndpoint.Sender,
// the streaming path, and the agent loop's `%w` wrapping. Three failure modes this
// catches that the adapter nail cannot: the stand-in answering a shape the adapter does
// not know, the gateway sender dropping the code, and the code not surviving the loop's
// wrapping.

// creditTurn drives one whole turn through the gateway sender and the agent loop and
// returns the error the agent surfaced.
func creditTurn(t *testing.T, host, token string) error {
	t.Helper()
	sender, err := GatewayEndpoint{Host: host, Tokens: signedIn(token)}.Sender(reasoningOff)
	if err != nil {
		t.Fatalf("Sender: %v", err)
	}
	a := agent.New(sender, nailsModel)
	a.System = "sys"
	a.MaxTokens = 256
	_, err = a.RunStream(context.Background(), "anything", nil, nil, nil)
	return err
}

func TestAnOutOfCreditTurnCarriesThePlatformsCode(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)
	standin.FailCompletions(http.StatusPaymentRequired, "insufficient_credits")

	err := creditTurn(t, ts.URL, token)
	if err == nil {
		t.Fatal("the turn succeeded against a platform that refused it with 402")
	}

	if got := agent.ErrorCodeOf(err); got != "insufficient_credits" {
		t.Fatalf("code = %q, want %q — the frontend can only choose 余额不足 if the code arrives (err: %v)", got, "insufficient_credits", err)
	}
	// The other half of L-C4c's complaint: whatever sentence travels, it must not be the
	// gateway's JSON body.
	if msg := err.Error(); strings.Contains(msg, "{") || strings.Contains(msg, `"code"`) {
		t.Errorf("error = %q still contains the raw body; the user must never see the envelope", msg)
	}
}

// The fixture must be able to say "no credit" at all, otherwise the branch above is
// unreachable for a hand walkthrough and for every future nail. 402 with the registered
// code, and — because the stand-in speaks the contract — a FLAT envelope rather than
// OpenAI's nested one. That difference is the whole bug: the adapter was written for the
// nested shape, so a fixture that answered nested could not reproduce it.
func TestFailCompletionsAnswersAFlatEnvelope(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)
	standin.FailCompletions(http.StatusPaymentRequired, "insufficient_credits")

	resp := rawCompletions(t, ts.URL, token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402", resp.StatusCode)
	}
	body := rawBody(t, resp.Body)
	if !strings.Contains(body, `"insufficient_credits"`) {
		t.Errorf("body = %s, want the registered code as a top-level field", body)
	}
	if strings.Contains(body, `"error"`) {
		t.Errorf("body = %s is nested; the control plane's envelope is flat (中台交付包 §3.2), and a fixture that answers nested cannot reproduce the bug", body)
	}
}

// And the switch is off by default: an unarmed stand-in serves the turn. Without this
// the default could be inverted and every other turn test would stay green, because they
// all arm it.
func TestTheGatewayRefusalIsOffByDefault(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)

	if err := creditTurn(t, ts.URL, token); err != nil {
		t.Fatalf("a turn against an unarmed stand-in failed: %v", err)
	}
}

// rawCompletions posts a streaming turn straight to the stand-in and hands back the
// response, so the fixture's wire shape can be inspected without the provider's
// aggregator standing between it and the assertion. It is the same request rawStream
// builds; it returns the response instead of the body because the status IS the
// assertion here.
func rawCompletions(t *testing.T, host, token string) *http.Response {
	t.Helper()
	body := `{"model":"` + nailsModel + `","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequest(http.MethodPost, host+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post turn: %v", err)
	}
	return resp
}

func rawBody(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
