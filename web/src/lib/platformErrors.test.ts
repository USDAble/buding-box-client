import { afterEach, describe, expect, it, vi } from 'vitest'
import { en, zh, platformErrorKey, platformErrorText, setLocale } from './i18n'
import { readErrorMessage, request, RequestError } from './api'

// OCTO-FORK: the client translates platform machine codes without displaying
// a server-authored Chinese message when its UI language is English.
afterEach(() => {
  vi.unstubAllGlobals()
  setLocale('en')
})

describe('platform response code translations', () => {
  it('has matching bilingual coverage for the published client error codes', () => {
    const keys = Object.keys(en).filter(key => key.startsWith('platform.error.'))
    expect(keys).toHaveLength(52) // 51 contract codes and one generic fallback.
    expect(Object.keys(zh).filter(key => key.startsWith('platform.error.')).sort()).toEqual(keys.sort())
    expect(keys.filter(key => /[\u3400-\u9fff]/u.test(en[key]))).toEqual([])
  })

  it('looks up codes case-sensitively and falls back in the selected language', () => {
    setLocale('en')
    expect(platformErrorText('code_expired')).toContain('expired')
    expect(platformErrorKey('RATE_LIMITED')).toBe('platform.error.RATE_LIMITED')
    expect(platformErrorKey('rate_limited')).toBe('platform.error.rate_limited')
    expect(platformErrorText('some_future_code')).toBe(en['platform.error.generic'])
    setLocale('zh')
    expect(platformErrorText('code_expired')).toContain('验证码')
    expect(platformErrorText('some_future_code')).toBe(zh['platform.error.generic'])
  })

  it('localizes generic API errors by code and keeps the server copy for diagnostics', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: 'BILLING_UNAVAILABLE', message: '积分服务暂不可用',
    }), { status: 503, headers: { 'Content-Type': 'application/json' } })))
    const error = await request<never>('/api/test').catch((cause: unknown) => cause as RequestError)
    expect(error).toBeInstanceOf(RequestError)
    expect(error.code).toBe('BILLING_UNAVAILABLE')
    expect(error.message).toBe(en['platform.error.BILLING_UNAVAILABLE'])
    expect(error.serverMessage).toBe('积分服务暂不可用')
  })

  it('uses a localized fallback for unknown codes, including raw-response callers', async () => {
    const payload = { code: 'NEW_CODE', message: '服务器中文错误' }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(payload), { status: 400 })))
    const error = await request<never>('/api/test').catch((cause: unknown) => cause as RequestError)
    expect(error.message).toBe(en['platform.error.generic'])
    expect(error.code).toBe('NEW_CODE')
    expect(error.serverMessage).toBe(payload.message)
    expect(await readErrorMessage(new Response(JSON.stringify(payload), { status: 400 }), 'fallback'))
      .toBe(en['platform.error.generic'])
  })
})
