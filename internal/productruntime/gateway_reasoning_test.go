package productruntime

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// PR-5b1's wire half: the two reasoning preferences have to reach the request
// the gateway sends, and a reasoning trace the gateway streams back has to reach
// the caller's onThinking.
//
// WHY BOTH HALVES ARE HERE AND NOT IN internal/server. The seam (who hands the
// preferences in) is internal/server's; what the sender then DOES with them is
// this package's and the provider's. Splitting them this way keeps the nails
// attached to the code that has to be right: a test in internal/server would
// have to stub the sender and could not see the request body at all.
//
// The two failures this prevents are independent, which is why the nails below
// do not collapse into one:
//
//   - "要它产生" — nothing is sent at all. internal/provider/openai's
//     applyReasoning does not emit reasoning_effort when the effort is empty
//     (omitempty), and the UI's default level IS empty ("off"), so a build that
//     wired only ShowReasoning would look correct and never ask a model to
//     reason.
//   - "要它显示" — the trace arrives and is dropped. app.sender.reasoningSink
//     returns nil unless ShowReasoning is set, and the gateway factory set
//     neither field. So a user who picked a level and had a model that reasons
//     saw nothing.

// reasoningStubStream is what a gateway that reasons streams back: the trace
// first, then the answer. The trace text deliberately does NOT contain the model
// id, so a nail that asserts "the answer names the model" cannot pass by
// accident if the trace ever leaked into the content channel.
const reasoningStubStream = `data: {"choices":[{"delta":{"role":"assistant"}}]}` + "\n\n" +
	`data: {"choices":[{"delta":{"reasoning_content":"weighing the request"}}]}` + "\n\n" +
	`data: {"choices":[{"delta":{"content":"ok"}}]}` + "\n\n" +
	"data: [DONE]\n\n"

// streamWithThinking dials the gateway once and returns both channels.
func streamWithThinking(t *testing.T, g GatewayEndpoint, tuning app.ReasoningTuning) (answer, thinking string, err error) {
	t.Helper()
	sender, err := g.Sender(tuning)
	if err != nil {
		return "", "", err
	}
	streaming, ok := sender.(agent.StreamingSender)
	if !ok {
		t.Fatalf("the gateway sender does not implement agent.StreamingSender")
	}
	var out, thought strings.Builder
	_, err = streaming.StreamMessages(context.Background(), "buding-privacy-1", "sys",
		[]agent.Message{{Role: "user", Content: "hi"}}, 64,
		func(chunk string) { out.WriteString(chunk) },
		func(chunk string) { thought.WriteString(chunk) })
	return out.String(), thought.String(), err
}

// TestTheStandInEmitsReasoningAndTheGatewaySurfacesIt is the premise nail as
// well as the main one: the fixture the developer runs by hand must actually
// reason, or the hand walkthrough in §6.4 step 5 has nothing to show and "the
// gateway does not display thinking" would be indistinguishable from "the
// stand-in never sent any".
//
// It goes through clienttest rather than a local stub on purpose. The other
// nails in this file assert the request; this one proves the two processes a
// developer actually starts agree with each other.
func TestTheStandInEmitsReasoningAndTheGatewaySurfacesIt(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()

	token := signInAgainstStandIn(t, ts.URL)

	answer, thinking, err := streamWithThinking(t,
		GatewayEndpoint{Host: ts.URL, Tokens: signedIn(token)},
		app.ReasoningTuning{ShowReasoning: true, ReasoningEffort: "medium"})
	if err != nil {
		t.Fatalf("StreamMessages against the stand-in: %v", err)
	}
	if thinking == "" {
		t.Fatal("the stand-in streamed nothing into onThinking; the trace reached the client and was dropped")
	}
	if !strings.Contains(thinking, "reasoning") && !strings.Contains(thinking, "weighing") {
		t.Errorf("thinking = %q, want the trace the stand-in sent", thinking)
	}
	// The trace must stay out of the answer: they are separate blocks (C10).
	if strings.Contains(answer, thinking) && thinking != "" {
		t.Errorf("answer = %q contains the trace %q; they must be separate blocks", answer, thinking)
	}
	if answer == "" {
		t.Error("the stand-in streamed no answer text")
	}
}

// The producing half: the user's level has to reach the wire, and it has to
// arrive as the flat reasoning_effort the contract now requires (中台交付包 §5.2,
// PQ27). Asserting the parsed body would accept any of the four other shapes
// internal/provider/openai knows how to emit.
func TestTheChosenLevelReachesTheWire(t *testing.T) {
	stub := &gatewayStub{stream: reasoningStubStream}
	host := stub.start(t)

	_, _, err := streamWithThinking(t,
		GatewayEndpoint{Host: host, Tokens: signedIn("tok-1")},
		app.ReasoningTuning{ShowReasoning: true, ReasoningEffort: "medium"})
	if err != nil {
		t.Fatalf("StreamMessages: %v", err)
	}

	if got := stub.requestBody(); !strings.Contains(got, `"reasoning_effort":"medium"`) {
		t.Errorf("request body = %s, want a flat reasoning_effort of \"medium\"", got)
	}
}

// The regression nail the plan calls out: wiring the preferences must not turn
// into "always send something". The UI's default level is off, and an unset
// level has to stay UNSET — a build that sent a default instead would silently
// buy reasoning the user did not ask for, and would 400 against any gateway that
// validates the field's values.
func TestAnOffLevelSendsNoReasoningField(t *testing.T) {
	stub := &gatewayStub{stream: reasoningStubStream}
	host := stub.start(t)

	_, _, err := streamWithThinking(t,
		GatewayEndpoint{Host: host, Tokens: signedIn("tok-1")},
		app.ReasoningTuning{ShowReasoning: true, ReasoningEffort: ""})
	if err != nil {
		t.Fatalf("StreamMessages: %v", err)
	}

	if got := stub.requestBody(); strings.Contains(got, "reasoning_effort") {
		t.Errorf("request body = %s, want NO reasoning_effort when the level is off", got)
	}
}

// The displaying half, as a counter-nail: the trace arrives (the fixture is
// streaming it — the nail above proved that) and must be dropped when the user
// turned the switch off. Without this, a change that always surfaced reasoning
// would pass every other nail here.
func TestReasoningIsDroppedWhenTheSwitchIsOff(t *testing.T) {
	stub := &gatewayStub{stream: reasoningStubStream}
	host := stub.start(t)

	_, thinking, err := streamWithThinking(t,
		GatewayEndpoint{Host: host, Tokens: signedIn("tok-1")},
		app.ReasoningTuning{ShowReasoning: false, ReasoningEffort: "medium"})
	if err != nil {
		t.Fatalf("StreamMessages: %v", err)
	}

	if thinking != "" {
		t.Errorf("onThinking got %q with the switch off; the trace must not be surfaced", thinking)
	}
}
