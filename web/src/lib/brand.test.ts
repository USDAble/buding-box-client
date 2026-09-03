import { describe, expect, it } from 'vitest'
import { brand, brandName, brandUrl, brandText } from './brand'

describe('brand configuration', () => {
  it('uses the Pudding Box names from configuration', () => {
    expect(brandName('zh-CN')).toBe('布丁盒子')
    expect(brandName('en-US')).toBe('Pudding Box')
    expect(brandName('fr-FR')).toBe('Pudding Box')
  })

  it('falls back to English for unsupported locales', () => {
    expect(brandName('de-DE')).toBe('Pudding Box')
  })

  it('resolves configured URLs without hardcoding them in callers', () => {
    expect(brandUrl('website')).toBe('https://puddingbox.example')
    expect(brandUrl('docs')).toBe('https://docs.puddingbox.example')
  })

  it('exposes localized text and stable compatibility identifiers', () => {
    expect(brandText('tagline', 'zh-CN')).toContain('智能代理')
    expect(brandText('tagline', 'en-US')).toContain('agent workflows')
    expect(brand.compatibility.cliCommand).toBe('octo')
    expect(brand.compatibility.configDir).toBe('~/.octo')
  })
})
