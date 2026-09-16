// The one place a turn failure's CODE becomes the sentence the user reads.
//
// WHY IT EXISTS (PR-5d3 / G3, 需求基线 C8). The server used to send only a sentence,
// and that sentence came from whatever the endpoint answered — for the gateway's 402
// that was `openai: HTTP 402: {"code":"insufficient_credits",…}`, i.e. English raw
// JSON where the requirement asks for one Chinese sentence. C8 puts the copy in the
// frontend (开发规范 §3.8: interface copy lives in the frontend's i18n) and forbids
// rendering the server's `message`, so the code has to travel and this table is what
// turns it into copy.
//
// ONE OWNER, TWO CONSUMERS. `ws_handlers.go` puts turn_error on the wire and two live
// consumers read it — `views/ChatView.svelte` (desktop) and `mobile/chatWiring.ts`.
// Both call turnErrorView below; a second mapping in either of them would be the
// second definition of "what does this code say" (§3.8).
//
// The reversed-looking pair, both deliberate:
//
//   - A known code IGNORES the server's sentence, even when it is a fine sentence.
//     C8 rule 1: the client must not use `message` as UI copy. A server that improves
//     its wording must not change what the user sees.
//   - An unknown code FALLS BACK to that sentence rather than printing "unknown
//     error". Bounded degradation (§3.9) has to name its target: the server's own
//     sentence is a real diagnosis, and replacing it with a generic one would hide
//     which failure happened.
import { tr } from './i18n'

// code → i18n key. The code values are the control plane's (中台接口清单 §2.4, the
// single registry): the gateway's own codes and the client-side fail-closed ones.
//
// COVERAGE (V-91). The table used to answer for two codes, so everything else in the
// registry reached the fallback below — and for a gateway refusal that fallback is
// `openai: HTTP 403: {"code":"safety_blocked",…}`, raw English JSON on a Chinese
// screen. C8's rule is the client must not use `message` as copy, which rules that out
// for any code we can name; the fallback stays for codes nobody has seen yet. So every
// code in §2.4 now answers here, with three deliberate reuses instead of new keys
// (§3.8 — a second key holding the same sentence is the copy-drifting twin):
//
//   - `model_not_found` → `session.model_withdrawn`: the picker already says "this
//     model is gone, pick another" for the same situation (B8 rule 2 / PR-5e).
//   - `content_restricted` → `turn_error.safety_blocked`: two platform codes, one fact
//     and one thing for the user to do (edit the message and send again).
//   - `upstream_unavailable` → `product.tier.upstream_unavailable`: the control plane's
//     own 5xx tier sentence, unchanged by the fact that the gateway also returns it.
//
// DELIBERATELY ABSENT: `unauthorized` / `token_expired`. On this path their correct
// answer is not a sentence but a refresh-and-replay, and then clearing the credential
// and returning to the interception page when the refresh is refused — none of which
// exists for the gateway (the provider holds a token per turn and has no exchange; see
// V-92 and 中台接口清单 §3.9). A sentence here would tell the user to retry a turn that
// cannot succeed, so the honest state is that this path has no copy until it has the
// behaviour.
export const TURN_ERROR_KEYS: Record<string, string> = {
  insufficient_credits: 'turn_error.insufficient_credits',
  model_withdrawn: 'session.model_withdrawn',
  model_not_found: 'session.model_withdrawn',
  rate_limited: 'turn_error.rate_limited',
  maintenance: 'turn_error.maintenance',
  plan_expired: 'turn_error.plan_expired',
  model_not_allowed: 'turn_error.model_not_allowed',
  feature_not_entitled: 'turn_error.feature_not_entitled',
  safety_blocked: 'turn_error.safety_blocked',
  content_restricted: 'turn_error.safety_blocked',
  request_in_progress: 'turn_error.request_in_progress',
  duplicate_request: 'turn_error.duplicate_request',
  invalid_request: 'turn_error.invalid_request',
  internal_error: 'turn_error.internal_error',
  upstream_unavailable: 'product.tier.upstream_unavailable',
}

// turnErrorKey returns the i18n key for a code, or null when nothing is registered.
export function turnErrorKey(code: string): string | null {
  return TURN_ERROR_KEYS[code] ?? null
}

// turnErrorView turns a turn_error payload into what the user should read.
//
// It takes the raw payload as `unknown` on purpose: the fields arrive from JSON, so
// a missing or wrongly-typed one is a real possibility, and a thrown exception here
// would blank the transcript instead of showing the failure.
export function turnErrorView(ev: unknown, fallback: string): { text: string; code: string } {
  const e = (ev ?? {}) as Record<string, unknown>
  const code = typeof e.code === 'string' ? e.code : ''
  const key = code ? turnErrorKey(code) : null
  if (key) return { text: tr(key), code }
  const msg = typeof e.error === 'string' && e.error.length > 0 ? e.error : fallback
  return { text: msg, code }
}
