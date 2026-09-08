import { describe, expect, it } from 'vitest'
import { diffCalendarDays, licenseDaysLeft, licenseStatus } from './license'

describe('license calendar days', () => {
  it('ignores time of day and treats today as expired with zero days', () => {
    const now = new Date(2026, 8, 8, 23, 59)
    expect(diffCalendarDays(new Date(2026, 8, 8, 0, 1), now)).toBe(0)
    expect(licenseDaysLeft(new Date(2026, 8, 8), now)).toBe(0)
    expect(licenseStatus(new Date(2026, 8, 8), now)).toBe('expired')
  })

  it('handles month boundaries and past dates', () => {
    const now = new Date(2026, 0, 31, 12)
    expect(diffCalendarDays(new Date(2026, 1, 2), now)).toBe(2)
    expect(licenseStatus(new Date(2025, 11, 31), now)).toBe('expired')
  })

  it('uses local dates rather than elapsed hours across DST', () => {
    const now = new Date(2026, 2, 7, 12)
    expect(diffCalendarDays(new Date(2026, 2, 9, 12), now)).toBe(2)
  })
})
