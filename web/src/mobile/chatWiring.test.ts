import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

// The mobile shell's hop from the WS payload to the sentence on screen.
//
// WHY THIS EXISTS (V-66's shape). turnError.test.ts nails turnErrorView itself with
// literal event bodies. It cannot see whether the mobile handler USES it: a build that
// kept the mapping module and kept rendering `ev.error ?? '…'` in the handler would
// leave every one of those nails green while the phone still shows the gateway's JSON.
// That is exactly the gap V-66 registered for `complete` → ChatView, so this nail
// covers the same hop for turn_error — and mobile is the shell where it is cheap, since
// the handler is a plain module over the ws singleton rather than a component.
//
// The ws module is faked with a real registry (handlers recorded, then invoked) so the
// handler runs the way the socket would call it. Nothing else is stubbed beyond the
// chat API, which wireMobileSession does not touch.

const handlers = new Map<string, (ev: any) => void>()

vi.mock('../lib/ws', () => ({
  ws: {
    on: vi.fn((type: string, fn: (ev: any) => void) => {
      handlers.set(type, fn)
      return () => handlers.delete(type)
    }),
    send: vi.fn(),
  },
}))

vi.mock('../lib/api', () => ({
  getSessionGoal: vi.fn(async () => ({ goal: null })),
  getSessionMessages: vi.fn(async () => ({ events: [] })),
}))

import { chatMessages } from '../lib/stores'
import { locale, tr } from '../lib/i18n'
import { wireMobileSession } from './chatWiring'

function notices(sid: string): string[] {
  return (get(chatMessages)[sid] ?? []).filter(m => m.type === 'notice').map(m => m.content)
}

beforeEach(() => {
  locale.set('zh')
  handlers.clear()
  chatMessages.set({})
})

describe('the mobile turn_error handler reads the code', () => {
  it('shows one Chinese sentence for a 402, not the gateway body', () => {
    wireMobileSession('s1')

    handlers.get('turn_error')!({
      type: 'turn_error',
      session_id: 's1',
      code: 'insufficient_credits',
      error: 'openai: HTTP 402: {"code":"insufficient_credits","message":"balance too low"}',
    })

    const shown = notices('s1')
    expect(shown.length).toBe(1)
    expect(shown[0]).toContain(tr('turn_error.insufficient_credits'))
    expect(shown[0]).not.toMatch(/HTTP|402|\{/)
  })

  it('keeps the server sentence when the code is unknown', () => {
    // The counter-nail: a handler that dropped `ev.error` entirely and always printed
    // the fallback would pass the test above while hiding every unregistered failure.
    wireMobileSession('s2')

    handlers.get('turn_error')!({ type: 'turn_error', session_id: 's2', code: 'brand_new', error: 'upstream exploded' })

    expect(notices('s2')[0]).toContain('upstream exploded')
  })

  it('ignores a turn_error for another session', () => {
    wireMobileSession('s3')

    handlers.get('turn_error')!({ type: 'turn_error', session_id: 'other', code: 'insufficient_credits', error: 'x' })

    expect(notices('s3')).toEqual([])
  })
})
