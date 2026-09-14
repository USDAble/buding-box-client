import { describe, expect, it } from 'vitest'
import { fmtDur, fmtTokens, thinkingTokenSegment, turnSummarySegments } from './turnSummary'

// PR-5d2 / G4: the per-turn summary must not print a number nobody measured.
//
// WHY THIS FILE EXISTS. Before PR-5d2 the stand-in sent no usage frame at all,
// so `complete.tokens` was always 0 and the chat line read "⏱ 1s, 0 tokens" —
// a zero presented as a measurement, which is the display half of the same
// defect §3.9 names on the backend side. The backend already omits `cache_pct`
// when it has nothing to report; this is that rule applied to the token count.
//
// The counter-nail below (a real count still prints) is what stops the cheap
// reading of this rule — "delete the token segment" — from passing.

describe('turnSummarySegments', () => {
  // 1. The measured facts appear, in order, when everything was reported.
  it('shows duration, tokens and cache when all were reported', () => {
    expect(turnSummarySegments(1234, 340, 40)).toEqual(['1s', '340 tokens', 'cache 40%'])
  })

  // 2. THE NAIL: no usage reported (tokens === 0) means no token segment. This is
  // the state every developer turn was in until the fixture started sending a
  // usage frame, and the line must not claim a measurement it does not have.
  it('omits the token segment when the gateway reported no usage', () => {
    const segs = turnSummarySegments(2000, 0)
    expect(segs).toEqual(['2s'])
    expect(segs.join(', ')).not.toContain('tokens')
  })

  // 3. The counter-nail. A rule implemented as "always drop the token segment"
  // satisfies #2 and destroys the feature, so a reported count has to survive.
  it('keeps the token segment when a usage count was reported', () => {
    expect(turnSummarySegments(2000, 4096).join(', ')).toContain('4.1k tokens')
  })

  // 4. Time is measured locally, so the line is never empty — a summary notice
  // that rendered nothing would anchor an empty bubble to the reply.
  it('always has at least the elapsed time', () => {
    expect(turnSummarySegments(0, 0).length).toBeGreaterThan(0)
  })

  // 5. cache_pct follows the backend's own contract: the field is OMITTED (not
  // 0) when there was no cache activity, so `undefined` must not become
  // "cache 0%". A build that defaulted it would claim a measurement.
  it('omits the cache segment when the field is absent, and keeps a real 0%', () => {
    expect(turnSummarySegments(1000, 5).join(', ')).not.toContain('cache')
    expect(turnSummarySegments(1000, 5, 0).join(', ')).toContain('cache 0%')
  })
})

describe('thinkingTokenSegment', () => {
  // 6. The provenance distinction. The uplink number came back with a usage frame
  // and is exact; the tilde belongs to the chars/4 output estimate only. Before
  // PR-5d2 BOTH carried it, which made a reported number look like a guess.
  it('marks the output estimate as approximate and the reported uplink as exact', () => {
    expect(thinkingTokenSegment(120, 0)).toBe('↓ ~120 tokens')
    expect(thinkingTokenSegment(0, 4096)).toBe('↑ 4.1k tokens')
  })

  // 7. The estimate wins while it exists: mid-turn it is the only number there is,
  // and swapping to the (larger, staler) uplink figure mid-stream would make the
  // counter jump backwards.
  it('prefers the live output estimate over the uplink figure', () => {
    expect(thinkingTokenSegment(120, 4096)).toBe('↓ ~120 tokens')
  })

  // 8. Neither available => a bare arrow, not "0 tokens" — the same rule as #2,
  // applied to the other readout so the two cannot drift apart.
  it('shows a bare arrow rather than a zero when nothing is known', () => {
    expect(thinkingTokenSegment(0, 0)).toBe('↑')
  })
})

describe('formatting', () => {
  it('renders durations under a minute in seconds, longer ones in minutes', () => {
    expect(fmtDur(45)).toBe('45s')
    expect(fmtDur(90)).toBe('1m30s')
  })

  it('abbreviates thousands so a long turn does not widen the row', () => {
    expect(fmtTokens(999)).toBe('999')
    expect(fmtTokens(4096)).toBe('4.1k')
  })
})
