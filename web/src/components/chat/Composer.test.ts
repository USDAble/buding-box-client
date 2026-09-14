import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { locale } from '../../lib/i18n'
import { productState } from '../../lib/product'
import { activeSessionId } from '../../lib/stores'
import { checkSensitive } from '../../lib/sensitive'
import Composer from './Composer.svelte'

// V-58 / PR-5d3: the composer must not turn a zero balance into a UI conclusion.
//
// PQ8 (2026-09-11) is the requirement: 客户端**不做任何额度判断**，全部由中台 402
// 驱动. Its second half is why this needs a counter-nail rather than one assertion:
// "balance 0 ⇒ show 积分不足" is the same mistake as "hide the top-up entry at 0" —
// a user on a free or bundled model may still be able to send. Deleting the strip
// outright would satisfy a one-sided test, so the sensitive-word notice (P8, the
// strip's other owner) is driven here and must still appear.
//
// Composer reaches for the local API on mount (models, skills, agents, MCP) and
// subscribes to the websocket. Neither is what this test is about, so both are stubbed;
// everything else — the notices array, the props, the rendering, the send path — is
// production code.

vi.mock('../../lib/api', () => ({
  listEndpoints: vi.fn(async () => ({ endpoints: [] })),
  listSkills: vi.fn(async () => []),
  listAgents: vi.fn(async () => []),
  listWorkflows: vi.fn(async () => []),
  listMcpServers: vi.fn(async () => ({ servers: [] })),
  getMcpServer: vi.fn(async () => ({})),
  listSessions: vi.fn(async () => []),
}))

vi.mock('../../lib/ws', () => ({
  ws: { on: vi.fn(() => () => {}), send: vi.fn(), interrupt: vi.fn(), connected: { subscribe: vi.fn(() => () => {}) } },
}))

vi.mock('../../lib/sensitive', () => ({
  checkSensitive: vi.fn(async () => ({ hit: false, masked: '' })),
}))

let target: HTMLElement
let app: Record<string, unknown> | null = null

function setBalance(balance: number) {
  productState.set({
    schemaVersion: 1,
    loggedIn: true,
    activated: true,
    credits: { balance },
    plan: { name: '' },
    prefs: { locale: 'zh', inputSensitiveCheck: true, defaultChatMode: 'default' },
    suppressOnboarding: true,
  } as never)
}

function render() {
  app = mount(Composer, { target, props: {} }) as Record<string, unknown>
  flushSync()
}

function noticeText(): string {
  return target.querySelector('[data-composer-notices]')?.textContent ?? ''
}

// sendWord types into the composer and clicks send, the way a user does. The send
// path is what clears (or sets) sensitiveHit, so a test that only rendered would
// never reach the notice it is looking for.
async function sendWord(word: string) {
  const box = target.querySelector('textarea') as HTMLTextAreaElement
  box.value = word
  box.dispatchEvent(new Event('input', { bubbles: true }))
  flushSync()
  const send = [...target.querySelectorAll('button')].find(b => b.className.includes('send-btn')) as HTMLButtonElement
  send.click()
  // checkSensitive is async; two ticks let the awaited branch settle before we read
  // the DOM. flushSync alone would read the pre-await state.
  await new Promise(r => setTimeout(r, 0))
  await new Promise(r => setTimeout(r, 0))
  flushSync()
}

beforeEach(() => {
  locale.set('zh')
  activeSessionId.set('s1')
  vi.mocked(checkSensitive).mockResolvedValue({ hit: false, masked: '' } as never)
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  productState.set(null)
  activeSessionId.set(null)
})

describe('the composer does not decide quota', () => {
  it('derives no notice at all from a zero balance', () => {
    setBalance(0)
    render()

    // Absence of the strip, not just absence of the words: a notice keyed off the
    // balance is the defect whichever sentence it carries, and asserting on the copy
    // would let a re-wording slip through.
    expect(target.querySelector('[data-composer-notices]')).toBeNull()
    expect(noticeText()).not.toContain('积分不足')
  })

  it('derives no notice at all from a positive balance either', () => {
    setBalance(12500)
    render()

    expect(target.querySelector('[data-composer-notices]')).toBeNull()
    expect(noticeText()).not.toContain('积分不足')
  })

  it('still shows the notice the strip is meant to own', async () => {
    // The counter-nail. Nothing about the balance should be able to suppress P8's
    // warning, so this drives a sensitive-word hit with the balance at zero — the
    // exact input the deleted branch used to fire on.
    setBalance(0)
    vi.mocked(checkSensitive).mockResolvedValue({ hit: true, masked: '***' } as never)
    render()

    await sendWord('加我微信')

    const text = noticeText()
    expect(text).not.toBe('')
    expect(text).not.toContain('积分不足')
    expect(target.querySelector('[data-composer-notices]')).not.toBeNull()
  })
})
