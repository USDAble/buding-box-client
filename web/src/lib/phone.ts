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
