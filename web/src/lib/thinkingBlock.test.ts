import { describe, it, expect, beforeEach } from 'vitest'
import { get } from 'svelte/store'
import { chatMessages, commitThinking } from './stores'

// L-C3b's exception branch (需求基线 C10): "无思考块的模型不得出现空块".
//
// PR-5b1's nails stop at the sender — they prove the trace reaches onThinking
// and stays out of the answer. What they cannot see is what the UI does when a
// model never reasons at all, and that is the branch a user meets most often:
// most models in the catalog stream no reasoning_content, so the empty case is
// the common one, not the exotic one.
//
// The guard is one line in upstream's commitThinking (`if (!t) return`), which
// makes it exactly the kind of rule that is invisible to review and cheap to
// lose: anything that moves the trim, or that pushes the block before checking
// the text, turns every non-reasoning reply into a blank "Thoughts" card. That
// is why it gets a nail rather than a comment.
//
// Scope (开发规范 §3.10): this is the UI half only. Whether a *given* model
// reasons is the platform's catalogue, and whether the trace survives the turn
// is PR-5b1's e2e nail in internal/productruntime/gateway_reasoning_test.go.

const SID = 'session-1'

describe('commitThinking', () => {
  beforeEach(() => {
    chatMessages.set({ [SID]: [] })
  })

  it('creates no block when the model sent no reasoning at all', () => {
    commitThinking(SID, '')

    expect(get(chatMessages)[SID]).toEqual([])
  })

  it('creates no block for whitespace-only reasoning', () => {
    // Not hypothetical: a gateway that opens a reasoning segment and sends a
    // single "\n" delta is a well-formed empty trace, and a blank Thoughts card
    // is the visible result of treating it as content.
    commitThinking(SID, '\n  \t ')

    expect(get(chatMessages)[SID]).toEqual([])
  })

  it('creates exactly one thinking block, trimmed, for a real trace', () => {
    commitThinking(SID, '  weighing the request  ')

    const msgs = get(chatMessages)[SID]
    expect(msgs).toHaveLength(1)
    expect(msgs[0].type).toBe('thinking')
    expect(msgs[0].thinking).toBe('weighing the request')
  })

  it('keeps the trace in its own block rather than appending it to the answer', () => {
    // C10's main clause, at the UI boundary: the two are separate messages, so
    // a trace can never be rendered as part of the reply text.
    chatMessages.set({
      [SID]: [{ type: 'assistant', content: 'the answer', streaming: true }],
    })

    commitThinking(SID, 'weighing the request')

    const msgs = get(chatMessages)[SID]
    expect(msgs.map((m: any) => m.type)).toEqual(['assistant', 'thinking'])
    expect(msgs[0].content).toBe('the answer')
    expect(msgs[1].thinking).toBe('weighing the request')
  })

  it('stops the previous reply from blinking when a trace is committed', () => {
    // The caret belongs to the in-flight reply; once reasoning becomes its own
    // segment the caret must follow it instead of blinking behind content that
    // no longer belongs to it.
    chatMessages.set({
      [SID]: [{ type: 'assistant', content: 'partial', streaming: true }],
    })

    commitThinking(SID, 'weighing the request')

    expect(get(chatMessages)[SID][0].streaming).toBe(false)
  })
})
