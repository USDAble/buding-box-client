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

// code → i18n key. The code values are the control plane's (中台交付包 §3.2, the
// single registry): the gateway's own codes and the client-side fail-closed ones.
//
// `session.model_withdrawn` is REUSED rather than duplicated: PR-5e's picker already
// shows that sentence for this exact situation (B8 rule 2), and a second key with the
// same words is the copy-drifting twin §3.8 forbids.
export const TURN_ERROR_KEYS: Record<string, string> = {
  insufficient_credits: 'turn_error.insufficient_credits',
  model_withdrawn: 'session.model_withdrawn',
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
