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
