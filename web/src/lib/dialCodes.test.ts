import { describe, expect, it } from 'vitest'
import { dialCodeOptions } from './dialCodes'

describe('dialCodeOptions', () => {
  it('provides a searchable global dialing-code picker with mainland first', () => {
    const options = dialCodeOptions('zh')
    expect(options.length).toBeGreaterThan(200)
    expect(options[0]).toMatchObject({ region: 'CN', code: '+86' })
    expect(dialCodeOptions('zh', '+852').some((option) => option.region === 'HK')).toBe(true)
    expect(dialCodeOptions('en', 'Singapore').some((option) => option.code === '+65')).toBe(true)
  })
})
