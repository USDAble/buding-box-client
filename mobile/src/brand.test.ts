import { describe, it, expect } from 'vitest'
import { brandName } from './brand'

describe('brandName', () => {
  it('resolves an exact locale tag', () => {
    expect(brandName('zh-CN')).toBe('布丁盒子')
    expect(brandName('en-US')).toBe('Pudding Box')
  })

  it('falls back to the bare language prefix', () => {
    expect(brandName('en-GB')).toBe('Pudding Box')
  })

  it('falls back to en-US for unknown locales', () => {
    expect(brandName('fr-FR')).toBe('Pudding Box')
  })
})
