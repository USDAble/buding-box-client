package productruntime

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/audit"
	"github.com/open-octo/octo-agent/internal/permission"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// PR-5b2's nails: what happens when the gateway asks for a tool (需求基线 C10,
// 闭环 L-C3c).
//
// WHY THESE ARE NOT A DUPLICATE OF THE AGENT LOOP'S OWN TESTS. The loop's
// handling of a tool_use block - gate consulted, denied calls never executed,
// a denial synthesised as an IsError tool_result - is upstream behaviour with
// upstream nails. What no upstream nail can see is whether a tool call that
// came from the GATEWAY travels that same path: the gateway reaches the loop
// through internal/productruntime's sender factory, and if that sender stopped
// satisfying agent.ToolStreamingSender the loop would fall back to TurnStream
// (agent.go:920) and every tool call would vanish with all of those upstream
// tests still green.
//
// So the subject here is the composition: real stand-in platform -> the fork's
// gateway sender -> the agent loop -> the local permission gate.
//
// THE GATE IS THE REAL ONE. These nails build app.NewPermissionGate around a
// real permission.Engine rather than a stub that answers yes/no, because C10's
// point is precisely that the decision comes from the local policy: the model
// asking for a tool is not the user authorising it (PQ20 - when the gateway
// sends no toolPolicy the local gate still applies).
//
// "Still applies" is not "always asks", and the difference is V-53: the default
// policy silently ALLOWS a set of safe verbs plus every read_file, so only a
// command matching no rule at all reaches the engine's implicit ask. Two nails
// below cover both halves, and the premise nail asserts which command this file
// is using.

const (
	nailsModel = "buding-privacy-1"
	// An ask-class command, which is what makes these the gate nails. It is
	// `hostname` and not something more obviously "dangerous" because the policy
	// auto-allows the common safe verbs (defaults.yml: `echo `, `ls`, `cat `,
	// `pwd` are in the allow block "so the user isn't drowning in prompts"), so
	// an innocuous-looking command can be allowed outright. `hostname` matches no
	// rule at all and falls through to the engine's implicit ask - the premise is
	// asserted by TestTheGateNailsUseAnAskClassTool rather than assumed, because
	// this file's first draft used `echo hello ...` and was silently testing the
	// allow path.
	nailsTool      = "terminal"
	nailsCommand   = "hostname"
	nailsArguments = `{"command":"hostname"}`
)

// toolDefs is the schema set the turn advertises. Hand-built rather than
// tools.DefaultToolsForCtx: the loop only forwards these, and a hermetic list
// keeps the nail about the gateway path rather than about the registry.
func toolDefs() []agent.ToolDefinition {
	schema := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	return []agent.ToolDefinition{
		{
			Name:        nailsTool,
			Description: "run a shell command",
			Parameters:  schema(map[string]any{"command": map[string]any{"type": "string"}}, "command"),
		},
		{
			Name:        "read_file",
			Description: "read a file",
			Parameters:  schema(map[string]any{"path": map[string]any{"type": "string"}}, "path"),
		},
	}
}

// recordingExecutor stands in for tools.DefaultRegistry: it answers with a
// recognisable string so the nail can find the executor's answer travelling
// back to the platform, and it counts calls so "exactly once" is checkable.
type recordingExecutor struct {
	mu    sync.Mutex
	calls []string
	args  []map[string]any
	text  string
}

func (e *recordingExecutor) Execute(_ context.Context, name string, input map[string]any) (agent.ToolResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, name)
	e.args = append(e.args, input)
	return agent.ToolResult{Text: e.text}, nil
}

func (e *recordingExecutor) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

func (e *recordingExecutor) lastArgs() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.args) == 0 {
		return nil
	}
	return e.args[len(e.args)-1]
}

// engineFor builds the real policy engine the gate wraps.
func engineFor(t *testing.T, mode permission.Mode) *permission.Engine {
	t.Helper()
	eng, err := permission.New("", "/work", mode)
	if err != nil {
		t.Fatalf("permission.New: %v", err)
	}
	return eng
}

// The premise the two gate nails rest on: in interactive mode an unlisted
// terminal command is an ASK, not an allow. Asserted here so a later change to
// defaults.yml makes this file fail loudly instead of quietly turning
// "denied" into "allowed" and passing for the wrong reason.
func TestTheGateNailsUseAnAskClassTool(t *testing.T) {
	eng := engineFor(t, permission.ModeInteractive)
	if got := eng.Check(nailsTool, map[string]any{"command": nailsCommand}); got != permission.Ask {
		t.Fatalf("permission decision for %q = %v, want Ask - these nails would prove nothing about the gate", nailsCommand, got)
	}
}

// nail 2: the interface that keeps tool calls from silently disappearing.
//
// RunStream takes the tool branch only while the sender satisfies
// agent.ToolStreamingSender; otherwise it falls back to TurnStream with no
// error and no warning. Asserting the interface here is what makes that fallback
// a test failure rather than a capability nobody notices is gone.
func TestTheGatewaySenderCanCarryToolCalls(t *testing.T) {
	g := GatewayEndpoint{Host: "http://127.0.0.1:1", Tokens: signedIn("tok")}
	s, err := g.Sender(reasoningOff)
	if err != nil {
		t.Fatalf("Sender: %v", err)
	}
	if _, ok := s.(agent.ToolStreamingSender); !ok {
		t.Fatalf("the gateway sender does not implement agent.ToolStreamingSender (type %T); "+
			"RunStream would fall back to TurnStream and drop every tool call without an error", s)
	}
}

// nail 1: the stand-in emits a tool call only when asked to.
//
// Both halves matter. Without the first, L-C3c cannot be judged at all (there
// would be no tool call anywhere on the path); without the second, every other
// hand walkthrough - and every nail that asserts a reply's text - would trip
// over an approval prompt for a tool nobody asked for.
func TestTheStandInRequestsAToolOnlyWhenSwitchedOn(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)
	host := ts.URL

	// Off (the default): a whole turn, no tool call, no executor involvement.
	exec := &recordingExecutor{text: "ran it"}
	offReply, err := toolTurn(t, host, token, nil, exec)
	if err != nil {
		t.Fatalf("turn with the switch off: %v", err)
	}
	if standin.ToolCallCount() != 0 {
		t.Errorf("the stand-in emitted %d tool call(s) with the switch off", standin.ToolCallCount())
	}
	if exec.count() != 0 {
		t.Errorf("the executor ran %d time(s) with no tool call on the wire", exec.count())
	}
	if !strings.Contains(offReply.Content, "stand-in gateway") {
		t.Errorf("reply = %q, want the stand-in's plain answer", offReply.Content)
	}
}

// nails 3-7: one whole loop, from the platform's request to the model's final
// word, with the real gate allowing the call.
func TestAToolCallFromTheGatewayRunsTheLocalLoop(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)

	standin.RequestToolCall(nailsTool, nailsArguments)

	exec := &recordingExecutor{text: "[executor] hello from the stand-in"}

	asks := 0
	gate := app.NewPermissionGate(engineFor(t, permission.ModeInteractive),
		func(_ context.Context, _ string, _ map[string]any) (bool, bool, error) {
			asks++
			return true, false, nil
		},
		audit.NewAt(""))

	reply, err := toolTurn(t, ts.URL, token, gate, exec)
	if err != nil {
		t.Fatalf("tool turn: %v", err)
	}

	// nail 3: the tools were advertised on the wire. Without this the model
	// could never have called one, and "no tool call happened" would be
	// indistinguishable from "the client never offered any".
	if got := standin.LastToolNames(); !contains(got, nailsTool) {
		t.Errorf("the gateway received tools %v, want one named %q", got, nailsTool)
	}

	// nail 5: the gate allowed, so the tool ran - exactly once. Once, not
	// once per streamed argument fragment: the arguments arrive across several
	// chunks and are concatenated by the provider, and an executor invoked per
	// fragment would pass any nail that only counted content.
	if exec.count() != 1 {
		t.Fatalf("the executor ran %d time(s), want exactly 1", exec.count())
	}
	if asks != 1 {
		t.Errorf("the gate was consulted %d time(s), want exactly 1", asks)
	}
	if got := exec.lastArgs(); got["command"] != nailsCommand {
		t.Errorf("the executor got %#v, want the command the stand-in sent", got)
	}

	// nail 6: the result travelled back to the platform, linked to the call.
	results := standin.LastToolResults()
	if len(results) != 1 {
		t.Fatalf("the gateway received %d tool result(s), want exactly 1: %v", len(results), results)
	}
	for id, content := range results {
		if id != standin.LastToolCallID() {
			t.Errorf("tool result id = %q, want the id the gateway issued (%q)", id, standin.LastToolCallID())
		}
		if !strings.Contains(content, "[executor] hello from the stand-in") {
			t.Errorf("the gateway received tool result %q, want the executor's text", content)
		}
	}

	// nail 7: the turn continued and ended on the model's answer - two calls,
	// not one (the loop stopped before feeding the result back) and not three
	// (the stand-in kept asking for a tool, so the loop never terminated).
	if got := standin.CompletionCount(); got != 2 {
		t.Errorf("the stand-in answered %d completions, want 2 (call, then result-fed-back)", got)
	}
	if !strings.Contains(reply.Content, "stand-in gateway") {
		t.Errorf("reply = %q, want the stand-in's final answer", reply.Content)
	}
}

// nail 4: a call the gate refuses must not reach the executor, and the model
// must be told - C10's "the gateway asking is not the user authorising".
//
// The posture is the non-interactive one (ask == nil), which is what a server
// or IM turn gets and what PQ20 requires when the gateway sends no toolPolicy:
// ask resolves to deny, never to allow.
func TestARefusedToolCallNeverReachesTheExecutor(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)

	standin.RequestToolCall(nailsTool, nailsArguments)

	exec := &recordingExecutor{text: "[executor] should never be seen"}
	gate := app.NewPermissionGate(engineFor(t, permission.ModeInteractive), nil, audit.NewAt(""))

	reply, err := toolTurn(t, ts.URL, token, gate, exec)
	if err != nil {
		t.Fatalf("tool turn: %v", err)
	}

	if exec.count() != 0 {
		t.Fatalf("the executor ran %d time(s) after the gate refused; a refused call must not run", exec.count())
	}
	results := standin.LastToolResults()
	if len(results) != 1 {
		t.Fatalf("the gateway received %d tool result(s), want exactly 1 (the refusal): %v", len(results), results)
	}
	for _, content := range results {
		if !strings.Contains(strings.ToLower(content), "denied") && !strings.Contains(content, "permission") {
			t.Errorf("the refusal sent back was %q; the model must be told why so it can adapt", content)
		}
		if strings.Contains(content, "[executor]") {
			t.Errorf("the refusal sent back was %q, which leaks the executor's output", content)
		}
	}
	// The turn still finishes: a refusal is information for the model, not a
	// reason to abort the run.
	if !strings.Contains(reply.Content, "stand-in gateway") {
		t.Errorf("reply = %q, want the turn to finish with the model's answer", reply.Content)
	}
}

// nail 5b: a call the local policy ALLOWS runs without consulting the ask at
// all. This is the other side of C10 - the gate is a policy engine, not a
// prompt - and it pins the distinction between "the user said yes" and "our own
// rules already said yes" (defaults.yml allows any read_file outside the
// credential set).
func TestALocallyAllowedToolNeedsNoPrompt(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)

	standin.RequestToolCall("read_file", `{"path":"notes.txt"}`)

	exec := &recordingExecutor{text: "[executor] file contents"}
	asks := 0
	gate := app.NewPermissionGate(engineFor(t, permission.ModeInteractive),
		func(_ context.Context, _ string, _ map[string]any) (bool, bool, error) {
			asks++
			return true, false, nil
		},
		audit.NewAt(""))

	if _, err := toolTurn(t, ts.URL, token, gate, exec); err != nil {
		t.Fatalf("tool turn: %v", err)
	}

	if exec.count() != 1 {
		t.Fatalf("the executor ran %d time(s), want 1", exec.count())
	}
	if asks != 0 {
		t.Errorf("the gate prompted %d time(s) for a tool the policy already allows", asks)
	}
}

// nail 8: with no tool call on the wire, a turn that carries tools, an executor
// and a gate behaves exactly as it did before this PR - one completion, plain
// text, nothing executed.
//
// This is the nail that protects every other hand walkthrough: the stand-in
// ships the tool-call capability switched off, and switching it off has to mean
// a turn with no approval prompt and no tool activity.
func TestATurnWithNoToolCallIsUnchanged(t *testing.T) {
	standin := clienttest.New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()
	token := signInAgainstStandIn(t, ts.URL)

	exec := &recordingExecutor{text: "unused"}
	gate := app.NewPermissionGate(engineFor(t, permission.ModeInteractive), nil, audit.NewAt(""))

	reply, err := toolTurn(t, ts.URL, token, gate, exec)
	if err != nil {
		t.Fatalf("plain turn: %v", err)
	}

	if got := standin.CompletionCount(); got != 1 {
		t.Errorf("completions = %d, want 1 for a turn with no tool call", got)
	}
	if exec.count() != 0 {
		t.Errorf("the executor ran %d time(s) with no tool call on the wire", exec.count())
	}
	if strings.TrimSpace(reply.Content) == "" {
		t.Error("the turn produced no answer")
	}
}

// toolTurn drives one whole turn through the gateway sender and the agent loop,
// with tools advertised and the run's gate installed.
func toolTurn(t *testing.T, host, token string, gate agent.PermissionGate, exec agent.ToolExecutor) (agent.Reply, error) {
	t.Helper()
	sender, err := GatewayEndpoint{Host: host, Tokens: signedIn(token)}.Sender(reasoningOff)
	if err != nil {
		return agent.Reply{}, err
	}
	a := agent.New(sender, nailsModel)
	a.System = "sys"
	a.MaxTokens = 256
	a.Gate = gate
	return a.RunStream(context.Background(), "please run it", toolDefs(), exec, nil)
}
