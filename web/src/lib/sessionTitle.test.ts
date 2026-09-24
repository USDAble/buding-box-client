import { describe, expect, it } from 'vitest'

import { sessionDisplayTitle } from './sessionTitle'

describe('sessionDisplayTitle', () => {
  it('replaces the upstream placeholder with configured product copy', () => {
    const session = { name: '*Octo Agent', title: '*Octo Agent' }
    expect(sessionDisplayTitle(session, 'zh')).toBe('Puddingbox agent')
    expect(sessionDisplayTitle(session, 'en')).toBe('Puddingbox agent')
  })

  it('prefers the server display name over a raw placeholder title', () => {
    expect(sessionDisplayTitle({ name: '第一条消息', title: '*Octo Agent' }, 'zh')).toBe('第一条消息')
  })

  it('keeps generated and user-edited titles', () => {
    expect(sessionDisplayTitle({ name: '', title: 'Release plan' }, 'en')).toBe('Release plan')
  })

  it('uses the caller fallback while a session is unavailable', () => {
    expect(sessionDisplayTitle(undefined, 'en', 'Chat')).toBe('Chat')
  })
})
