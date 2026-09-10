import { describe, expect, it } from 'vitest'

import {
  BRAND_SCHEMA_VERSION,
  IDENTIFIER,
  brand,
  brandAsset,
  brandColor,
  brandCopyright,
  brandCopyrightFor,
  brandDisplay,
  brandIdentifier,
  brandLink,
  brandName,
  brandShortName,
  brandTeamName,
  brandText,
  localize,
} from './brand'

describe('brand configuration', () => {
  it('embeds the schema version this module understands', () => {
    expect(brand.schemaVersion).toBe(BRAND_SCHEMA_VERSION)
  })

  it('resolves the full and short names per locale', () => {
    expect(brandName('zh-CN')).toBe('布丁盒子')
    expect(brandName('en-US')).toBe('Pudding Box')
    expect(brandShortName('zh-CN')).toBe('布丁盒子')
    expect(brandShortName('en-US')).toBe('Pudding')
  })

  // The regression this module exists to prevent: the UI locale store holds
  // bare 'zh'/'en', the configuration is keyed 'zh-CN'/'en-US'. Indexing the
  // map directly renders English text inside the Chinese UI.
  it('resolves the bare locale the UI store actually holds', () => {
    expect(brandName('zh')).toBe('布丁盒子')
    expect(brandName('en')).toBe('Pudding Box')
    expect(brandTeamName('zh')).toBe('布丁盒子工作室')
    expect(brandTeamName('en')).toBe('Pudding Box Studio')
  })

  it('never returns an empty string for an unknown locale', () => {
    for (const locale of ['', 'de-DE', 'ja', 'zh-Hant']) {
      expect(brandName(locale)).not.toBe('')
      expect(brandTeamName(locale)).not.toBe('')
    }
  })
})

describe('localize', () => {
  it('prefers an exact tag', () => {
    expect(localize({ 'zh-CN': '精确', 'en-US': 'exact' }, 'zh-CN')).toBe('精确')
  })

  it('falls back through the language subtag', () => {
    expect(localize({ 'zh-CN': '简体', 'en-US': 'en' }, 'zh-Hant')).toBe('简体')
  })

  it('falls back to English, then to any value', () => {
    expect(localize({ 'en-US': 'en' }, 'ja-JP')).toBe('en')
    expect(localize({ 'ja-JP': '日本語' }, 'ko-KR')).toBe('日本語')
  })

  it('skips empty values instead of returning a blank string', () => {
    expect(localize({ 'zh-CN': '', 'en-US': 'en' }, 'zh-CN')).toBe('en')
  })

  it('tolerates a missing map', () => {
    expect(localize(undefined, 'en-US')).toBe('')
    expect(localize({}, 'en-US')).toBe('')
  })
})

describe('copyright', () => {
  it('keeps the placeholder in the raw value', () => {
    expect(brandCopyright('zh-CN')).toContain('{year}')
  })

  it('substitutes an explicit year', () => {
    expect(brandCopyrightFor('zh-CN', 2026)).toBe('© 2026 布丁盒子工作室')
    expect(brandCopyrightFor('en-US', 2026)).toBe('© 2026 Pudding Box Studio')
  })

  it('defaults to the current year and leaves no placeholder behind', () => {
    const rendered = brandCopyrightFor('zh')
    expect(rendered).not.toContain('{year}')
    expect(rendered).toContain(String(new Date().getFullYear()))
  })
})

describe('class B fixed display values', () => {
  it('are single values, not locale maps', () => {
    expect(brandDisplay('windows', 'productName')).toBe('Pudding Box')
    expect(brandDisplay('windows', 'fileDescription')).toBe('布丁盒子')
  })

  it('return empty rather than throwing on an unknown group', () => {
    expect(brandDisplay('nope', 'productName')).toBe('')
  })
})

describe('class C identifiers and paths', () => {
  it('resolves every declared key', () => {
    for (const key of Object.values(IDENTIFIER)) {
      expect(brandIdentifier(key), `identifier ${key}`).not.toBe('')
    }
  })

  // Class C is machine-read: a Chinese character or a space in a path, a lock
  // name or a protocol scheme is a functional bug, not a cosmetic one.
  it('are printable ASCII without spaces', () => {
    const groups: Record<string, Record<string, string>> = {
      'identifiers.current': brand.identifiers.current,
      'identifiers.future': brand.identifiers.future,
      'visual.logo': brand.visual.logo,
      // Absent until the brand colour is agreed; an empty group contributes
      // nothing rather than throwing.
      'visual.colors': brand.visual.colors ?? {},
    }
    for (const [group, links] of Object.entries(brand.links)) groups[`links.${group}`] = links

    for (const [group, values] of Object.entries(groups)) {
      for (const [key, value] of Object.entries(values)) {
        expect(value, `${group}.${key}`).toMatch(/^[\x21-\x7E]+$/)
      }
    }
  })

  it('keeps the single-instance lock off the upstream identifier', () => {
    expect(brandIdentifier(IDENTIFIER.singleInstanceId)).toBe('app.puddingbox.desktop')
  })

  // These stay on the upstream values this phase; changing them breaks
  // in-place upgrades for already-installed copies.
  it('keeps the compatibility identifiers unchanged', () => {
    expect(brandIdentifier(IDENTIFIER.cliCommand)).toBe('octo')
    expect(brandIdentifier(IDENTIFIER.configDir)).toBe('~/.octo')
    expect(brandIdentifier(IDENTIFIER.envPrefix)).toBe('OCTO_')
  })

  it('exposes usable links, assets and colours', () => {
    expect(brandLink('external', 'license')).toMatch(/^https:\/\//)
    expect(brandLink('external', 'license')).not.toMatch(/[<>]/)
    expect(brandLink('inApp', 'terms')).toMatch(/^\//)
    expect(brandLink('nope', 'license')).toBe('')
    expect(brandAsset('mark')).not.toBe('')
    // The primary brand colour is decided (sampled from logo-mark.png,
    // 2026-09-09); an unknown colour group must still degrade to an empty
    // string rather than throw on the missing key.
    expect(brandColor('primary')).toBe('#437EB1')
    expect(brandColor('nope')).toBe('')
  })
})

describe('shared copy', () => {
  it('resolves both locales', () => {
    expect(brandText('termsBody', 'zh-CN')).not.toBe('')
    expect(brandText('termsBody', 'en-US')).not.toBe('')
  })

  it('returns empty for an unknown key', () => {
    expect(brandText('no-such-key', 'en-US')).toBe('')
  })
})
