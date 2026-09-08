package app

import (
	"context"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

// fakeFullSender implements every agent sender interface so the decorator's
// capability-preservation can be exercised against the full stack.
type fakeFullSender struct {
	reply          agent.Reply
	deltas         []string
	thinkingDeltas []string
	toolDeltas     []string
	err            error
}

func (f *fakeFullSender) SendMessages(context.Context, string, string, []agent.Message, int) (agent.Reply, error) {
	return f.reply, f.err
}

func (f *fakeFullSender) StreamMessages(_ context.Context, _, _ string, _ []agent.Message, _ int, onChunk func(string), onThinking func(string)) (agent.Reply, error) {
	for _, d := range f.deltas {
		if onChunk != nil {
			onChunk(d)
		}
	}
	for _, d := range f.thinkingDeltas {
		if onThinking != nil {
			onThinking(d)
		}
	}
	return f.reply, f.err
}

func (f *fakeFullSender) SendMessagesWithTools(context.Context, string, string, []agent.Message, int, []agent.ToolDefinition) (agent.Reply, error) {
	return f.reply, f.err
}

func (f *fakeFullSender) StreamMessagesWithTools(_ context.Context, _, _ string, _ []agent.Message, _ int, _ []agent.ToolDefinition, onChunk func(string), onToolDelta agent.ToolInputDeltaFunc, onThinking agent.ThinkingDeltaFunc) (agent.Reply, error) {
	for _, d := range f.deltas {
		if onChunk != nil {
			onChunk(d)
		}
	}
	for _, d := range f.toolDeltas {
		if onToolDelta != nil {
			onToolDelta("t1", "tool", d)
		}
	}
	for _, d := range f.thinkingDeltas {
		if onThinking != nil {
			onThinking(d)
		}
	}
	return f.reply, f.err
}

func (f *fakeFullSender) LowEffort() agent.Sender   { return f }
func (f *fakeFullSender) NoReasoning() agent.Sender { return f }

func testEngine(t *testing.T) *sensitive.Engine {
	t.Helper()
	return sensitive.New("")
}

// TestWrapSensitiveKeepsFullCapability is the guard P8 §7 calls out: wrapping
// a full sender must still satisfy every interface the agent can type-assert,
// or streaming silently degrades to buffered.
func TestWrapSensitiveKeepsFullCapability(t *testing.T) {
	w := WrapSensitive(&fakeFullSender{}, testEngine(t))
	if _, ok := w.(agent.StreamingSender); !ok {
		t.Fatal("wrap dropped StreamingSender")
	}
	if _, ok := w.(agent.ToolSender); !ok {
		t.Fatal("wrap dropped ToolSender")
	}
	if _, ok := w.(agent.ToolStreamingSender); !ok {
		t.Fatal("wrap dropped ToolStreamingSender")
	}
	if _, ok := w.(agent.LowEffortSender); !ok {
		t.Fatal("wrap dropped LowEffortSender")
	}
	if _, ok := w.(agent.NoReasoningSender); !ok {
		t.Fatal("wrap dropped NoReasoningSender")
	}
}

// TestWrapSensitiveNilPassthrough: a nil sender or engine returns unchanged.
func TestWrapSensitiveNilPassthrough(t *testing.T) {
	if WrapSensitive(nil, testEngine(t)) != nil {
		t.Fatal("nil sender should pass through")
	}
	inner := &fakeFullSender{}
	if WrapSensitive(inner, nil) != inner {
		t.Fatal("nil engine should return the sender unchanged")
	}
}

func TestFilterReplyMasksText(t *testing.T) {
	inner := &fakeFullSender{reply: agent.Reply{Content: "增值税发票管理"}}
	w := WrapSensitive(inner, testEngine(t))
	r, err := w.SendMessages(context.Background(), "m", "s", nil, 0)
	if err != nil {
		t.Fatalf("SendMessages: %v", err)
	}
	if r.Content != "增值税***管理" {
		t.Fatalf("Content = %q, want masked", r.Content)
	}
}

func TestFilterReplyMasksTextBlocksAndToolResults(t *testing.T) {
	inner := &fakeFullSender{reply: agent.Reply{Blocks: []agent.ContentBlock{
		agent.NewTextBlock("这里有个发票"),
		agent.NewToolResultBlock("t1", "工具返回了赌博", false),
	}}}
	w := WrapSensitive(inner, testEngine(t))
	r, err := w.SendMessages(context.Background(), "m", "s", nil, 0)
	if err != nil {
		t.Fatalf("SendMessages: %v", err)
	}
	if r.Blocks[0].Text != "这里有个***" {
		t.Fatalf("text block = %q, want masked", r.Blocks[0].Text)
	}
	if r.Blocks[1].Result != "工具返回了***" {
		t.Fatalf("tool_result = %q, want masked", r.Blocks[1].Result)
	}
}

func TestFilterReplyKeepsToolUseInputIntact(t *testing.T) {
	raw := `{"q":"发票"}`
	block := agent.NewToolUseBlockFromJSON("t1", "search", raw)
	inner := &fakeFullSender{reply: agent.Reply{Blocks: []agent.ContentBlock{block}}}
	w := WrapSensitive(inner, testEngine(t))
	r, err := w.SendMessages(context.Background(), "m", "s", nil, 0)
	if err != nil {
		t.Fatalf("SendMessages: %v", err)
	}
	// The tool_use input JSON must survive unmasked — masking would corrupt
	// the argument the tool dispatches on (P8 §3.4).
	if got := r.Blocks[0].Input["q"]; got != "发票" {
		t.Fatalf("tool_use input = %v, want untouched 发票", got)
	}
}

func TestFilterReplyKeepsThinkingBlockForRoundTrip(t *testing.T) {
	// thinking blocks round-trip to the provider with their signature; the
	// stored text must stay verbatim so the provider can verify it.
	block := agent.NewThinkingBlock("这段思考含发票", "SIG")
	inner := &fakeFullSender{reply: agent.Reply{Blocks: []agent.ContentBlock{block}}}
	w := WrapSensitive(inner, testEngine(t))
	r, err := w.SendMessages(context.Background(), "m", "s", nil, 0)
	if err != nil {
		t.Fatalf("SendMessages: %v", err)
	}
	if r.Blocks[0].Thinking != "这段思考含发票" {
		t.Fatalf("thinking block = %q, want untouched", r.Blocks[0].Thinking)
	}
}

func TestStreamMasksAcrossDeltas(t *testing.T) {
	inner := &fakeFullSender{deltas: []string{"增值", "税发", "票管理"}}
	w := WrapSensitive(inner, testEngine(t))

	var sb strings.Builder
	_, err := w.(agent.StreamingSender).StreamMessages(
		context.Background(), "m", "s", nil, 0,
		func(d string) { sb.WriteString(d) },
		nil,
	)
	if err != nil {
		t.Fatalf("StreamMessages: %v", err)
	}
	if got := sb.String(); got != "增值税***管理" {
		t.Fatalf("streamed text = %q, want 增值税***管理", got)
	}
}

func TestStreamMasksThinkingDeltas(t *testing.T) {
	inner := &fakeFullSender{thinkingDeltas: []string{"这段", "含发票"}}
	w := WrapSensitive(inner, testEngine(t))

	var sb strings.Builder
	_, err := w.(agent.StreamingSender).StreamMessages(
		context.Background(), "m", "s", nil, 0,
		nil,
		func(d string) { sb.WriteString(d) },
	)
	if err != nil {
		t.Fatalf("StreamMessages: %v", err)
	}
	if got := sb.String(); got != "这段含***" {
		t.Fatalf("streamed thinking = %q, want 这段含***", got)
	}
}

func TestStreamLeavesToolDeltasUnfiltered(t *testing.T) {
	inner := &fakeFullSender{toolDeltas: []string{`{"q":"发票"}`}}
	w := WrapSensitive(inner, testEngine(t))

	var got string
	_, err := w.(agent.ToolStreamingSender).StreamMessagesWithTools(
		context.Background(), "m", "s", nil, 0, nil, nil,
		func(_, _ string, partialJSON string) { got += partialJSON },
		nil,
	)
	if err != nil {
		t.Fatalf("StreamMessagesWithTools: %v", err)
	}
	if got != `{"q":"发票"}` {
		t.Fatalf("tool delta = %q, want untouched", got)
	}
}

func TestLowEffortAndNoReasoningStayWrapped(t *testing.T) {
	inner := &fakeFullSender{}
	w := WrapSensitive(inner, testEngine(t))

	le := w.(agent.LowEffortSender).LowEffort()
	if _, ok := le.(filteringSender); !ok {
		t.Fatalf("LowEffort() = %T, want filteringSender", le)
	}
	nr := w.(agent.NoReasoningSender).NoReasoning()
	if _, ok := nr.(filteringSender); !ok {
		t.Fatalf("NoReasoning() = %T, want filteringSender", nr)
	}
}
