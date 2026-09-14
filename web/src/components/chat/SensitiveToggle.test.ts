import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { locale } from '../../lib/i18n'
import { productState, updatePrefs } from '../../lib/product'
import { confirmDialog } from '../../lib/confirm'
import SensitiveToggle from './SensitiveToggle.svelte'

// PR-6c / L-D1 前端半边：输入敏感词检测的开关，关闭前必须确认（关前确认），
// 取消则不变。开发规范 §6.4.3 / V-23：一个返回字符串的函数证明不了用户看见
// 什么，所以断言读真实点击路径 + updatePrefs 的调用。
//
// 这里 mock 的只有 updatePrefs（本地 API）与 confirmDialog；组件挂载、点击、
// 开关状态读取（$productState.prefs.inputSensitiveCheck）都是生产路径。

vi.mock('../../lib/product', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/product')>()
  return { ...actual, updatePrefs: vi.fn(async () => ({})), updateNickname: vi.fn(async () => ({})) }
})

vi.mock('../../lib/confirm', () => ({
  confirmDialog: vi.fn(async () => true),
}))

let target: HTMLElement
let app: Record<string, unknown> | null = null

function setPrefs(inputSensitiveCheck: boolean) {
  productState.set({
    schemaVersion: 1,
    loggedIn: true,
    activated: true,
    account: { nickname: '', phoneMasked: '', lastLoginAt: '' },
    credits: { balance: 0 },
    plan: { name: '' },
    prefs: { locale: 'zh', inputSensitiveCheck, defaultChatMode: 'default' },
    suppressOnboarding: true,
  } as never)
}

beforeEach(() => {
  locale.set('zh')
  target = document.createElement('div')
  document.body.appendChild(target)
  vi.mocked(updatePrefs).mockClear()
  vi.mocked(confirmDialog).mockClear()
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  productState.set(null)
})

function render() {
  app = mount(SensitiveToggle, { target }) as Record<string, unknown>
  flushSync()
}

async function settle() {
  await new Promise((r) => setTimeout(r, 0))
  await new Promise((r) => setTimeout(r, 0))
  flushSync()
}

describe('SensitiveToggle', () => {
  // 判据 6（反钉）：取消确认 ⇒ 什么都不写。
  it('asks for confirmation before turning the input check off', async () => {
    setPrefs(true)
    vi.mocked(confirmDialog).mockResolvedValueOnce(false)
    render()

    target.querySelector<HTMLButtonElement>('button.row')!.click()
    await settle()

    expect(confirmDialog).toHaveBeenCalled()
    expect(updatePrefs).not.toHaveBeenCalled()
  })

  // 判据 6：确认 ⇒ 写 inputSensitiveCheck:false。
  it('persists the off state only after confirmation', async () => {
    setPrefs(true)
    vi.mocked(confirmDialog).mockResolvedValueOnce(true)
    render()

    target.querySelector<HTMLButtonElement>('button.row')!.click()
    await settle()

    expect(updatePrefs).toHaveBeenCalledWith({ inputSensitiveCheck: false })
  })
})
