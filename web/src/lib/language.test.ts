import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import { locale, setLocale, t, zh, zhHant } from './i18n'
import { productState, type ProductStateDTO } from './product'
import { languageSaving, saveLanguagePreference } from './language'
import { brandTagline } from './brand'

beforeEach(() => {
  productState.set({ prefs: { locale: 'en' } } as ProductStateDTO)
  setLocale('en')
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{"ok":true}')))
})
afterEach(() => {
  productState.set(null)
  setLocale('en')
  vi.unstubAllGlobals()
})

it('ships a complete Traditional table with preserved substitutions and translated branding', () => {
  expect(Object.keys(zhHant).sort()).toEqual(Object.keys(zh).sort())
  for (const [key, value] of Object.entries(zh)) {
    expect(zhHant[key].trim()).not.toBe('')
    expect(zhHant[key].match(/\{[^}]+\}/g)).toEqual(value.match(/\{[^}]+\}/g))
  }
  setLocale('zh-Hant')
  expect(get(t)('settings.language')).toBe('語言')
  expect(get(t)('settings.language_failed')).toBe('更新語言失敗')
  expect(brandTagline('zh-Hant')).toContain('協作')
  expect(document.documentElement.lang).toBe('zh-Hant')
})

it('persists Traditional Chinese without coercing it to English and restores it from product state', async () => {
  await saveLanguagePreference('zh-Hant')
  expect(get(locale)).toBe('zh-Hant')
  expect(get(productState)?.prefs.locale).toBe('zh-Hant')
  expect(vi.mocked(fetch).mock.calls.map(([url, init]) => [url, JSON.parse(String(init?.body))])).toEqual([
    ['/api/product/locale', { locale: 'zh-Hant' }],
    ['/api/config/language', { language: 'zh-Hant' }],
  ])
  productState.set(null)
  setLocale('en')
  productState.set({ prefs: { locale: 'zh-Hant' } } as ProductStateDTO)
  expect(get(locale)).toBe('zh-Hant')
})

it('restores the previous choice on a refused product save and does not write its mirror', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response('{}', { status: 500 }))
  await expect(saveLanguagePreference('zh-Hant')).rejects.toThrow()
  expect(get(locale)).toBe('en')
  expect(get(productState)?.prefs.locale).toBe('en')
  expect(fetch).toHaveBeenCalledTimes(1)
  expect(get(languageSaving)).toBe(false)
})

it('keeps the persisted product choice if only the native mirror fails', async () => {
  vi.mocked(fetch).mockResolvedValueOnce(new Response('{"ok":true}'))
    .mockResolvedValueOnce(new Response('{}', { status: 500 }))
  await expect(saveLanguagePreference('zh-Hant')).rejects.toThrow()
  expect(get(locale)).toBe('zh-Hant')
  expect(get(productState)?.prefs.locale).toBe('zh-Hant')
  expect(get(languageSaving)).toBe(false)
})

it('blocks overlapping saves until the current selection finishes', async () => {
  let resolve!: (response: Response) => void
  vi.mocked(fetch).mockImplementationOnce(() => new Promise<Response>(done => { resolve = done }))
  const first = saveLanguagePreference('zh-Hant')
  expect(get(languageSaving)).toBe(true)
  await saveLanguagePreference('zh')
  expect(fetch).toHaveBeenCalledTimes(1)
  resolve(new Response('{"ok":true}'))
  await first
  expect(get(locale)).toBe('zh-Hant')
  expect(get(languageSaving)).toBe(false)
})

it('saves the config language in a plain web session without product state', async () => {
  productState.set(null)
  await saveLanguagePreference('zh-Hant')
  expect(fetch).toHaveBeenCalledExactlyOnceWith('/api/config/language', expect.objectContaining({ body: '{"language":"zh-Hant"}' }))
})
