// Phone normalization for the login form — the TypeScript mirror of the Go
// side (internal/productstate/phone.go). Both run the same rules against the
// same fixtures so a number the server accepts is exactly what the client
// validated; the client check is for responsiveness, the server's is the rule.
//
// International numbers use E.164. A mainland 11-digit number remains a
// compatibility shorthand and is canonicalized to +86 before it is sent.
export function normalizePhone(raw: string): { ok: boolean; value: string } {
  let v = raw.trim().replace(/[\s().-]/g, '').replace(/＋/g, '+').replace(/[０-９]/g, (d) => String(d.charCodeAt(0) - '０'.charCodeAt(0)))
  if (/^1[3-9]\d{9}$/.test(v)) v = `+86${v}`
  else if (/^86(?:1[3-9]\d{9})$/.test(v)) v = `+${v}`
  if (/^\+[1-9]\d{7,14}$/.test(v)) return { ok: true, value: v }
  return { ok: false, value: '' }
}

// OCTO-FORK: the shared login/activation form adds its selected calling code
// without breaking users who paste a complete international number.
export function normalizePhoneWithCallingCode(raw: string, callingCode: string): { ok: boolean; value: string } {
  const full = normalizePhone(raw)
  if (/^[+＋]/.test(raw.trim()) || (callingCode === '+86' && full.ok)) return full
  return normalizePhone(`${callingCode}${raw}`)
}

// OCTO-FORK: local auth accepts a separate region_code, while the control
// plane still uses E.164. Unknown pasted codes keep the legacy E.164 route.
export function splitPhoneForLocalAPI(e164: string, callingCodes: readonly string[]): { phone: string; region_code?: string } {
  let match = ''
  for (const code of callingCodes) {
    if (e164.startsWith(code) && code.length > match.length) match = code
  }
  return match ? { phone: e164.slice(match.length), region_code: match.slice(1) } : { phone: e164 }
}
