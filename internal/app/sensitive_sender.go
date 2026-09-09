package app

import (
	"context"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

// filteringSender decorates any agent.Sender, masking the visible text the
// model returns through the P7 engine. It is a decorator, not a change to the
// agent loop: internal/agent is upstream core, and editing it would conflict
// on every upstream merge. internal/app is the fork's single Sender-assembly
// layer, so the wrap belongs here.
//
// OCTO-FORK: P8 敏感词接入 — see
// dev-docs-usdable/需求/2260906/技术方案/P8-敏感词接入.md §3.3.

// WrapSensitive wraps s so every visible text it returns is filtered through
// e: buffered assistant text, streamed text deltas, tool-result text, and the
// streamed thinking deltas (the user-visible copy).
//
// Deliberately NOT filtered (see P8 §3.4):
//   - tool_use input JSON — the UI never shows raw arguments, and masking
//     would corrupt the JSON the tool dispatches on.
//   - thinking blocks stored in Reply.Blocks — those round-trip to the
//     provider on the next request and must keep the original signature/text;
//     only the streamed onThinking deltas are filtered.
func WrapSensitive(s agent.Sender, e *sensitive.Engine) agent.Sender {
	if s == nil || e == nil {
		return s
	}
	return filteringSender{inner: s, engine: e}
}

type filteringSender struct {
	inner  agent.Sender
	engine *sensitive.Engine
}

// filterReply masks the visible text in a reply. Blocks are copied so the
// caller's slice is never mutated in place.
func (f filteringSender) filterReply(r agent.Reply) agent.Reply {
	if r.Content != "" {
		r.Content = f.engine.Filter(r.Content).Text
	}
	if len(r.Blocks) == 0 {
		return r
	}
	blocks := make([]agent.ContentBlock, len(r.Blocks))
	for i, b := range r.Blocks {
		blocks[i] = b
		switch b.Type {
		case "text":
			blocks[i].Text = f.engine.Filter(b.Text).Text
		case "tool_result":
			blocks[i].Result = f.engine.Filter(b.Result).Text
			// "thinking" is left intact: it must round-trip to the provider with
			// its original signature (see WrapSensitive doc). "tool_use" input
			// JSON is never filtered. "image" carries no text.
		}
	}
	r.Blocks = blocks
	return r
}

// SendMessages implements agent.Sender.
func (f filteringSender) SendMessages(ctx context.Context, model, system string, msgs []agent.Message, maxTokens int) (agent.Reply, error) {
	r, err := f.inner.SendMessages(ctx, model, system, msgs, maxTokens)
	if err != nil {
		return r, err
	}
	return f.filterReply(r), nil
}

// StreamMessages implements agent.StreamingSender. Streamed text and thinking
// deltas are filtered incrementally via StreamFilter (the earliest possible
// masking); the returned Reply is filtered too, since it is the authoritative
// copy the agent writes into history.
func (f filteringSender) StreamMessages(
	ctx context.Context,
	model, system string,
	msgs []agent.Message,
	maxTokens int,
	onChunk func(string),
	onThinking func(string),
) (agent.Reply, error) {
	ss, ok := f.inner.(agent.StreamingSender)
	if !ok {
		return f.SendMessages(ctx, model, system, msgs, maxTokens)
	}

	text := f.engine.NewStream()
	thought := f.engine.NewStream()
	defer func() {
		if onChunk != nil {
			if tail := text.Flush(); tail != "" {
				onChunk(tail)
			}
		}
		if onThinking != nil {
			if tail := thought.Flush(); tail != "" {
				onThinking(tail)
			}
		}
	}()

	wrapChunk := onChunk
	if onChunk != nil {
		wrapChunk = func(delta string) {
			if out := text.Write(delta); out != "" {
				onChunk(out)
			}
		}
	}
	wrapThinking := onThinking
	if onThinking != nil {
		wrapThinking = func(delta string) {
			if out := thought.Write(delta); out != "" {
				onThinking(out)
			}
		}
	}

	r, err := ss.StreamMessages(ctx, model, system, msgs, maxTokens, wrapChunk, wrapThinking)
	if err != nil {
		return r, err
	}
	return f.filterReply(r), nil
}

// SendMessagesWithTools implements agent.ToolSender.
func (f filteringSender) SendMessagesWithTools(
	ctx context.Context,
	model, system string,
	msgs []agent.Message,
	maxTokens int,
	tools []agent.ToolDefinition,
) (agent.Reply, error) {
	ts, ok := f.inner.(agent.ToolSender)
	if !ok {
		return f.SendMessages(ctx, model, system, msgs, maxTokens)
	}
	r, err := ts.SendMessagesWithTools(ctx, model, system, msgs, maxTokens, tools)
	if err != nil {
		return r, err
	}
	return f.filterReply(r), nil
}

// StreamMessagesWithTools implements agent.ToolStreamingSender. Tool-argument
// deltas (onToolDelta) pass through untouched — they are the raw JSON the
// agent accumulates for dispatch, not user-visible text.
func (f filteringSender) StreamMessagesWithTools(
	ctx context.Context,
	model, system string,
	msgs []agent.Message,
	maxTokens int,
	tools []agent.ToolDefinition,
	onChunk func(string),
	onToolDelta agent.ToolInputDeltaFunc,
	onThinking agent.ThinkingDeltaFunc,
) (agent.Reply, error) {
	tss, ok := f.inner.(agent.ToolStreamingSender)
	if !ok {
		return f.SendMessagesWithTools(ctx, model, system, msgs, maxTokens, tools)
	}

	text := f.engine.NewStream()
	thought := f.engine.NewStream()
	defer func() {
		if onChunk != nil {
			if tail := text.Flush(); tail != "" {
				onChunk(tail)
			}
		}
		if onThinking != nil {
			if tail := thought.Flush(); tail != "" {
				onThinking(tail)
			}
		}
	}()

	wrapChunk := onChunk
	if onChunk != nil {
		wrapChunk = func(delta string) {
			if out := text.Write(delta); out != "" {
				onChunk(out)
			}
		}
	}
	wrapThinking := onThinking
	if onThinking != nil {
		wrapThinking = func(delta string) {
			if out := thought.Write(delta); out != "" {
				onThinking(out)
			}
		}
	}

	r, err := tss.StreamMessagesWithTools(ctx, model, system, msgs, maxTokens, tools, wrapChunk, onToolDelta, wrapThinking)
	if err != nil {
		return r, err
	}
	return f.filterReply(r), nil
}

// LowEffort implements agent.LowEffortSender by delegating to the inner
// sender when it supports it, keeping the sensitive filter wrapped around the
// cheaper variant so suggestion/title calls stay filtered.
func (f filteringSender) LowEffort() agent.Sender {
	le, ok := f.inner.(agent.LowEffortSender)
	if !ok {
		return f
	}
	return WrapSensitive(le.LowEffort(), f.engine)
}

// NoReasoning implements agent.NoReasoningSender (see LowEffort).
func (f filteringSender) NoReasoning() agent.Sender {
	nr, ok := f.inner.(agent.NoReasoningSender)
	if !ok {
		return f
	}
	return WrapSensitive(nr.NoReasoning(), f.engine)
}

// Compile-time assertions: filteringSender keeps every capability the agent
// can type-assert. This is the guard against silent capability downgrade that
// P8 §7 calls out — if a new Sender interface is added upstream, this block
// (and the test) must grow with it.
var (
	_ agent.Sender              = filteringSender{}
	_ agent.StreamingSender     = filteringSender{}
	_ agent.ToolSender          = filteringSender{}
	_ agent.ToolStreamingSender = filteringSender{}
	_ agent.LowEffortSender     = filteringSender{}
	_ agent.NoReasoningSender   = filteringSender{}
)
