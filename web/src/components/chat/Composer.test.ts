import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { get } from 'svelte/store'
import { locale } from '../../lib/i18n'
import { allowEnvironmentModelSource, productState } from '../../lib/product'
import {
  activeSessionId,
  pendingConfidentialSession,
  pendingModel,
  pendingPersonalInfoProtection,
  sessions,
  toasts,
} from '../../lib/stores'
import { checkSensitive } from '../../lib/sensitive'
import { getEndpoints, getProductModels, transformPersonalInfo } from '../../lib/api'
import { parkDraft, takeDraft } from '../../lib/composerDrafts'
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
  getProductModels: vi.fn(async () => ({ state: 'absent', catalogVersion: '', vendors: [] })),
  getEndpoints: vi.fn(async () => ({ endpoints: [] })),
  setSessionProtection: vi.fn(),
  updateSessionModel: vi.fn(),
  transformPersonalInfo: vi.fn(async (text: string) => ({ hit: false, masked: text, matches: [], ruleVersion: 'builtin-1' })),
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
    prefs: { locale: 'zh', inputSensitiveCheck: true },
    suppressOnboarding: true,
  } as never)
}

function render(onSend?: (text: string, files?: any[], queued?: boolean) => void) {
  app = mount(Composer, { target, props: { onSend } }) as Record<string, unknown>
  flushSync()
}

function noticeText(): string {
  return target.querySelector('[data-composer-notices]')?.textContent ?? ''
}

async function settle() {
  await new Promise(r => setTimeout(r, 0))
  await new Promise(r => setTimeout(r, 0))
  flushSync()
}

function openSecurityMenu() {
  const button = [...target.querySelectorAll('button.meta-chip')]
    .find(node => node.textContent?.includes('安全与隐私') || node.textContent?.includes('私密会话')) as HTMLButtonElement
  expect(button).toBeTruthy()
  button.click()
  flushSync()
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
  await settle()
}

beforeEach(() => {
  locale.set('zh')
  activeSessionId.set('s1')
  sessions.set([])
  pendingModel.set('')
  pendingPersonalInfoProtection.set(true)
  pendingConfidentialSession.set(false)
  allowEnvironmentModelSource.set(true)
  toasts.set([])
  takeDraft('s1')
  vi.mocked(checkSensitive).mockResolvedValue({ hit: false, masked: '' } as never)
  vi.mocked(transformPersonalInfo).mockImplementation(async (text: string) => ({ hit: false, masked: text, matches: [], ruleVersion: 'builtin-1' }))
  vi.mocked(getProductModels).mockResolvedValue({ state: 'absent', catalogVersion: '', vendors: [] })
  vi.mocked(getEndpoints).mockResolvedValue({ endpoints: [] })
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  productState.set(null)
  activeSessionId.set(null)
  sessions.set([])
  pendingModel.set('')
  pendingPersonalInfoProtection.set(true)
  pendingConfidentialSession.set(false)
  toasts.set([])
  takeDraft('s1')
  sessionStorage.clear()
  vi.unstubAllGlobals()
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

describe('personal information preview', () => {
  it('sends only the masked text and reports aggregate categories', async () => {
    setBalance(1)
    vi.mocked(transformPersonalInfo).mockResolvedValue({
      hit: true,
      masked: '电话 [PHONE]',
      matches: [{ category: 'cn_mobile', count: 1 }],
      ruleVersion: 'builtin-1',
    })
    const onSend = vi.fn()
    render(onSend)

    await sendWord('电话 13800138000')

    expect(onSend).toHaveBeenCalledWith('电话 [PHONE]', undefined, false)
    expect(JSON.stringify(onSend.mock.calls)).not.toContain('13800138000')
    expect((target.querySelector('textarea') as HTMLTextAreaElement).value).toBe('')
    expect(target.textContent).not.toContain('13800138000')
    expect(noticeText()).toContain('已自动脱敏 1 处个人信息')
  })

  it('keeps the draft and sends nothing when the preview fails', async () => {
    setBalance(1)
    vi.mocked(transformPersonalInfo).mockRejectedValue(new Error('preview unavailable'))
    const onSend = vi.fn()
    render(onSend)

    await sendWord('电话 13800138000')

    expect(onSend).not.toHaveBeenCalled()
    expect((target.querySelector('textarea') as HTMLTextAreaElement).value).toContain('13800138000')
  })
})

// OCTO-FORK: the picker must refresh catalog availability through the local
// runtime owner before it reads the downstream model-cache projection.
describe('catalog refresh before model projection', () => {
  it('checks the catalog at mount and when opening the model picker', async () => {
    // A desktop token makes refreshCatalogState use the local availability
    // endpoint. The endpoint itself decides whether a central refresh is
    // necessary; a ready answer here represents the no-network-cache case.
    sessionStorage.setItem('octo_window_token', 'catalog-token')
    const catalogFetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      statusText: '',
      json: async () => ({ state: 'ready', retryable: false }),
    }))
    vi.stubGlobal('fetch', catalogFetch)

    render()
    await settle()
    expect(catalogFetch).toHaveBeenCalledTimes(1)
    expect(catalogFetch).toHaveBeenCalledWith('/api/product/catalog', expect.objectContaining({ cache: 'no-store' }))

    const picker = target.querySelector('iconify-icon[icon="ant-design:robot-outlined"]')?.closest('button') as HTMLButtonElement
    expect(picker).toBeTruthy()
    picker.click()
    await settle()

    expect(catalogFetch).toHaveBeenCalledTimes(2)
  })
})

// OCTO-FORK: pin the unified safety/privacy panel and development-profile
// private-model UX required by the downstream privacy design.
describe('safety and privacy controls', () => {
  it('shows three independent protections without a master switch', () => {
    setBalance(1)
    render()
    openSecurityMenu()

    const panel = target.querySelector('.security-menu') as HTMLElement
    expect(panel.textContent).toContain('敏感词检测')
    expect(panel.textContent).toContain('个人信息保护')
    expect(panel.textContent).toContain('私密会话')
    expect(panel.textContent).toContain('输出与昵称保护')
    expect(panel.textContent).toContain('始终开启')
    expect(panel.querySelectorAll('button.security-row')).toHaveLength(2)
  })

  it('renders the server lock as disabled session controls', () => {
    setBalance(1)
    sessions.set([{
      id: 's1',
      model: 'private-chat',
      model_id: 'local::private-chat',
      turn_count: 1,
      protection_policy: {
        version: 1,
        personal_info_protection: true,
        confidential_session: true,
        locked: true,
      },
    }] as never)
    render()
    openSecurityMenu()

    const controls = [...target.querySelectorAll('button.security-row')] as HTMLButtonElement[]
    expect(controls).toHaveLength(2)
    expect(controls.every(control => control.disabled)).toBe(true)
    expect(target.querySelector('.security-menu')?.textContent).toContain('首条消息被接受后已锁定')
  })

  it('warns that attachment contents are outside automatic masking', () => {
    setBalance(1)
    parkDraft('s1', '', [{ name: 'contract.pdf', path: '/api/uploads/contract.pdf' }])
    render()

    expect(noticeText()).toContain('本期暂不自动脱敏附件内容')
  })

  it('auto-selects a private local model when a new private session is enabled', async () => {
    activeSessionId.set(null)
    setBalance(1)
    vi.mocked(getEndpoints).mockResolvedValue({
      endpoints: [{
        id: 'local',
        name: 'Local Lab',
        provider: 'custom',
        base_url: 'http://127.0.0.1:9000/v1',
        has_api_key: false,
        models: [
          { model: 'public-chat', vision: false, confidential: false },
          { model: 'private-chat', vision: false, confidential: true },
        ],
      }],
      default: 'local::public-chat',
    } as never)
    render()
    await settle()
    openSecurityMenu()

    const privateControl = [...target.querySelectorAll('button.security-row')]
      .find(node => node.textContent?.includes('私密会话')) as HTMLButtonElement
    privateControl.click()
    await settle()

    expect(get(pendingConfidentialSession)).toBe(true)
    expect(get(pendingModel)).toBe('local::private-chat')
    expect(get(toasts).at(-1)?.msg).toContain('已切换到私密模型：private-chat')
  })
})
