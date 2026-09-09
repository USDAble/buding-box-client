package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

type piiCaptureSender struct {
	messages []agent.Message
	reply    agent.Reply
}

func (s *piiCaptureSender) capture(messages []agent.Message) (agent.Reply, error) {
	s.messages = messages
	return s.reply, nil
}

func (s *piiCaptureSender) SendMessages(_ context.Context, _, _ string, messages []agent.Message, _ int) (agent.Reply, error) {
	return s.capture(messages)
}

func (s *piiCaptureSender) StreamMessages(_ context.Context, _, _ string, messages []agent.Message, _ int, _ func(string), _ func(string)) (agent.Reply, error) {
	return s.capture(messages)
}

func (s *piiCaptureSender) SendMessagesWithTools(_ context.Context, _, _ string, messages []agent.Message, _ int, _ []agent.ToolDefinition) (agent.Reply, error) {
	return s.capture(messages)
}

func (s *piiCaptureSender) StreamMessagesWithTools(_ context.Context, _, _ string, messages []agent.Message, _ int, _ []agent.ToolDefinition, _ func(string), _ agent.ToolInputDeltaFunc, _ agent.ThinkingDeltaFunc) (agent.Reply, error) {
	return s.capture(messages)
}

func (s *piiCaptureSender) LowEffort() agent.Sender   { return s }
func (s *piiCaptureSender) NoReasoning() agent.Sender { return s }

func TestWrapPIIKeepsFullCapability(t *testing.T) {
	wrapped := WrapPII(&piiCaptureSender{})
	for name, ok := range map[string]bool{
		"StreamingSender":     implements[agent.StreamingSender](wrapped),
		"ToolSender":          implements[agent.ToolSender](wrapped),
		"ToolStreamingSender": implements[agent.ToolStreamingSender](wrapped),
		"LowEffortSender":     implements[agent.LowEffortSender](wrapped),
		"NoReasoningSender":   implements[agent.NoReasoningSender](wrapped),
	} {
		if !ok {
			t.Errorf("wrap dropped %s", name)
		}
	}
	if WrapPII(nil) != nil {
		t.Fatal("nil sender should pass through")
	}
}

func implements[T any](value any) bool {
	_, ok := value.(T)
	return ok
}

func TestWrapPIIMasksProviderCopyWithoutMutatingHistory(t *testing.T) {
	toolResult := agent.NewToolResultBlock("t1", "tool returned 13900005678", false)
	messages := []agent.Message{
		agent.NewUserMessage("call 13800001234"),
		agent.NewAssistantMessage("assistant 13700001234"),
		{Role: agent.RoleUser, Blocks: []agent.ContentBlock{
			agent.NewTextBlock("photo owner 13600001234"),
			toolResult,
			agent.NewToolUseBlock("t2", "lookup", map[string]any{"phone": "13500001234"}),
		}},
	}
	original := cloneMessages(messages)
	inner := &piiCaptureSender{}

	_, err := WrapPII(inner).SendMessages(WithPrivacy(context.Background(), true), "m", "s", messages, 0)
	if err != nil {
		t.Fatalf("SendMessages: %v", err)
	}
	if got := inner.messages[0].Content; got != "call 138****1234" {
		t.Errorf("user content = %q", got)
	}
	if got := inner.messages[1].Content; got != "assistant 13700001234" {
		t.Errorf("assistant content changed: %q", got)
	}
	if got := inner.messages[2].Blocks[0].Text; got != "photo owner 136****1234" {
		t.Errorf("user text block = %q", got)
	}
	if got := inner.messages[2].Blocks[1].Result; got != toolResult.Result {
		t.Errorf("tool result changed: %q", got)
	}
	if got := inner.messages[2].Blocks[2].Input["phone"]; got != "13500001234" {
		t.Errorf("tool input changed: %v", got)
	}
	if !reflect.DeepEqual(messages, original) {
		t.Fatalf("original messages mutated:\n got: %#v\nwant: %#v", messages, original)
	}
}

func TestWrapPIIDisabledLeavesMessagesUntouched(t *testing.T) {
	messages := []agent.Message{agent.NewUserMessage("13800001234")}
	inner := &piiCaptureSender{}
	ctx := WithPrivacy(WithPrivacy(context.Background(), true), false)
	_, err := WrapPII(inner).SendMessages(ctx, "m", "s", messages, 0)
	if err != nil {
		t.Fatalf("SendMessages: %v", err)
	}
	if &inner.messages[0] != &messages[0] {
		t.Fatal("disabled path should pass the original slice through")
	}
	if got := inner.messages[0].Content; got != "13800001234" {
		t.Fatalf("content = %q, want unchanged", got)
	}
}

func TestWrapPIIMasksEveryTransport(t *testing.T) {
	ctx := WithPrivacy(context.Background(), true)
	messages := []agent.Message{agent.NewUserMessage("13800001234")}
	inner := &piiCaptureSender{}
	wrapped := WrapPII(inner)

	if _, err := wrapped.(agent.StreamingSender).StreamMessages(ctx, "m", "s", messages, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	assertCapturedPhone(t, inner)
	if _, err := wrapped.(agent.ToolSender).SendMessagesWithTools(ctx, "m", "s", messages, 0, nil); err != nil {
		t.Fatal(err)
	}
	assertCapturedPhone(t, inner)
	if _, err := wrapped.(agent.ToolStreamingSender).StreamMessagesWithTools(ctx, "m", "s", messages, 0, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	assertCapturedPhone(t, inner)
}

func TestWrapPIILowEffortAndNoReasoningStayWrapped(t *testing.T) {
	wrapped := WrapPII(&piiCaptureSender{})
	if _, ok := wrapped.(agent.LowEffortSender).LowEffort().(piiSender); !ok {
		t.Fatal("LowEffort dropped PII wrapper")
	}
	if _, ok := wrapped.(agent.NoReasoningSender).NoReasoning().(piiSender); !ok {
		t.Fatal("NoReasoning dropped PII wrapper")
	}
}

func TestPIIAndSensitiveDecoratorsKeepTheirDirections(t *testing.T) {
	inner := &piiCaptureSender{reply: agent.Reply{Content: "增值税发票管理 13800001234"}}
	wrapped := WrapPII(WrapSensitive(inner, testEngine(t)))
	reply, err := wrapped.SendMessages(
		WithPrivacy(context.Background(), true),
		"m", "s", []agent.Message{agent.NewUserMessage("call 13800001234")}, 0,
	)
	if err != nil {
		t.Fatalf("SendMessages: %v", err)
	}
	if got := inner.messages[0].Content; got != "call 138****1234" {
		t.Fatalf("provider input = %q", got)
	}
	if reply.Content != "增值税***管理 13800001234" {
		t.Fatalf("reply = %q", reply.Content)
	}
}

func assertCapturedPhone(t *testing.T, inner *piiCaptureSender) {
	t.Helper()
	if got := inner.messages[0].Content; got != "138****1234" {
		t.Fatalf("captured content = %q", got)
	}
}

func cloneMessages(messages []agent.Message) []agent.Message {
	out := append([]agent.Message(nil), messages...)
	for i := range out {
		out[i].Blocks = append([]agent.ContentBlock(nil), out[i].Blocks...)
	}
	return out
}
