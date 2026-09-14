import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { get } from 'svelte/store'
import { locale } from '../../../lib/i18n'
import { productState } from '../../../lib/product'
import { toasts } from '../../../lib/stores'
import CreditsPage from './CreditsPage.svelte'
import PlanPage from './PlanPage.svelte'

// PR-5d2 / L-C4b + E10: 充值 and 升级 used to be permanently `disabled`, which is
// a dead end rather than a placeholder — a disabled control cannot be focused or
// clicked and its `title` never shows on a touch screen, so E10's "点击打开占位或
// 外链" was not reachable at all. These nails assert the click produces a visible
// answer.
//
// WHY A COMPONENT TEST AND NOT A UNIT TEST OF A HANDLER. 开发规范 §6.4.3 records a
// PR whose every branch was correct and whose endpoints were unreachable; V-23
// records a helper that looked complete with no call site. A function that
// returns a string proves nothing about the user seeing it, so the assertion is
// made against the toast store after a real click on the rendered button.
//
// The two things these pages do for real are mocked: the local API
// (refreshCredits, which CreditsPage calls on mount) and window.open. Everything
// else — the button, the click, the toast store — is the production path.

vi.mock('../../../lib/product', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../../lib/product')>()
  return { ...actual, refreshCredits: () => Promise.resolve() }
})

let target: HTMLElement
let app: Record<string, unknown> | null = null
let openSpy: ReturnType<typeof vi.spyOn>

beforeEach(() => {
  toasts.set([])
  locale.set('zh')
  target = document.createElement('div')
  document.body.appendChild(target)
  // "No real payment flow" has one testable half: nothing external is opened.
  openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  toasts.set([])
  openSpy.mockRestore()
})

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

async function clickButton(selector: string) {
  const button = target.querySelector<HTMLButtonElement>(selector)
  if (!button) throw new Error(`no ${selector} button rendered`)
  button.click()
  // The handler is synchronous, but showToast goes through a Svelte store; flush
  // so the assertion reads the settled value rather than a mid-update one.
  await Promise.resolve()
  flushSync()
}

function toastMessages(): string[] {
  return get(toasts).map((entry) => entry.msg)
}

function renderCredits() {
  app = mount(CreditsPage, { target }) as Record<string, unknown>
  flushSync()
}

function renderPlan() {
  app = mount(PlanPage, { target }) as Record<string, unknown>
  flushSync()
}

// 1. THE ENTRY ANSWERS. This is L-C4b's判据.
it('answers the 充值 entry point with a notice instead of doing nothing', async () => {
  setBalance(5000)
  renderCredits()

  await clickButton('button.btn-recharge')

  expect(toastMessages()).toHaveLength(1)
  expect(toastMessages()[0]).toContain('充值')
})

// 2. AND IT IS NOT GATED ON THE BALANCE (PQ8 / E9 rule 6). The requirement is
// that the entry is permanent, "regardless of whether the balance is 0 and
// regardless of whether a 402 arrived" — and the naive designs sit at BOTH ends
// of that range, which is why there are two nails here rather than one:
//
//   - "show it only when the balance is 0" (top up means you ran out) — caught by
//     the ample-balance nail below.
//   - "hide it when the balance is 0" (nothing left to spend, so nothing to
//     change) — caught by the zero-balance nail.
//
// A single nail at one balance passes against the other mistake: the first
// version of this file only asserted the ample case, and a `disabled={balance <= 0}`
// mutation passed it. Hence both.
it('keeps the 充值 entry clickable when the balance is ample', async () => {
  setBalance(12500)
  renderCredits()

  const button = target.querySelector<HTMLButtonElement>('button.btn-recharge')
  expect(button).toBeTruthy()
  expect(button!.disabled).toBe(false)

  await clickButton('button.btn-recharge')
  expect(toastMessages()).toHaveLength(1)
})

it('keeps the 充值 entry clickable when the balance is zero', async () => {
  setBalance(0)
  renderCredits()

  const button = target.querySelector<HTMLButtonElement>('button.btn-recharge')
  expect(button).toBeTruthy()
  expect(button!.disabled).toBe(false)

  await clickButton('button.btn-recharge')
  expect(toastMessages()).toHaveLength(1)
})

// 3. NO REAL PAYMENT STREAM. A build that "made the button work" by opening a
// payment page — or by inlining a URL that no contract, brand file or profile
// declares — would satisfy the two nails above and ship something we cannot
// support. Nothing external opens, and there is nowhere for a hardcoded URL to
// live anyway: this asserts the first half.
it('opens nothing external when the 充值 entry is clicked', async () => {
  setBalance(5000)
  renderCredits()

  await clickButton('button.btn-recharge')

  expect(openSpy).not.toHaveBeenCalled()
})

// 4. E10's OTHER HALF. The same requirement sentence covers 升级, but only 充值
// has a closed-loop row, so nothing else would have caught 升级 staying dead —
// and a panel where one entry answers and its twin does not is the state this PR
// exists to remove.
it('answers the 升级 entry point on the plan page too', async () => {
  setBalance(5000)
  renderPlan()

  await clickButton('button.btn-upgrade')

  expect(toastMessages()).toHaveLength(1)
  expect(toastMessages()[0]).toContain('升级')
  expect(openSpy).not.toHaveBeenCalled()
})
