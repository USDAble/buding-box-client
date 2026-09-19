import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

// The desktop window's hop from a product WS event to what the user sees.
//
// WHY THIS EXISTS (V-97). The server side of the freeze chain has four nails
// (internal/server/product_events_test.go): the event is emitted globally, the credit
// nudge carries nothing, datastore:lost is replayed to a connection that arrives while
// the root is gone, and a healthy root replays nothing. Not one of them can see
// whether the WINDOW does anything with the event: a build that kept product_events.go
// intact and registered the handlers under misspelt event names would leave all four
// green while a pulled U盘 showed no overlay and an empty credits_update refreshed
// nothing. That is the gap V-66 registered for `complete` → ChatView, and the same one
// that burned L-D2 ("the wiring point was wrong and every test stayed green").
//
// The wiring is a plain module over the ws singleton, so the test drives the exact
// bytes the socket would deliver — the shapes are the ones 本地API契约 §4 registers.

const handlers = new Map<string, (ev: any) => void>()

vi.mock('./ws', () => ({
  ws: {
    on: vi.fn((type: string, fn: (ev: any) => void) => {
      handlers.set(type, fn)
      return () => handlers.delete(type)
    }),
  },
}))

const { refreshCredits } = vi.hoisted(() => ({ refreshCredits: vi.fn(async () => 0) }))

// Partial mock: only the ledger read is faked. productState stays REAL, because the
// counter-nail below asserts the event does NOT write to it — and a stubbed store
// could not be written to by the bug it is there to catch.
vi.mock('./product', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./product')>()),
  refreshCredits: () => refreshCredits(),
}))

import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { frozen } from './stores'
import { productState, type ProductStateDTO } from './product'
import { wireProductEvents } from './productEvents'

// handler names the callback the socket would invoke. A misspelt name in the wiring
// makes this throw with the reason, rather than letting a test pass by asserting on a
// store that nothing ever writes.
function handler(type: string): (ev: any) => void {
  const fn = handlers.get(type)
  if (!fn) throw new Error(`no handler registered for "${type}" — the window ignores this event`)
  return fn
}

function stateWith(balance: number): ProductStateDTO {
  return {
    schemaVersion: 1,
    loggedIn: true,
    activated: true,
    account: null,
    credits: { balance },
    plan: { name: 'basic' },
    prefs: { locale: 'zh', inputSensitiveCheck: false },
    suppressOnboarding: true,
  }
}

beforeEach(() => {
  handlers.clear()
  frozen.set(false)
  productState.set(null)
  refreshCredits.mockClear()
})

describe('the freeze events drive the store the overlay reads', () => {
  it('freezes on datastore:lost and thaws on datastore:restored', () => {
    wireProductEvents()

    handler('datastore:lost')({ type: 'datastore:lost' })
    expect(get(frozen)).toBe(true)

    handler('datastore:restored')({ type: 'datastore:restored' })
    expect(get(frozen)).toBe(false)
  })

  it('thaws only on restored, never on a repeat of the event that froze it', () => {
    // The counter-nail: a handler wired to clear the flag on the wrong event (or one
    // that toggles instead of setting) passes the test above and would un-freeze a
    // window whose data root is still gone.
    wireProductEvents()
    handler('datastore:lost')({ type: 'datastore:lost' })
    handler('datastore:lost')({ type: 'datastore:lost' })
    expect(get(frozen)).toBe(true)
  })
})

describe('the credits nudge asks the ledger and merges nothing', () => {
  it('asks the ledger for the number it does not trust the event to carry', () => {
    wireProductEvents()
    productState.set(stateWith(10))

    // The shape the server actually sends: no payload at all.
    handler('credits_update')({ type: 'credits_update' })

    expect(refreshCredits).toHaveBeenCalledTimes(1)
  })

  it('does not let a payload balance become a second writer of the ledger number', () => {
    // This is the bug the handler was fixed for on 2026-09-14: it used to run
    // `productState.update(s => ({ ...s, credits: ev.credits }))`, which made a pushed
    // number a second writer of a value the ledger owns (需求基线 E9 rule 2) — and a
    // stale or reordered push would overwrite a fresher read with no way to tell.
    wireProductEvents()
    productState.set(stateWith(10))

    handler('credits_update')({ type: 'credits_update', credits: { balance: 999 } })

    expect(refreshCredits).toHaveBeenCalledTimes(1)
    expect(get(productState)?.credits.balance).toBe(10)
  })
})

describe('the shell wires the product events through this module', () => {
  // A source scan rather than a behavioural test, because the failure is "a second
  // registration site": the two tests above drive this module directly, so they would
  // stay green if App.svelte kept its own inline handlers and never called it. Same
  // reasoning — and the same shape — as localeSource.test.ts.
  const app = readFileSync(join(process.cwd(), 'src', 'App.svelte'), 'utf8')

  function withoutComments(source: string): string {
    return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\/\/[^\n]*/g, '')
  }

  it('calls wireProductEvents, so the handlers above are the ones a window installs', () => {
    expect(withoutComments(app)).toContain('wireProductEvents(')
  })

  it('registers none of the three events itself', () => {
    const code = withoutComments(app)
    for (const type of ['datastore:lost', 'datastore:restored', 'credits_update']) {
      expect(
        code,
        `App.svelte registers ${type} itself — two registration sites drift apart, ` +
          'and the nails in this file only cover the one in productEvents.ts',
      ).not.toContain(type)
    }
  })
})
