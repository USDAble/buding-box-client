import { beforeEach, describe, expect, it } from 'vitest'
import { en, locale, tr, zh } from './i18n'
import { TURN_ERROR_KEYS, turnErrorKey, turnErrorView } from './turnError'

beforeEach(() => locale.set('zh'))

// These tests feed turnErrorView LITERAL event bodies, the way internal/server writes
// them ({"type":"turn_error","session_id":…,"code":…,"error":…}), instead of building
// them with a helper. That is deliberate, and it is V-57's lesson: when the fixture and
// the producer share a builder, a wrong field name round-trips and every nail stays
// green. It is also V-66's lesson — the nail has to cover the hop from the wire field
// into the copy, not just a pure function's output.
describe('turnErrorView reads the event, not the sentence', () => {
  it('answers the gateway 402 with one Chinese sentence and no raw JSON', () => {
    const ev = {
      type: 'turn_error',
      session_id: 's1',
      code: 'insufficient_credits',
      error: 'openai: HTTP 402: {"code":"insufficient_credits","message":"balance too low"}',
    }

    const { text, code } = turnErrorView(ev, tr('turn_error.unknown'))

    expect(code).toBe('insufficient_credits')
    expect(text).toBe(tr('turn_error.insufficient_credits'))
    // L-C4c's actual complaint: the user must not be shown the gateway's body.
    expect(text).not.toMatch(/HTTP|402|\{/)
  })

  it('shows the withdrawn-model code the sentence the picker already shows', () => {
    const ev = {
      type: 'turn_error',
      session_id: 's1',
      code: 'model_withdrawn',
      error: 'model "buding-cloud-retired" is no longer in the model list this build was given — pick one from the list',
    }

    expect(turnErrorView(ev, tr('turn_error.unknown')).text).toBe(tr('session.model_withdrawn'))
  })

  it('degrades to the server sentence for a code nobody knows', () => {
    // Bounded degradation (§3.9) with a named target: the server's own sentence. Not a
    // blank notice (the failure would disappear) and not "unknown error" (which hides
    // which failure it was).
    const ev = { type: 'turn_error', code: 'brand_new_code', error: 'upstream exploded' }

    expect(turnErrorView(ev, tr('turn_error.unknown')).text).toBe('upstream exploded')
  })

  it('uses the caller fallback when the event carries no sentence either', () => {
    expect(turnErrorView({ type: 'turn_error' }, tr('turn_error.unknown')).text).toBe(tr('turn_error.unknown'))
    expect(turnErrorView({ type: 'turn_error', error: '' }, 'other').text).toBe('other')
  })

  it('treats an absent or non-string code as no code', () => {
    expect(turnErrorView({ type: 'turn_error', error: 'boom' }, 'f').code).toBe('')
    // A future server sending null must not blank the transcript.
    expect(turnErrorView({ type: 'turn_error', code: null, error: 'boom' }, 'f').text).toBe('boom')
  })
})

describe('the mapping table is the single owner of code → copy', () => {
  it('has copy in both locales for every key it can return', () => {
    const keys = Object.values(TURN_ERROR_KEYS)
    expect(keys.length).toBeGreaterThan(0)
    for (const key of keys) {
      // A typo here renders the key itself where the sentence belongs — the failure
      // would look like a broken UI rather than a broken mapping.
      expect(en[key], `en is missing ${key}`).toBeTruthy()
      expect(zh[key], `zh is missing ${key}`).toBeTruthy()
    }
  })

  it('does not answer for a code it does not know', () => {
    expect(turnErrorKey('not_a_registered_code')).toBeNull()
  })
})
