import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { get } from 'svelte/store'
import { locale } from '../../lib/i18n'
import { productState } from '../../lib/product'
import { accountPanelOpen, settingsModalOpen, settingsTarget } from '../../lib/stores'
import AccountPanel from './AccountPanel.svelte'

let target: HTMLElement
let app: Record<string, unknown> | null = null

beforeEach(() => {
  locale.set('zh')
  accountPanelOpen.set(true)
  settingsModalOpen.set(false)
  settingsTarget.set(null)
  productState.set({
    schemaVersion: 1, loggedIn: true, activated: true,
    account: { nickname: '测试用户', phoneMasked: '138****0000' },
    credits: { balance: 12 }, plan: { name: 'trial' },
    prefs: { locale: 'zh', inputSensitiveCheck: true }, suppressOnboarding: true,
  } as never)
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  accountPanelOpen.set(false)
  settingsModalOpen.set(false)
  settingsTarget.set(null)
})

function render() {
  app = mount(AccountPanel, { target, props: { anchorEl: null } }) as Record<string, unknown>
  flushSync()
}

function buttonWithText(text: string): HTMLButtonElement {
  const button = [...document.body.querySelectorAll('button')].find((item) => item.textContent?.includes(text))
  if (!button) throw new Error(`missing button ${text}`)
  return button as HTMLButtonElement
}

describe('AccountPanel', () => {
  it('keeps the compact panel single-level and moves safety management to Settings', () => {
    render()
    expect(document.body.querySelector('button.logout')).toBeNull()

    buttonWithText('安全与隐私').click()
    flushSync()

    expect(get(accountPanelOpen)).toBe(false)
    expect(get(settingsModalOpen)).toBe(true)
    expect(get(settingsTarget)).toEqual({ cat: 'safety', dataSubView: undefined })
  })
})
