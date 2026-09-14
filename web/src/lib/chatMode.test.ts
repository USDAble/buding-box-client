import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import { isPrivacyMode, modelDisplayName, setSessionMode } from './chatMode'
import { chatMode, chatModel, sessions } from './stores'
import { en, zh, setLocale } from './i18n'
import type { ChatModeModel, Session } from './api'

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
    //
    // The `model.` prefix is therefore RESERVED, and PR-5e is the near-miss that
    // proves the guard earns its keep: its L-C7 sentence was first spelled
    // `model.withdrawn`, which is a *sentence*, not a name table — and this test
    // failed. The fix was to rename the key (`session.model_withdrawn`), never to
    // allowlist it: a guard that grows exceptions for keys that mean something
    // else stops being able to tell a name table from a sentence, and a name
    // table is the failure that quietly keeps rendering a platform's stale rename.
    const table = [...Object.keys(en), ...Object.keys(zh)].filter((k) => k.startsWith('model.'))
    expect(table).toEqual([])
  })
})

describe('setSessionMode', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
    chatMode.set({})
    chatModel.set({})
    sessions.set([])
  })

  it('saves the mode before the model, and updates both stores', async () => {
    // V-46. The mode request is the FIRST of the two a switch makes, and it used
    // to go to `/api/sessions/{id}/chat-mode` — a path no server has ever
    // registered (only the DEV fake backend answered it). On a real build that
    // 404 rejected before the model request or either local store update ran, so
    // the visible result was "the switch did nothing, plus a 404". Both the path
    // and the order are therefore part of the contract this pins.
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => ({
      ok: true,
      status: 200,
      statusText: '',
      json: async () => ({
        ok: true,
        chat_mode: 'privacy',
        model: 'buding-gateway::buding-cloud-pro',
        model_id: 'buding-gateway::buding-cloud-pro',
      }),
    }))
    vi.stubGlobal('fetch', fetchMock)
    sessions.set([{ id: 's1' } as unknown as Session])

    await setSessionMode('s1', 'privacy', 'buding-gateway::buding-cloud-pro')

    expect(
      fetchMock.mock.calls.map((c) => [String(c[0]), (c[1] as RequestInit)?.method]),
    ).toEqual([
      ['/api/sessions/s1/chat_mode', 'PATCH'],
      ['/api/sessions/s1/model', 'PATCH'],
    ])
    expect(get(chatMode)['s1']).toBe('privacy')
    expect(get(chatModel)['s1']).toBe('buding-gateway::buding-cloud-pro')
  })
})
