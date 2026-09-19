import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { get } from 'svelte/store'
import { locale } from '../../../lib/i18n'
import { productState, updateNickname, updatePrefs, ProductError } from '../../../lib/product'
import { toasts } from '../../../lib/stores'
import SettingsPage from './SettingsPage.svelte'

// PR-6c / L-D3 前端半边：昵称的两档拒绝（格式 / 敏感词）必须在账户面板上以
// 不同文案呈现，而不是静默失败或混为一谈（§6.4.3 / V-23：一个返回字符串的
// 函数证明不了用户看见什么，所以每条断言读渲染树或 toast store）。同一页的
// 语言切换是 L-E5 的偏好半边 —— 断言它走 updatePrefs。
//
// 这里 mock 的只有 updateNickname / updatePrefs（本地 API）；组件挂载、输入、
// 点击、字段错误渲染、toast store 都是生产路径。

vi.mock('../../../lib/product', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../../lib/product')>()
  return {
    ...actual,
    updateNickname: vi.fn(async () => ({})),
    updatePrefs: vi.fn(async () => ({})),
  }
})

let target: HTMLElement
let app: Record<string, unknown> | null = null

function setState(overrides: Record<string, unknown> = {}) {
  const base: Record<string, unknown> = {
    schemaVersion: 1,
    loggedIn: true,
    activated: true,
    account: { nickname: '', phoneMasked: '', lastLoginAt: '' },
    credits: { balance: 0 },
    plan: { name: '' },
    prefs: { locale: 'zh', inputSensitiveCheck: true },
    suppressOnboarding: true,
  }
  productState.set({ ...base, ...overrides } as never)
}

beforeEach(() => {
  locale.set('zh')
  toasts.set([])
  target = document.createElement('div')
  document.body.appendChild(target)
  vi.mocked(updateNickname).mockClear()
  vi.mocked(updatePrefs).mockClear()
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  toasts.set([])
  productState.set(null)
  locale.set('zh')
})

function render() {
  app = mount(SettingsPage, { target }) as Record<string, unknown>
  flushSync()
}

async function typeNickname(value: string) {
  const input = target.querySelector<HTMLInputElement>('#nickname-input')!
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  flushSync()
}

async function settle() {
  // saveNickname awaits updateNickname; two ticks let the awaited branch settle
  // before we read the DOM/toast (flushSync alone would read the pre-await state).
  await new Promise((r) => setTimeout(r, 0))
  await new Promise((r) => setTimeout(r, 0))
  flushSync()
}

function toastMessages(): string[] {
  return get(toasts).map((e) => e.msg)
}

describe('SettingsPage nickname and preferences', () => {
  // 判据 1：合法昵称 → updateNickname 被调 + 「昵称已更新」toast。
  it('saves a legal nickname and shows the saved toast', async () => {
    setState()
    render()

    await typeNickname('新昵称')
    target.querySelector<HTMLButtonElement>('button.save')!.click()
    await settle()

    expect(updateNickname).toHaveBeenCalledWith('新昵称')
    expect(toastMessages()).toContain('昵称已更新')
  })

  // 判据 2：服务端拒绝 nickname_format → 字段级文案「昵称格式不正确」。
  it('shows the format error when the server refuses with nickname_format', async () => {
    setState()
    vi.mocked(updateNickname).mockRejectedValueOnce(new ProductError(400, {}, 'nickname_format'))
    render()

    await typeNickname('新昵称')
    target.querySelector<HTMLButtonElement>('button.save')!.click()
    await settle()

    expect(target.querySelector('.field-err')?.textContent).toContain('昵称格式不正确')
    expect(toastMessages()).toHaveLength(0)
  })

  // 判据 3：服务端拒绝 nickname_sensitive → 敏感词文案（反钉：不是格式文案）。
  it('shows the sensitive error when the server refuses with nickname_sensitive', async () => {
    setState()
    vi.mocked(updateNickname).mockRejectedValueOnce(new ProductError(400, {}, 'nickname_sensitive'))
    render()

    await typeNickname('新昵称')
    target.querySelector<HTMLButtonElement>('button.save')!.click()
    await settle()

    const err = target.querySelector('.field-err')?.textContent ?? ''
    expect(err).toContain('昵称包含敏感内容')
    expect(err).not.toContain('格式')
  })

})
