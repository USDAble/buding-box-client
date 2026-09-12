import { afterEach, describe, expect, it } from 'vitest'
import { isPrivacyMode, modelDisplayName } from './chatMode'
import { en, zh, setLocale } from './i18n'
import type { ChatModeModel } from './api'

// Fixtures are built through this helper rather than inline so a row always has
// the shape the projection produces: a catalog name in both languages and a
// composite id.
function model(id: string, zhName: string, enName: string): ChatModeModel {
  return { id, displayName: { zh: zhName, en: enName }, compositeId: `buding-gateway::${id}` }
}

afterEach(() => setLocale('en'))

describe('isPrivacyMode', () => {
  it('only enables privacy affordances for the privacy mode', () => {
    expect(isPrivacyMode('privacy')).toBe(true)
    expect(isPrivacyMode('smart')).toBe(false)
    expect(isPrivacyMode('default')).toBe(false)
    expect(isPrivacyMode('')).toBe(false)
    expect(isPrivacyMode(undefined)).toBe(false)
  })
})

describe('modelDisplayName', () => {
  it('renders the name in the interface language', () => {
    const m = model('buding-cloud-pro', '云端旗舰', 'Cloud Pro')

    setLocale('zh')
    expect(modelDisplayName(m)).toBe('云端旗舰')

    setLocale('en')
    expect(modelDisplayName(m)).toBe('Cloud Pro')
  })

  it('treats every zh-* interface language as Chinese, the way dictFor does', () => {
    // i18n serves zh-TW from the Simplified dictionary (i18n.ts dictFor). If the
    // model name did not follow the same rule, a zh-TW interface would render
    // English model names beside Chinese copy — the visible half of a mismatch
    // whose other half lives in i18n.ts.
    const m = model('x', '中文名', 'English name')

    setLocale('zh-TW')
    expect(modelDisplayName(m)).toBe('中文名')
  })

  it('falls back zh → en, and only then to the raw id', () => {
    setLocale('zh')
    expect(modelDisplayName(model('buding-x', '', 'English name'))).toBe('English name')
    expect(modelDisplayName(model('buding-x', '', ''))).toBe('buding-x')
  })

  it('renders a model this client has never heard of', () => {
    // The whole point of showing the catalog's string: a model added next month
    // renders correctly on a client built today, with no release between them.
    setLocale('zh')
    const unknown = model('buding-cloud-ultra-2027', '云端至尊', 'Cloud Ultra')

    expect(modelDisplayName(unknown)).toBe('云端至尊')
  })

  it('keeps no id→name table behind the fallback', () => {
    // 需求基线 B6 规则 2. The tiers stop at the id on purpose, and this is the
    // assertion that keeps a fourth tier from growing back: PR-4d deleted the
    // eight `model.*` keys that used to be it. The reason it must stay deleted
    // is not tidiness — a local table shadows the server's copy, so a platform
    // rename silently keeps rendering the old name, and the stale value looks
    // authoritative in a way a bare id does not.
    //
    // Asserted over the dictionaries rather than over modelDisplayName, because
    // the failure it prevents is a *re-added* key plus a reverted lookup; only
    // one half of that is visible from the function.
    const table = [...Object.keys(en), ...Object.keys(zh)].filter((k) => k.startsWith('model.'))
    expect(table).toEqual([])
  })
})
