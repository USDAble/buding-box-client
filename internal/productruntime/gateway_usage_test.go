package productruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// PR-5d2's wire half: the `usage` step of L-C3a's pipeline (content delta →
// usage → 终态).
//
// WHAT WAS ACTUALLY MISSING. Not a parser and not a field: `complete.tokens`
// already carried the provider-reported total (ws_handlers builds it from
// a.SessionTokens(), which accrueUsage fills from Reply.InputTokens/
// OutputTokens), and ChatView already rendered it. What was missing is that the
// FIXTURE never sent a usage frame, so the number was always 0 — a fabricated
// zero presented as a fact (§3.9) — and nailing the pipeline was impossible,
// which is why "usage is not covered yet" survived so long.
//
// WHY THE NUMBERS ARE ODD ON PURPOSE. Same lesson as V-57, where the stand-in
// serialised bootstrap with the client's own DTO: when the fixture is
// self-consistent with the code under test, every nail passes even when the
// code is wrong. If the stand-in reported roughly len(reply)/4, a build that
// replaced the provider's number with the transcript estimate would still be
// green. So the fixture reports a prompt count two orders of magnitude above the
// estimate and a completion count that no chars/4 division can produce, and the
// nail below asserts BOTH the equality and the inequality.

// usageTurn drives one whole turn through the gateway sender and the agent loop
// — the same shape as toolTurn, with no tools advertised, because this nail is
// about the usage accounting rather than the tool loop.
func usageTurn(t *testing.T, host, token, prompt string) (agent.Reply, *agent.Agent, error) {
	t.Helper()
	sender, err := GatewayEndpoint{Host: host, Tokens: signedIn(token)}.Sender(reasoningOff)
	if err != nil {
		return agent.Reply{}, nil, err
	}
	a := agent.New(sender, nailsModel)
	a.System = "sys"
	a.MaxTokens = 256
	reply, err := a.RunStream(context.Background(), prompt, nil, nil, nil)
	return reply, a, err
}

// The premise nail: the fixture the developer starts by hand must actually send
// a usage frame, or the hand walkthrough shows `0 tokens` and "the gateway
// under-reports" is indistinguishable from "the stand-in never told it".
//
// It reads the raw SSE rather than the agent's counters on purpose: a nail that
// only looked at the counters would pass on a build that invented a number.
func TestTheStandInStreamsAUsageFrame(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)

	body := rawStream(t, ts.URL, token)

	var sawUsage bool
	for _, frame := range sseFrames(t, body) {
		var chunk struct {
			Choices []json.RawMessage `json:"choices"`
			Usage   *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(frame), &chunk); err != nil {
			t.Fatalf("frame is not OpenAI-shaped JSON: %v (%s)", err, frame)
		}
		if chunk.Usage == nil {
			continue
		}
		// The shape matters as much as the numbers: OpenAI sends the usage frame
		// with an EMPTY choices array, and internal/provider/openai only reads
		// chunk-level usage in that branch. A fixture that attached usage to a
		// content chunk would be read by the fallback path instead, leaving the
		// real branch unexercised at the one layer a person can watch.
		if len(chunk.Choices) != 0 {
			t.Errorf("the usage frame carried %d choice(s); OpenAI sends it with none", len(chunk.Choices))
		}
		if chunk.Usage.PromptTokens != clienttest.StandinPromptTokens {
			t.Errorf("prompt_tokens = %d, want %d", chunk.Usage.PromptTokens, clienttest.StandinPromptTokens)
		}
		if chunk.Usage.CompletionTokens != clienttest.StandinCompletionTokens {
			t.Errorf("completion_tokens = %d, want %d", chunk.Usage.CompletionTokens, clienttest.StandinCompletionTokens)
		}
		sawUsage = true
	}
	if !sawUsage {
		t.Fatal("the stand-in streamed no usage frame; the turn summary would read 0 tokens")
	}
}

// The pipeline nail: what the gateway reported is what the turn is accounted
// for. This is the half that makes `usage` a covered step rather than a
// supported-but-unwitnessed one.
func TestTheReportedUsageBecomesTheTurnsAccounting(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)

	reply, a, err := usageTurn(t, ts.URL, token, "hi")
	if err != nil {
		t.Fatalf("turn against the stand-in: %v", err)
	}

	inTok, outTok := a.SessionTokens()
	if inTok <= 0 || outTok <= 0 {
		t.Fatalf("SessionTokens() = (%d, %d); a real turn reports both counts", inTok, outTok)
	}
	if inTok != clienttest.StandinPromptTokens {
		t.Errorf("input tokens = %d, want the gateway's %d", inTok, clienttest.StandinPromptTokens)
	}
	if outTok != clienttest.StandinCompletionTokens {
		t.Errorf("output tokens = %d, want the gateway's %d", outTok, clienttest.StandinCompletionTokens)
	}

	// The counter-nail. A build that fell back to the transcript estimate —
	// len(text)/4, which is what the UI used to draw its own "↓ ~N tokens" — would
	// satisfy "greater than zero" and "roughly plausible" while never having read
	// the usage frame at all.
	if est := estimateFor(reply); est == outTok {
		t.Fatalf("the reported output count equals the chars/4 estimate (%d) — the fixture cannot tell them apart", est)
	}
	if outTok < estimateFor(reply)*4 {
		t.Errorf("output tokens = %d, but the fixture's reply is only ~%d chars/4; the reported count should be unmistakably not an estimate",
			outTok, estimateFor(reply))
	}
}

// And the input side of the same fact: the number the UI shows as context fill
// must be the reported prompt count, not the transcript estimate that
// ContextUsage falls back to. RealContextTokens is the accessor that refuses the
// fallback, so it is the one that can distinguish them.
func TestTheReportedPromptCountIsTheRealContextSize(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)

	_, a, err := usageTurn(t, ts.URL, token, "hi")
	if err != nil {
		t.Fatalf("turn against the stand-in: %v", err)
	}

	if got := a.RealContextTokens(); got != clienttest.StandinPromptTokens {
		t.Errorf("RealContextTokens() = %d, want the gateway's prompt_tokens %d", got, clienttest.StandinPromptTokens)
	}
}

// rawStream dials the stand-in's completions route directly and returns the
// event-stream body, so the fixture can be inspected without the provider's
// aggregator standing between it and the assertion.
func rawStream(t *testing.T, host, token string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"model":  nailsModel,
		"stream": true,
		// The client always sends this, and a fixture that only emits usage when
		// asked would otherwise differ from the wire the real client produces.
		"stream_options": map[string]any{"include_usage": true},
		"messages":       []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, host+"/v1/chat/completions", bytes.NewReader(body))
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
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read turn body: %v", err)
	}
	return string(raw)
}

// sseFrames returns the payload of every `data:` line, minus the terminator.
func sseFrames(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(body, "\n") {
		payload, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "data: ")
		if !ok || payload == "[DONE]" {
			continue
		}
		out = append(out, payload)
	}
	return out
}

// estimateFor is the chars/4 estimate the UI used to draw its own token readout
// — the number a build that skipped the usage frame would end up showing. Kept
// local to the counter-nails so it cannot drift into the production path.
func estimateFor(reply agent.Reply) int {
	chars := len(strings.TrimSpace(reply.Content))
	return chars / 4
}
