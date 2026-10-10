// Phone normalization for the login form — the TypeScript mirror of the Go
// side (internal/productstate/phone.go). Both run the same rules against the
// same fixtures so a number the server accepts is exactly what the client
// validated; the client check is for responsiveness, the server's is the rule.
//
// International numbers use E.164. A mainland 11-digit number remains a
// compatibility shorthand and is canonicalized to +86 before it is sent.
export function normalizePhone(raw: string): { ok: boolean; value: string } {
  let v = compactPhone(raw)
  if (/^1[3-9]\d{9}$/.test(v)) v = `+86${v}`
  else if (/^86(?:1[3-9]\d{9})$/.test(v)) v = `+${v}`
  if (/^\+[1-9]\d{7,14}$/.test(v)) return { ok: true, value: v }
  return { ok: false, value: '' }
}

// OCTO-FORK: split input must retain the canonical normalizer's accepted spellings.
function compactPhone(raw: string): string {
  return raw.trim().replace(/[\s().-]/g, '').replace(/＋/g, '+').replace(/[０-９]/g, (d) => String(d.charCodeAt(0) - '０'.charCodeAt(0)))
}

// OCTO-FORK: the country selector and local-number input must submit one E.164
// phone value to the existing product API without changing its wire contract.
export function normalizePhoneParts(dialCode: string, localNumber: string): { ok: boolean; value: string } {
  const dial = dialCode.trim()
  let local = compactPhone(localNumber)
  if (!/^\+[1-9]\d{0,2}$/.test(dial)) return { ok: false, value: '' }
  if (local.startsWith('+')) {
    if (!local.startsWith(dial)) return { ok: false, value: '' }
    local = local.slice(dial.length)
  } else if (dial === '+86' && /^861[3-9]\d{9}$/.test(local)) {
    local = local.slice(2)
  }
  if (!/^\d+$/.test(local)) return { ok: false, value: '' }
  if (dial === '+86' && !/^1[3-9]\d{9}$/.test(local)) return { ok: false, value: '' }
  return normalizePhone(dial + local)
}
