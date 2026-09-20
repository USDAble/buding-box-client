/** Format the platform's millionth-of-a-point integer without rounding the balance. */
export function formatPoints(credits?: { balance: number; known?: boolean } | null): string {
  if (credits?.known !== true || !Number.isSafeInteger(credits.balance) || credits.balance < 0) return '—'
  const amount = BigInt(credits.balance)
  const whole = amount / 1_000_000n
  const fraction = (amount % 1_000_000n).toString().padStart(6, '0').replace(/0+$/, '')
  return fraction ? `${whole}.${fraction}` : whole.toString()
}
