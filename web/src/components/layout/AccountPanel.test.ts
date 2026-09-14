import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { get } from 'svelte/store'
import { locale } from '../../lib/i18n'
import { productState } from '../../lib/product'
import { accountPanelOpen, accountPanelPage, toasts } from '../../lib/stores'
import AccountPanel from './AccountPanel.svelte'

// V-54 / PR-2f: the logout answer has two halves, and the view has to say so.
//
// WHY THIS IS A COMPONENT TEST. The wiring being pinned is one line inside
// AccountPanel (a toast when `revoked` is false), and this repo has already been
// burned by the opposite arrangement: V-23 recorded a specification that looked
// complete and had no call site at all, and 开发规范 §6.4.3 records that PR-2b1's
// endpoints were unreachable with every branch correct. A helper returning the
// right boolean proves nothing about the user seeing anything, so the assertion
// is made against the toast store after a click.
//
// The two mocks are the two things the panel would otherwise do for real: talk to
// the local API, and open a confirmation dialog. Everything else - the button, the
// click, the toast store - is the production path.

const logout = vi.fn<() => Promise<{ revoked: boolean }>>()
vi.mock('../../lib/product', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/product')>()
  return { ...actual, logout: () => logout() }
})

vi.mock('../../lib/confirm', () => ({
  confirmDialog: () => Promise.resolve(true),
}))

let target: HTMLElement
let app: Record<string, unknown> | null = null

beforeEach(() => {
  logout.mockReset()
  toasts.set([])
  locale.set('zh')
  accountPanelOpen.set(true)
  accountPanelPage.set('root')
  productState.set({
    schemaVersion: 1,
    loggedIn: true,
    activated: true,
    credits: { balance: 0 },
    plan: { name: '' },
    prefs: { locale: 'zh', inputSensitiveCheck: true, defaultChatMode: 'default' },
    suppressOnboarding: true,
  } as never)
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  toasts.set([])
  accountPanelOpen.set(false)
  accountPanelPage.set('root')
})

function render() {
  // `anchorEl` is a required prop whose value may be null (the panel reads the
  // trigger's rect for its geometry; null means "no trigger", which is what a
  // test mounting the panel directly has). Passing `{}` satisfies esbuild but
  // fails svelte-check — V-63.
  app = mount(AccountPanel, { target, props: { anchorEl: null } }) as Record<string, unknown>
  flushSync()
}

async function clickLogout() {
  // The panel is portaled to <body> (its own header comment says why: the
  // sidebar clips overflow and the rail is narrower than the panel), so it is not
  // inside `target` at all. Querying the document is what the portal requires,
  // not a shortcut.
  const button = document.body.querySelector<HTMLButtonElement>('button.logout')
  if (!button) throw new Error('the panel rendered no logout button')
  button.click()
  // The handler chains two awaits (the confirm dialog, then the logout call), and
  // it is a chain rather than one hop, so macro-task turns are used to drain it -
  // counting microtasks by hand is the kind of assertion that breaks the next time
  // an await is added before the toast.
  for (let turn = 0; turn < 3; turn++) await new Promise((resolve) => setTimeout(resolve, 0))
  flushSync()
}

function toastMessages(): string[] {
  return get(toasts).map((entry) => entry.msg)
}

// 1. THE SENTENCE APPEARS when the platform session was not revoked. Without the
// toast the user is told nothing, and silence reads as "the copy on my u-disk is
// dead now" - the one thing that may not be true.
it('tells the user when the platform session was not revoked', async () => {
  logout.mockResolvedValue({ revoked: false })
  render()

  await clickLogout()

  const messages = toastMessages()
  expect(messages.length).toBe(1)
  expect(messages[0]).toContain('平台会话未能撤销')
})

// 2. AND IT STAYS QUIET on the ordinary path. A notice that fires on every logout
// is noise, and noise is how a real warning gets ignored (the reverse of the
// first test - without this one, a hardcoded toast would pass).
it('shows nothing when the platform session was revoked', async () => {
  logout.mockResolvedValue({ revoked: true })
  render()

  await clickLogout()

  expect(toastMessages()).toEqual([])
})

// 3. THE PANEL CLOSES EITHER WAY, because the local half always succeeded: the
// user asked to sign out and did sign out (PQ29 option 1). Leaving the panel open
// would suggest the click did nothing.
it('closes the panel even when the platform session was not revoked', async () => {
  logout.mockResolvedValue({ revoked: false })
  render()

  await clickLogout()

  expect(get(accountPanelOpen)).toBe(false)
})
