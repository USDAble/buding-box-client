// sensitive.ts — client half of the sensitive-word input gate (P8).
//
// The composer calls checkSensitive before sending so it can substitute the
// input box and show the notice without a round-trip through the chat path.
// The server re-checks on the chat path anyway (frontends can be bypassed), so
// this is purely for responsiveness + input-box substitution — not the source
// of truth. See P8-敏感词接入.md §3.5.
import { request } from "./api";

export interface SensitiveCheck {
  hit: boolean;
  masked: string;
}

/**
 * Asks the server whether text hits a sensitive word, returning the masked
 * form when it does. Resolves with hit=false outside the desktop shell (no
 * window token), matching the server's gate-exempt behaviour.
 *
 * Goes through request() (the same funnel as sensitiveDict.ts) rather than a
 * raw fetch: a 401 must return the window to the login page (noteSessionLost)
 * exactly like every other product call, not be swallowed by the composer's
 * catch. The composer's "failure doesn't block sending" still holds — the
 * server re-checks authoritatively on the chat path — but a revoked session is
 * no longer silently ignored. PR-6c §4.1 第 38 行.
 */
export async function checkSensitive(text: string): Promise<SensitiveCheck> {
  const body = await request<{ hit?: boolean; masked?: string }>("/api/product/sensitive/check", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text }),
  });
  return { hit: !!body.hit, masked: body.masked ?? "" };
}

/** The `input_sensitive` payload, as the server sends it on the socket. */
export interface SensitiveRejectionEvent {
  session_id?: string;
  text?: string;
}

/**
 * What applying a rejection to the UI needs. Both effects are injected rather
 * than imported: the callers are a component method and a toast, and reaching
 * for either from here would tie this module to one surface. The sentence stays
 * with the caller too — i18n has exactly one owner (开发规范 §3.8).
 */
export interface SensitiveRejectionTargets {
  /** The session the event must be for; events for other sessions are ignored. */
  sessionID: string;
  /** Put the masked text back where the user can edit it. */
  restore: (maskedText: string) => void;
  /** Say why the message did not send. */
  notify: () => void;
}

/**
 * Applies the server's input-gate rejection to the UI.
 *
 * This is the other direction of the same gate as checkSensitive, and it fires
 * for the case that function cannot cover: the composer asks the server before
 * sending for responsiveness, but the server re-checks authoritatively on the
 * chat path, so a rejection reaches the UI here — on the socket — when the
 * pre-check never ran or failed open. Nothing else would put the masked text
 * back, which is why the two effects live in one place instead of two.
 *
 * Returns whether it applied, because "addressed to another session" is a real
 * outcome: one window subscribes to one session, but the event carries the
 * session id, and the caller needs the difference to stay testable.
 */
export function applySensitiveRejection(
  ev: SensitiveRejectionEvent,
  targets: SensitiveRejectionTargets,
): boolean {
  if (ev.session_id && ev.session_id !== targets.sessionID) return false;
  // An absent text still restores an empty box: the box must not be left holding
  // whatever the user typed, or they would resend the very text that was refused.
  targets.restore(ev.text ?? "");
  targets.notify();
  return true;
}
