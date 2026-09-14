// The per-turn summary line, e.g. "1.2s, 340 tokens, cache 40%".
//
// WHY THIS IS A MODULE AND NOT TWO TEMPLATE LITERALS INSIDE ChatView. The rule
// it encodes is a judgement about honesty, not a formatting preference: a
// number that was never measured must not be printed (§3.9). `complete` carries
// `tokens` as 0 when the gateway reported no usage — which the stand-in did on
// every turn until PR-5d2 — and the line used to say "0 tokens" anyway, which
// reads as a measurement. Same reasoning the backend already applies to
// `cache_pct`: it omits the field rather than sending 0.
//
// Keeping that decision next to the strings instead of inside a 3,500-line
// component is what makes it assertable — 开发规范 §6.4.3 records a PR whose
// branches were all correct and unreachable, so "the condition is obviously
// right" is not a nail.
//
// The two call sites render with different separators (the chat notice reads
// "1.2s, 340 tokens"; the silent-panel chip reads "1.2s · 340 tokens"), so they
// join the SAME segment list themselves. What is shared is which segments exist.

export function fmtDur(seconds: number): string {
  return seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m${seconds % 60}s`
}

export function fmtTokens(n: number): string {
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : `${n}`
}

/**
 * The facts a turn summary may show, in order. Never empty: the elapsed time is
 * measured locally and is therefore always available.
 *
 * @param durationMs wall-clock time the turn took
 * @param tokens     the provider-reported total. 0 means "no usage reported"
 *                   and is omitted, not printed as zero.
 * @param cachePct   cache utilisation, when the backend reported any. Omitted
 *                   rather than defaulted, matching the backend's own rule.
 */
export function turnSummarySegments(durationMs: number, tokens: number, cachePct?: number): string[] {
  const segments = [fmtDur(Math.round(durationMs / 1000))]
  if (tokens > 0) segments.push(`${fmtTokens(tokens)} tokens`)
  if (typeof cachePct === 'number') segments.push(`cache ${cachePct}%`)
  return segments
}

/**
 * The token half of the live "thinking" line: the segment that follows the
 * elapsed clock, e.g. "↓ ~120 tokens" / "↑ 4.1k tokens" / "↑".
 *
 * The two numbers have DIFFERENT PROVENANCE and the "~" is what tells them
 * apart. `thinkTokens` is this app's own chars/4 estimate of the reply being
 * written — the provider has not reported anything mid-turn — so it is marked
 * approximate. `ctxTokens` is the reported context occupancy that arrived with a
 * usage frame, so it is exact and must not carry the tilde; before PR-5d2 it did,
 * which understated what the number is.
 *
 * When neither is available the output is a bare arrow rather than "0 tokens":
 * the same rule turnSummarySegments applies, so neither readout can invent a
 * measurement (开发规范 §3.9). A bare arrow is honest about "nothing yet".
 */
export function thinkingTokenSegment(thinkTokens: number, ctxTokens: number): string {
  if (thinkTokens > 0) return `↓ ~${fmtTokens(thinkTokens)} tokens`
  if (ctxTokens > 0) return `↑ ${fmtTokens(ctxTokens)} tokens`
  return '↑'
}
