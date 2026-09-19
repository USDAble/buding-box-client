import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { locale } from '../../../lib/i18n'
import { productState } from '../../../lib/product'
import LicensePage from './LicensePage.svelte'

// L-A5 / E5 rule 1: the license page must show the three licence facts on one
// page — days left, the masked phone, and the box code. Today the masked phone
// sits on SettingsPage and this page has zero component tests (V-23). A function
// returning the right string proves nothing about the user seeing it (开发规范
// §6.4.3), so every assertion reads the rendered tree.
//
// Nothing here talks to the network or opens a window: the component reads the
// productState store and the i18n / license helpers, so the production path is
// what the test mounts — no mocks needed.

let target: HTMLElement
let app: Record<string, unknown> | null = null

const iso = (daysFromNow: number) =>
  new Date(Date.now() + daysFromNow * 86_400_000).toISOString()

function setState(overrides: Record<string, unknown> = {}) {
  const base: Record<string, unknown> = {
    schemaVersion: 1,
    loggedIn: true,
    activated: true,
    activation: {
      activatedAt: iso(-1),
      expiresAt: iso(3),
      boxCode: 'BOX-DEMO-0001',
    },
    account: { phoneMasked: '138****8000', nickname: 'n', lastLoginAt: '' },
    credits: { balance: 0 },
    plan: { name: '' },
    prefs: { locale: 'zh', inputSensitiveCheck: true },
    suppressOnboarding: true,
  }
  productState.set({ ...base, ...overrides } as never)
}

beforeEach(() => {
  locale.set('zh')
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  productState.set(null)
})

function render() {
  app = mount(LicensePage, { target }) as Record<string, unknown>
  flushSync()
}

// 1. THE THREE FACTS ON ONE PAGE. This is the criterion E5 rule 1 exists to
// pin: not "three tests each finding one value", but all three in the same
// render tree — the whole point is that the user no longer flips to SettingsPage.
it('renders days left, the masked phone and the box code on one page', () => {
  setState()
  render()

  const text = target.textContent ?? ''
  expect(text).toContain('剩余 3 天')
  expect(text).toContain('138****8000')
  expect(text).toContain('BOX-DEMO-0001')
})

// 2. THE DAYS NUMBER USES THE LOCAL CALENDAR. license.ts already has a unit
// test for the arithmetic; here the assertion is on what reaches the screen.
it('shows the calendar days left as the number on the page', () => {
  setState({ activation: { activatedAt: iso(-1), expiresAt: iso(3), boxCode: 'BOX-DEMO-0001' } })
  render()

  expect(target.textContent).toContain('剩余 3 天')
})

// 3. AN EXPIRED LICENSE RENDERS "0 days" AND DOES NOT BLOCK. The panel still
// draws — expiry is display-only this phase (E5 rule 2 / PQ7).
it('renders 0 days and the expired label when the date is in the past', () => {
  setState({ activation: { activatedAt: iso(-2), expiresAt: iso(-1), boxCode: 'BOX-DEMO-0001' } })
  render()

  const text = target.textContent ?? ''
  expect(text).toContain('已过期')
  expect(text).toContain('0')
  // still fully rendered — not an empty/blocked panel
  expect(text).toContain('BOX-DEMO-0001')
})

// 4. THE MASKED PHONE IS RENDERED VERBATIM. The platform already masks, so a
// second mask would mangle 138****8000 into 138******00 (reverse nail).
it('renders the masked phone exactly as the server sent it', () => {
  setState({ account: { phoneMasked: '138****8000', nickname: 'n', lastLoginAt: '' } })
  render()

  expect(target.textContent).toContain('138****8000')
  expect(target.textContent).not.toContain('138******00')
})

// 5. OLD DATA WITHOUT boxCode RENDERS "—" AND BLOCKS NOTHING (PQ19).
it('renders an em dash for the box code when the field is missing', () => {
  setState({ activation: { activatedAt: iso(-1), expiresAt: iso(3) } })
  render()

  const text = target.textContent ?? ''
  expect(text).toContain('盒子编号：—')
  // the other two facts are unaffected
  expect(text).toContain('138****8000')
})

// 6. A MISSING phoneMasked RENDERS "—" AND BLOCKS NOTHING.
it('renders an em dash for the phone when the field is missing', () => {
  setState({ account: { nickname: 'n', lastLoginAt: '' } })
  render()

  const text = target.textContent ?? ''
  expect(text).toContain('绑定手机号：—')
  expect(text).toContain('BOX-DEMO-0001')
})
