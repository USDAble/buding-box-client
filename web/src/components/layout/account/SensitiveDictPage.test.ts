import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { get } from 'svelte/store'
import { locale } from '../../../lib/i18n'
import { fetchDict, importWords, saveDict } from '../../../lib/sensitiveDict'
import { RequestError } from '../../../lib/api'
import { confirmDialog } from '../../../lib/confirm'
import { toasts } from '../../../lib/stores'
import SensitiveDictPage from './SensitiveDictPage.svelte'

// PR-6c / L-D4a+b+c 前端半边：词库页读回的三层（内置 / 用户）要上屏，导入要
// 先走 dryRun 预览（展示 added/skipped）再经确认落盘。开发规范 §6.4.3 / V-23：
// 一个返回字符串的函数证明不了用户看见什么，所以断言读渲染树与 confirm 文案。
//
// 这里 mock 的只有网络（fetchDict / saveDict / importWords / exportDict）与
// confirmDialog；parseDictText / normalizeWord 等纯函数走真实实现（它们也是
// 导入流程的一部分）。

vi.mock('../../../lib/sensitiveDict', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../../lib/sensitiveDict')>()
  return {
    ...actual,
    fetchDict: vi.fn(async () => ({ builtin: [], user: [] })),
    saveDict: vi.fn(async (user: string[]) => ({ user })),
    importWords: vi.fn(async () => ({ added: 0, skipped: 0 })),
    exportDict: vi.fn(async () => {}),
  }
})

vi.mock('../../../lib/confirm', () => ({
  confirmDialog: vi.fn(async () => true),
}))

let target: HTMLElement
let app: Record<string, unknown> | null = null

beforeEach(() => {
  locale.set('zh')
  toasts.set([])
  target = document.createElement('div')
  document.body.appendChild(target)
  vi.mocked(fetchDict).mockClear()
  vi.mocked(saveDict).mockClear()
  vi.mocked(importWords).mockClear()
  vi.mocked(confirmDialog).mockClear()
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  toasts.set([])
})

function render() {
  app = mount(SensitiveDictPage, { target }) as Record<string, unknown>
  flushSync()
}

async function settle() {
  // load() and onImportFile await network; two ticks let the awaited branches
  // settle before we read the DOM.
  await new Promise((r) => setTimeout(r, 0))
  await new Promise((r) => setTimeout(r, 0))
  flushSync()
}

function toastMessages(): string[] {
  return get(toasts).map((e) => e.msg)
}

describe('SensitiveDictPage', () => {
  // 判据 4：fetchDict 的 builtin + user 各渲染一栏，且内置词只读（无删除钮）。
  it('renders builtin (read-only) and user words from fetchDict', async () => {
    vi.mocked(fetchDict).mockResolvedValueOnce({ builtin: ['内置词'], user: ['用户词'] })
    render()
    await settle()

    expect(target.textContent).toContain('用户词')

    // builtin 栏默认折叠；展开后才能断言它的词与只读形态。
    target.querySelector<HTMLButtonElement>('.section-head')!.click()
    flushSync()
    const readOnly = target.querySelector('ul.words.read-only')
    expect(readOnly).toBeTruthy()
    expect(readOnly!.textContent).toContain('内置词')
  })

  // 判据 5：导入先 dryRun 预览（展示 added/skipped），确认后才落盘（dryRun=false）。
  it('imports through dryRun preview and writes only after confirm', async () => {
    vi.mocked(importWords).mockResolvedValueOnce({ added: 2, skipped: 1 })
    vi.mocked(importWords).mockResolvedValueOnce({ added: 2, skipped: 1 })
    render()
    await settle()

    const input = target.querySelector<HTMLInputElement>('input.file-input')!
    Object.defineProperty(input, 'files', {
      value: [{ text: async () => '词A\n词B\n词C\n' }],
      configurable: true,
    })
    input.dispatchEvent(new Event('change', { bubbles: true }))
    await settle()

    expect(importWords).toHaveBeenNthCalledWith(1, ['词A', '词B', '词C'], true)
    const msg = vi.mocked(confirmDialog).mock.calls[0][0] as string
    expect(msg).toContain('2')
    expect(msg).toContain('1')
    expect(importWords).toHaveBeenNthCalledWith(2, ['词A', '词B', '词C'], false)
  })

  // 判据 6（V-76）：saveDict 被服务端以 invalid_word 拒绝 ⇒ 页面显示「该词无效」，
  // 而不是「400 Bad Request」。反钉：文案里没有「400」。
  it('shows the invalid_word copy when the server refuses the word (V-76)', async () => {
    vi.mocked(fetchDict).mockResolvedValueOnce({ builtin: [], user: [] })
    vi.mocked(saveDict).mockRejectedValueOnce(
      new RequestError('400 Bad Request', 'invalid_word', null, '！@#'),
    )
    render()
    await settle()

    const input = target.querySelector<HTMLInputElement>('.add-row input')!
    input.value = '新词'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
    target.querySelector<HTMLButtonElement>('.add-btn')!.click()
    await settle()

    expect(saveDict).toHaveBeenCalled()
    expect(toastMessages()).toContain('该词无效')
    expect(toastMessages().join(' ')).not.toContain('400')
  })

  // 判据 6（V-76 的另一半）：非业务失败（500）不再泄漏 HTTP 状态行，而是回
  // 「发送失败，请重试」。
  it('shows the generic copy instead of the HTTP status line on a 500 (V-76)', async () => {
    vi.mocked(fetchDict).mockResolvedValueOnce({ builtin: [], user: [] })
    vi.mocked(saveDict).mockRejectedValueOnce(new RequestError('500 Internal Server Error'))
    render()
    await settle()

    const input = target.querySelector<HTMLInputElement>('.add-row input')!
    input.value = '新词'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
    target.querySelector<HTMLButtonElement>('.add-btn')!.click()
    await settle()

    expect(toastMessages()).toContain('发送失败，请重试')
    expect(toastMessages().join(' ')).not.toContain('500')
  })
})
