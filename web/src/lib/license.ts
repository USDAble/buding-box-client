export type LicenseStatus = 'active' | 'expired'

function localDate(value: Date | string): Date {
  const date = value instanceof Date ? new Date(value.getTime()) : new Date(value)
  if (Number.isNaN(date.getTime())) throw new Error('invalid license date')
  return new Date(date.getFullYear(), date.getMonth(), date.getDate())
}

/** Returns the difference in local calendar days, independent of time of day. */
export function diffCalendarDays(expiresAt: Date | string, now: Date = new Date()): number {
  const end = localDate(expiresAt)
  const start = localDate(now)
  return Math.round((end.getTime() - start.getTime()) / 86400000)
}

export function licenseDaysLeft(expiresAt: Date | string, now: Date = new Date()): number {
  return Math.max(0, diffCalendarDays(expiresAt, now))
}

export function licenseStatus(expiresAt: Date | string, now: Date = new Date()): LicenseStatus {
  return diffCalendarDays(expiresAt, now) > 0 ? 'active' : 'expired'
}
