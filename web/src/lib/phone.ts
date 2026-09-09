// Phone normalization for the login form — the TypeScript mirror of the Go
// side (internal/productstate/phone.go). Both run the same rules against the
// same fixtures so a number the server accepts is exactly what the client
// validated; the client check is for responsiveness, the server's is the rule.
//
// Rules (需求 §5.3.2):
//   1. strip all whitespace (full-width included) and hyphens;
//   2. if the result starts with "+86" or "86" AND the remainder is exactly 11
//      digits, drop that prefix;
//   3. the result must be 11 digits starting with "1".
export function normalizePhone(raw: string): { ok: boolean; value: string } {
  let v = raw.replace(/\s|-/g, '')

  for (const prefix of ['+86', '86']) {
    if (v.startsWith(prefix)) {
      const rest = v.slice(prefix.length)
      if (rest.length === 11 && /^\d{11}$/.test(rest)) {
        v = rest
        break
      }
    }
  }

  if (v.length === 11 && v[0] === '1' && /^\d{11}$/.test(v)) {
    return { ok: true, value: v }
  }
  return { ok: false, value: '' }
}
