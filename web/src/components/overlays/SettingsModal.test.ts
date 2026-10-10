// OCTO-FORK: account refreshes must preserve the user's settings navigation.
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { get } from 'svelte/store'
import { locale } from '../../lib/i18n'
import { productPhase, productState } from '../../lib/product'
import { nativeShell, settingsModalOpen, settingsTarget, openSettingsAt } from '../../lib/stores'
import * as branding from '../../lib/brand'
import * as externalLinks from '../../lib/externalLinks'
import SettingsModal from './SettingsModal.svelte'

let app: ReturnType<typeof mount> | undefined
let target: HTMLDivElement
const accountState = { loggedIn: true, activated: true, account: { nickname: '测试用户' }, credits: { balance: 100, known: true }, prefs: { locale: 'zh' } }
function json(value: unknown) { return new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } }) }
beforeEach(() => {
  locale.set('zh'); productPhase.set('ready'); productState.set(accountState as never)
  settingsModalOpen.set(false); settingsTarget.set(null); nativeShell.set(false)
  sessionStorage.setItem('octo_window_token', 'test')
  vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
    const path = String(input).split('?')[0]
    if (path.endsWith('/credits')) return json({ state: { ...accountState, credits: { balance: 200, known: true } } })
    if (path.endsWith('/wallet')) return json({ available_points: '200.0000', reserved_points: '0.0000', total_recharged_points: '0.0000', total_consumed_points: '0.0000', status: 'active' })
    if (path.endsWith('/recharge/options')) return json({ items: [], payment_methods: [], payment_enabled: false })
    if (path.endsWith('/recharge/intent')) return json({ scope: 'test-scope', intent: null })
    if (path.endsWith('/recharge/orders')) return json({ items: [], next_cursor: '', has_more: false })
    return json({ language: 'zh', current: 'test' })
  }))
  target = document.createElement('div'); document.body.append(target)
})
afterEach(async () => {
  if (app) await unmount(app)
  app = undefined; target.remove(); settingsModalOpen.set(false); settingsTarget.set(null)
  vi.restoreAllMocks(); vi.unstubAllGlobals(); sessionStorage.clear(); productPhase.set('unknown'); nativeShell.set(false)
})
// OCTO-FORK: navigation renders before asynchronous config; assertions need the loaded page.
function activePage() { return target.querySelector('.loading-state') ? undefined : target.querySelector('.rail [aria-current="page"]')?.textContent?.trim() }
function open() { app = mount(SettingsModal, { target }); flushSync() }

// OCTO-FORK: neither mounting Settings nor a delayed config read can revert login's choice.
it('keeps the active language when mounted closed and opened with stale config', async () => {
  productState.set({ ...accountState, prefs: { locale: 'en' } } as never)
  open()
  expect(get(locale)).toBe('en')
  flushSync(() => settingsModalOpen.set(true))
  await vi.waitFor(() => expect(target.querySelector('.setrow select')).not.toBeNull())
  await vi.waitFor(() => expect(vi.mocked(globalThis.fetch).mock.calls.some(([path]) => String(path).endsWith('/api/config'))).toBe(true))
  await vi.waitFor(() => expect(target.querySelector('.loading-state')).toBeNull())
  flushSync()
  expect(get(locale)).toBe('en')
  expect(target.querySelector<HTMLSelectElement>('.setrow select')?.value).toBe('en')
  flushSync(() => locale.set('zh'))
  expect(target.querySelector<HTMLSelectElement>('.setrow select')?.value).toBe('zh')
})
function nicknameEditor() { return target.querySelector<HTMLInputElement>('.account-edit input') }
function nicknameButton(label: string) {
  return [...target.querySelectorAll<HTMLButtonElement>('.account-edit button')].find(button => button.textContent?.trim() === label)!
}
function editNickname(value: string) {
  nicknameButton('编辑').click(); flushSync()
  const input = nicknameEditor()!
  input.value = value; input.dispatchEvent(new Event('input', { bubbles: true })); flushSync()
}

it('hides the co-author toggle without removing other agent defaults', async () => {
  openSettingsAt('agent'); open()
  await vi.waitFor(() => expect(activePage()).toBe('助手默认值'))
  expect(target.textContent).toContain('显示推理过程')
  expect(target.textContent).not.toContain('提交署名')
})

it('persists the console language for the next login wall', async () => {
  settingsModalOpen.set(true); open()
  await vi.waitFor(() => expect(activePage()).toBe('常规'))
  const select = target.querySelector<HTMLSelectElement>('.setrow select')!
  select.value = 'en'
  select.dispatchEvent(new Event('change', { bubbles: true }))
  flushSync()

  await vi.waitFor(() => {
    const paths = vi.mocked(globalThis.fetch).mock.calls.map(([input]) => String(input))
    expect(paths.some(path => path.endsWith('/api/config/language'))).toBe(true)
    expect(paths.some(path => path.endsWith('/api/product/locale'))).toBe(true)
  })
})

it('keeps About information while hiding first-run and removing the license entry', async () => {
  openSettingsAt('about'); open()
  await vi.waitFor(() => expect(activePage()).toBe('关于'))
  expect(target.textContent).toContain('版本')
  expect(target.textContent).not.toContain('首次引导')
  expect(target.textContent).not.toContain('开源许可')
  expect([...target.querySelectorAll('button')].some(button => button.textContent?.includes('重新运行'))).toBe(false)
})

it('shows the nickname as read-only until Edit and cancels without submitting', async () => {
  openSettingsAt('account'); open()
  await vi.waitFor(() => expect(activePage()).toBe('账号'))
  expect(target.querySelector('.account-edit')?.textContent).toContain('测试用户')
  expect(nicknameEditor()).toBeNull()
  editNickname('新昵称')
  expect(nicknameEditor()?.value).toBe('新昵称')
  nicknameButton('取消').click(); flushSync()
  expect(nicknameEditor()).toBeNull()
  expect(target.querySelector('.account-edit')?.textContent).toContain('测试用户')
  expect(vi.mocked(globalThis.fetch).mock.calls.some(([input]) => String(input).endsWith('/api/product/nickname'))).toBe(false)
})

it('submits a nickname only on Save and returns to read-only after success', async () => {
  const original = globalThis.fetch
  let sent: { method?: string; body?: unknown } | undefined
  vi.stubGlobal('fetch', vi.fn(async (input: unknown, init?: RequestInit) => {
    if (String(input).endsWith('/api/product/nickname')) {
      sent = { method: init?.method, body: JSON.parse(String(init?.body)) }
      return json({ state: { ...accountState, account: { nickname: '新昵称' } } })
    }
    return original(input as RequestInfo, init)
  }))
  openSettingsAt('account'); open()
  await vi.waitFor(() => expect(activePage()).toBe('账号'))
  editNickname('新昵称')
  expect(sent).toBeUndefined()
  nicknameButton('保存').click()
  await vi.waitFor(() => expect(nicknameEditor()).toBeNull())
  expect(sent).toEqual({ method: 'PUT', body: { nickname: '新昵称' } })
  expect(target.querySelector('.account-edit')?.textContent).toContain('新昵称')
})

it('keeps the nickname draft editable when Save fails', async () => {
  const original = globalThis.fetch
  vi.stubGlobal('fetch', vi.fn(async (input: unknown, init?: RequestInit) => {
    if (String(input).endsWith('/api/product/nickname')) {
      return new Response(JSON.stringify({ code: 'internal_error' }), { status: 500, headers: { 'Content-Type': 'application/json' } })
    }
    return original(input as RequestInfo, init)
  }))
  openSettingsAt('account'); open()
  await vi.waitFor(() => expect(activePage()).toBe('账号'))
  editNickname('新昵称')
  nicknameButton('保存').click()
  await vi.waitFor(() => expect(target.querySelector('.account-error')).not.toBeNull())
  expect(nicknameEditor()?.value).toBe('新昵称')
  expect(nicknameButton('保存').disabled).toBe(false)
})

it('keeps the wallet selected after its balance fetch and subsequent account refresh', async () => {
  settingsModalOpen.set(true); open()
  await vi.waitFor(() => expect(activePage()).toBe('常规'))
  const walletLink = [...target.querySelectorAll<HTMLButtonElement>('.rail button')].find(button => button.textContent?.includes('钱包与充值'))!
  walletLink.click(); flushSync()
  await vi.waitFor(() => expect(get(productState)?.credits.balance).toBe(200))
  expect(activePage()).toBe('钱包与充值')
  flushSync(() => productState.update(state => ({ ...state!, account: { ...state!.account!, nickname: '新昵称' } })))
  expect(activePage()).toBe('钱包与充值')
})

it('consumes a wallet shortcut only once and returns to general on a later plain open', async () => {
  openSettingsAt('wallet'); open()
  await vi.waitFor(() => expect(get(productState)?.credits.balance).toBe(200))
  expect(activePage()).toBe('钱包与充值')
  expect(get(settingsTarget)).toBeNull()
  flushSync(() => settingsModalOpen.set(false))
  flushSync(() => settingsModalOpen.set(true))
  await vi.waitFor(() => expect(activePage()).toBe('常规'))
})

it('shows a compact feedback form with optional details and sends the visible fields', async () => {
  const original = globalThis.fetch
  let sent: Record<string, unknown> | undefined
  vi.stubGlobal('fetch', vi.fn(async (input: unknown, init?: RequestInit) => {
    if (String(input).endsWith('/feedback')) { sent = JSON.parse(String(init?.body)); return json({ feedbackId: 'receipt-visible' }) }
    return original(input as RequestInfo, init)
  }))
  openSettingsAt('help'); open()
  await vi.waitFor(() => expect(target.querySelector('.center-tabs')).not.toBeNull())
  const tab = [...target.querySelectorAll<HTMLButtonElement>('.center-tabs button')].find(button => button.textContent?.includes('反馈'))!
  tab.click(); flushSync()
  const submit = target.querySelector<HTMLButtonElement>('.feedback-submit')!
  expect(submit.disabled).toBe(true)
  expect(target.querySelector<HTMLDetailsElement>('.feedback-optional')?.open).toBe(false)
  const title = target.querySelector<HTMLInputElement>('.feedback-form input')!
  const content = target.querySelector<HTMLTextAreaElement>('.feedback-content')!
  expect(title.placeholder).toContain('一句话')
  title.value = '反馈标题'; title.dispatchEvent(new Event('input', { bubbles: true }))
  content.value = '具体反馈内容'; content.dispatchEvent(new Event('input', { bubbles: true })); flushSync()
  expect(target.textContent).toContain('4 / 120 字')
  expect(submit.disabled).toBe(false); submit.click()
  await vi.waitFor(() => expect(target.querySelector('[role="status"]')?.textContent).toContain('receipt-visible'))
  expect(sent).toMatchObject({ title: '反馈标题', content: '具体反馈内容', reproduction: '', expected: '', contact: '' })
})

it('keeps the official help center slot visible but disabled until its brand URL is configured', async () => {
  // OCTO-FORK: exercise the unconfigured state independently of the shipped URL.
  const brandLink = branding.brandLink
  vi.spyOn(branding, 'brandLink').mockImplementation((group, key) =>
    group === 'external' && key === 'helpCenter' ? '' : brandLink(group, key))
  openSettingsAt('help'); open()
  await vi.waitFor(() => expect(target.querySelector('.help-portal-card')).not.toBeNull())
  expect(target.querySelector('.rail .scat[aria-current="page"]')?.textContent).toContain('帮助与反馈')
  expect(target.querySelector('.help-portal-card')?.textContent).toContain('官网帮助中心')
  expect(target.querySelector<HTMLButtonElement>('.help-portal-action')?.disabled).toBe(true)
  expect(target.querySelector('.help-portal-status')?.textContent).toContain('待配置')
})

// OCTO-FORK: configured help links must stay enabled and open their brand destination.
it('opens the configured official help center', async () => {
  const destination = 'https://help.example.test/'
  const brandLink = branding.brandLink
  vi.spyOn(branding, 'brandLink').mockImplementation((group, key) =>
    group === 'external' && key === 'helpCenter' ? destination : brandLink(group, key))
  const openUrl = vi.spyOn(externalLinks, 'openUrl').mockImplementation(() => {})
  openSettingsAt('help'); open()
  await vi.waitFor(() => expect(target.querySelector('.help-portal-card')).not.toBeNull())
  const button = target.querySelector<HTMLButtonElement>('.help-portal-action')!
  expect(button.disabled).toBe(false)
  expect(target.querySelector('.help-portal-status')).toBeNull()
  button.click()
  expect(openUrl).toHaveBeenCalledWith(destination)
})

it('opens only one FAQ item at a time', async () => {
  openSettingsAt('help'); open()
  await vi.waitFor(() => expect(target.querySelectorAll('.help-faq-item')).toHaveLength(5))
  const items = [...target.querySelectorAll<HTMLElement>('.help-faq-item')]
  expect(items[0].classList.contains('open')).toBe(true)
  items[1].querySelector<HTMLButtonElement>('.help-faq-trigger')?.click()
  flushSync()
  expect(items[0].classList.contains('open')).toBe(false)
  expect(items[1].classList.contains('open')).toBe(true)
  expect(target.querySelectorAll('.help-faq-item.open')).toHaveLength(1)
})

// OCTO-FORK: the desktop Mobile page owns the one-click tunnel lifecycle and
// shows the existing pairing QR only after the relay reports connected.
it('starts and stops the desktop phone tunnel from Settings', async () => {
  nativeShell.set(true)
  let state = 'off'
  const original = globalThis.fetch
  vi.stubGlobal('fetch', vi.fn(async (input: unknown, init?: RequestInit) => {
    const path = String(input).split('?')[0]
    if (path.endsWith('/api/product/tunnel/start')) { state = 'connected'; return json({ state }) }
    if (path.endsWith('/api/product/tunnel/stop')) { state = 'off'; return json({ state }) }
    if (path.endsWith('/api/product/tunnel')) return json({ state })
    if (path.endsWith('/api/tunnel/pairing')) return json({ enabled: state === 'connected', pair_url: 'octo-pair://v1?tok=test', relay: 'wss://relay.example.com', tunnel_id: 'test-id' })
    return original(input as RequestInfo, init)
  }))
  openSettingsAt('mobile'); open()
  const control = () => target.querySelector<HTMLButtonElement>('.mobile-control button')
  await vi.waitFor(() => expect(control()?.textContent).toContain('启动连接'))
  expect(target.querySelector('.mobile-pair')).toBeNull()
  control()!.click()
  await vi.waitFor(() => expect(target.querySelector('.mobile-pair')).not.toBeNull())
  await vi.waitFor(() => expect(target.querySelector<HTMLImageElement>('.mobile-pair img.qr')?.src).toMatch(/^data:image\/png;base64,/))
  expect(target.querySelector('.mobile-status')?.textContent).toContain('已连接')
  expect(control()?.textContent).toContain('关闭连接')
  control()!.click()
  await vi.waitFor(() => expect(control()?.textContent).toContain('启动连接'))
  expect(target.querySelector('.mobile-pair')).toBeNull()
  const paths = vi.mocked(globalThis.fetch).mock.calls.map(([input]) => String(input))
  expect(paths).toContain('/api/product/tunnel/start')
  expect(paths).toContain('/api/product/tunnel/stop')
})

it('shows a relay error without exposing a stale pairing QR', async () => {
  nativeShell.set(true)
  const original = globalThis.fetch
  vi.stubGlobal('fetch', vi.fn(async (input: unknown, init?: RequestInit) => {
    const path = String(input).split('?')[0]
    if (path.endsWith('/api/product/tunnel')) return json({ state: 'retrying', error: 'relay_unavailable' })
    if (path.endsWith('/api/tunnel/pairing')) throw new Error('pairing should be hidden while retrying')
    return original(input as RequestInfo, init)
  }))
  openSettingsAt('mobile'); open()
  await vi.waitFor(() => expect(target.querySelector('.mobile-error')?.textContent).toMatch(/中继连接失败|Relay connection failed/))
  expect(target.querySelector('.mobile-status')?.textContent).toMatch(/正在重试|retrying/)
  expect(target.querySelector('.mobile-pair')).toBeNull()
})
