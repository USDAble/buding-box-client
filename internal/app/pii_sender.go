package app

import (
	"context"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/pii"
)

type privacyContextKey struct{}

// WithPrivacy records whether this provider call belongs to a privacy-mode
// session. The sender is shared across sessions, so the switch must be scoped
// to one call rather than stored on the decorator.
func WithPrivacy(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, privacyContextKey{}, enabled)
}

// WrapPII masks user-authored phone numbers in the provider-bound message
// copy. It never mutates Agent history or provider responses.
func WrapPII(s agent.Sender) agent.Sender {
	if s == nil {
		return nil
	}
	return piiSender{inner: s}
}

type piiSender struct {
	inner agent.Sender
}

func (p piiSender) outgoing(ctx context.Context, messages []agent.Message) []agent.Message {
	enabled, _ := ctx.Value(privacyContextKey{}).(bool)
	if !enabled {
		return messages
	}

	var copied []agent.Message
	for i, message := range messages {
		if message.Role != agent.RoleUser {
			continue
		}

		maskedContent := pii.MaskPhones(message.Content)
		var maskedBlocks []agent.ContentBlock
		for j, block := range message.Blocks {
			if block.Type != "text" {
				continue
			}
			masked := pii.MaskPhones(block.Text)
			if masked == block.Text {
				continue
			}
			if maskedBlocks == nil {
				maskedBlocks = append([]agent.ContentBlock(nil), message.Blocks...)
			}
			maskedBlocks[j].Text = masked
		}

		if maskedContent == message.Content && maskedBlocks == nil {
			continue
		}
		if copied == nil {
			copied = append([]agent.Message(nil), messages...)
		}
		copied[i].Content = maskedContent
		if maskedBlocks != nil {
			copied[i].Blocks = maskedBlocks
		}
	}
	if copied == nil {
		return messages
	}
	return copied
}

func (p piiSender) SendMessages(ctx context.Context, model, system string, messages []agent.Message, maxTokens int) (agent.Reply, error) {
	return p.inner.SendMessages(ctx, model, system, p.outgoing(ctx, messages), maxTokens)
}

func (p piiSender) StreamMessages(
	ctx context.Context,
	model, system string,
	messages []agent.Message,
	maxTokens int,
	onChunk func(string),
	onThinking func(string),
) (agent.Reply, error) {
	streaming, ok := p.inner.(agent.StreamingSender)
	if !ok {
		return p.SendMessages(ctx, model, system, messages, maxTokens)
	}
	return streaming.StreamMessages(ctx, model, system, p.outgoing(ctx, messages), maxTokens, onChunk, onThinking)
}

func (p piiSender) SendMessagesWithTools(
	ctx context.Context,
	model, system string,
	messages []agent.Message,
	maxTokens int,
	tools []agent.ToolDefinition,
) (agent.Reply, error) {
	toolSender, ok := p.inner.(agent.ToolSender)
	if !ok {
		return p.SendMessages(ctx, model, system, messages, maxTokens)
	}
	return toolSender.SendMessagesWithTools(ctx, model, system, p.outgoing(ctx, messages), maxTokens, tools)
}

func (p piiSender) StreamMessagesWithTools(
	ctx context.Context,
	model, system string,
	messages []agent.Message,
	maxTokens int,
	tools []agent.ToolDefinition,
	onChunk func(string),
	onToolDelta agent.ToolInputDeltaFunc,
	onThinking agent.ThinkingDeltaFunc,
) (agent.Reply, error) {
	streaming, ok := p.inner.(agent.ToolStreamingSender)
	if !ok {
		return p.SendMessagesWithTools(ctx, model, system, messages, maxTokens, tools)
	}
	return streaming.StreamMessagesWithTools(ctx, model, system, p.outgoing(ctx, messages), maxTokens, tools, onChunk, onToolDelta, onThinking)
}

func (p piiSender) LowEffort() agent.Sender {
	low, ok := p.inner.(agent.LowEffortSender)
	if !ok {
		return p
	}
	return WrapPII(low.LowEffort())
}

func (p piiSender) NoReasoning() agent.Sender {
	noReasoning, ok := p.inner.(agent.NoReasoningSender)
	if !ok {
		return p
	}
	return WrapPII(noReasoning.NoReasoning())
}

var (
	_ agent.Sender              = piiSender{}
	_ agent.StreamingSender     = piiSender{}
	_ agent.ToolSender          = piiSender{}
	_ agent.ToolStreamingSender = piiSender{}
	_ agent.LowEffortSender     = piiSender{}
	_ agent.NoReasoningSender   = piiSender{}
)
