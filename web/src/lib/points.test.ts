import { expect, it } from 'vitest'
import { formatPoints } from './points'

it('distinguishes an unread balance from a verified zero', () => {
  expect(formatPoints({ balance: 0 })).toBe('—')
  expect(formatPoints({ balance: 0, known: true })).toBe('0')
})
it('shows points rather than raw micro-credit units', () => {
  expect(formatPoints({ balance: 12345600, known: true })).toBe('12.3456')
  expect(formatPoints({ balance: 1, known: true })).toBe('0.000001')
  expect(formatPoints({ balance: Number.MAX_SAFE_INTEGER, known: true })).toBe('9007199254.740991')
})
