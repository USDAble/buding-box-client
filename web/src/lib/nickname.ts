// Nickname generation and validation for the login form — the TypeScript
// mirror of the Go side (internal/productstate/nickname.go), sharing the same
// fixtures. The client check is for responsiveness; the server re-runs the rule.

/** A fresh default nickname in the current UI language (需求 §5.3.3). */
export function randomNickname(locale: 'zh' | 'en'): string {
  const n = Math.floor(Math.random() * 10000).toString().padStart(4, '0')
  return (locale === 'zh' ? '用户' : 'User') + n
}

// nicknameChars matches the allowed set: Han ideographs, letters, digits and
// underscore. Unicode properties rather than [一-龥] so extended Han ranges
// (and Latin/Greek/etc.) aren't wrongly rejected or accepted. The explicit
// Script=Han spelling (not the \p{Han} shorthand) so it also compiles on
// small-ICU/older V8 builds used by some test runners.
const NICKNAME_CHARS = /^[\p{Script=Han}\p{L}\p{Nd}_]+$/u;

/**
 * Validates a nickname: 2–16 characters (counted as code points, not bytes),
 * drawn from Han / letters / digits / underscore. Returns 'ok' or 'format' —
 * the sensitive-word check is server-side (P8), not a shape rule here.
 */
export function validateNickname(v: string): 'ok' | 'format' {
  const n = [...v].length
  if (n < 2 || n > 16) return 'format'
  if (!NICKNAME_CHARS.test(v)) return 'format'
  return 'ok'
}
