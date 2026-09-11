// sensitive.ts — client half of the sensitive-word input gate (P8).
//
// The composer calls checkSensitive before sending so it can substitute the
// input box and show the notice without a round-trip through the chat path.
// The server re-checks on the chat path anyway (frontends can be bypassed), so
// this is purely for responsiveness + input-box substitution — not the source
// of truth. See P8-敏感词接入.md §3.5.
import { WINDOW_TOKEN_HEADER, windowToken } from "./product";

export interface SensitiveCheck {
  hit: boolean;
  masked: string;
}

/**
 * Asks the server whether text hits a sensitive word, returning the masked
 * form when it does. Resolves with hit=false outside the desktop shell (no
 * window token), matching the server's gate-exempt behaviour.
 */
export async function checkSensitive(text: string): Promise<SensitiveCheck> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  const token = windowToken();
  if (token) headers[WINDOW_TOKEN_HEADER] = token;
  const res = await fetch("/api/product/sensitive/check", {
    method: "POST",
    headers,
    body: JSON.stringify({ text }),
  });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  const body = (await res.json()) as { hit?: boolean; masked?: string };
  return { hit: !!body.hit, masked: body.masked ?? "" };
}
