import { describe, expect, it } from 'vitest'
import { isPrivacyMode } from './chatMode'

describe('isPrivacyMode', () => {
  it('only enables privacy affordances for the privacy mode', () => {
    expect(isPrivacyMode('privacy')).toBe(true)
    expect(isPrivacyMode('smart')).toBe(false)
    expect(isPrivacyMode('default')).toBe(false)
    expect(isPrivacyMode('')).toBe(false)
    expect(isPrivacyMode(undefined)).toBe(false)
  })
})
