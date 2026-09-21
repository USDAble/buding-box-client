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

// V-91. The registry (中台接口清单 §2.4) used to have one answer for two codes and the
// fallback for everything else — and on this path the fallback is the provider's
// sentence, i.e. `openai: HTTP 403: {"code":"safety_blocked",…}` on a Chinese screen.
// These are the codes a turn can actually fail with; the SMS and activation codes are
// control-plane only and cannot reach turn_error, so inventing copy for them here would
// map a path they never travel.
const GATEWAY_CODES = [
	'confidential_model_required',
	'confidential_model_unavailable',
  'model_withdrawn',
  'insufficient_credits',
  'rate_limited',
  'maintenance',
  'plan_expired',
  'model_not_allowed',
  'feature_not_entitled',
  'safety_blocked',
  'content_restricted',
  'model_not_found',
  'request_in_progress',
  'duplicate_request',
  'invalid_request',
  'internal_error',
  'upstream_unavailable',
]

describe('no code the client can name reaches the server sentence (V-91)', () => {
  it.each(GATEWAY_CODES)('%s is answered with local copy, never the raw body', (code) => {
    // The body is the shape internal/provider/openai's HTTPError.Error() produces, which
    // is what internal/server puts in turn_error.error. If the mapping misses, this is
    // exactly what the bubble shows.
    const ev = {
      type: 'turn_error',
      session_id: 's1',
      code,
      error: `openai: HTTP 403: {"code":"${code}","message":"refused"}`,
    }

    const key = turnErrorKey(code)
    if (key === null) {
      // Say the thing that is wrong. Without this the next line dereferences null inside
      // interpolateBrand and the report is a stack trace in i18n.ts, which reads like a
      // broken key rather than a missing mapping — the diagnosis the test exists to give.
      throw new Error(`no mapping for ${code}: the user would be shown the provider's raw body`)
    }
    expect(en[key], `en is missing ${key}`).toBeTruthy()
    expect(zh[key], `zh is missing ${key}`).toBeTruthy()

    const { text } = turnErrorView(ev, tr('turn_error.unknown'))

    expect(text).toBe(tr(key))
    expect(text).not.toMatch(/openai|HTTP|403|\{/)
  })

  it('answers the two model codes with the sentence the picker already shows', () => {
    // Reuse rather than a second key with the same words (§3.8): the picker says this
    // for a model the catalog no longer offers, and the gateway says it with its own code.
    expect(turnErrorKey('model_not_found')).toBe('session.model_withdrawn')
    expect(turnErrorKey('model_withdrawn')).toBe('session.model_withdrawn')
  })

  it('keeps temporary catalog and eligibility refusals distinct from withdrawal', () => {
    expect(turnErrorKey('catalog_unavailable')).toBe('turn_error.catalog_unavailable')
    expect(turnErrorKey('model_unavailable')).toBe('session.model_unavailable')
    for (const code of ['catalog_unavailable', 'model_unavailable']) {
      const key = turnErrorKey(code)!
      expect(en[key]).toBeTruthy()
      expect(zh[key]).toBeTruthy()
      expect(zh[key]).not.toContain('已下架')
    }
  })

  it('leaves the 401 pair to the behaviour it needs, not to a sentence', () => {
    // Pinned as ABSENT, not as an oversight (V-92): this path has no refresh-and-replay
    // and no clear-the-credential step, so copy would tell the user to retry a turn that
    // cannot succeed. Delete this test when V-92 lands the behaviour.
    expect(turnErrorKey('unauthorized')).toBeNull()
    expect(turnErrorKey('token_expired')).toBeNull()
  })
})
