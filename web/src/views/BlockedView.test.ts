import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { locale } from '../lib/i18n'
import { productState } from '../lib/product'
import BlockedView from './BlockedView.svelte'

// The activation form now carries two credentials, not one (需求基线 E1): the
// USB activation code and the box code, submitted together and validated
// independently by the server. The failure that matters is a form that quietly
// drops one of them — that is a one-shot code burned against the wrong box.

let target: HTMLElement
let app: Record<string, unknown> | null = null

function firstActivationState() {
  return {
    schemaVersion: 1,
    loggedIn: false,
    activated: false,
    credits: { balance: 0, monthUsed: 0, monthKey: '2026-09' },
    plan: { name: '' },
    prefs: { locale: 'zh', inputSensitiveCheck: true, defaultChatMode: 'default' },
    suppressOnboarding: true,
  }
}

beforeEach(() => {
  locale.set('zh')
  productState.set(firstActivationState() as never)
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  productState.set(null)
  locale.set('en')
  vi.unstubAllGlobals()
})

function render() {
  app = mount(BlockedView, { target }) as Record<string, unknown>
  flushSync()
}

function input(id: string): HTMLInputElement {
  const el = target.querySelector<HTMLInputElement>(`#${id}`)
  if (!el) throw new Error(`no #${id} input`)
  return el
}

/** bind:value only syncs through an input event, not a bare .value write. */
function type(id: string, value: string) {
  const el = input(id)
  el.value = value
  el.dispatchEvent(new Event('input', { bubbles: true }))
  flushSync()
}

function submit() {
  const form = target.querySelector('form')
  if (!form) throw new Error('no form')
  form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  flushSync()
}

describe('BlockedView first activation', () => {
  it('orders the five fields as the requirement pins them', () => {
    render()

    // 需求基线 E1: phone, code, activation code, box code (adjacent), nickname.
    // The nickname is deliberately last — it is the one field the user is
    // expected to edit, so it sits nearest the submit button.
    const ids = [...target.querySelectorAll<HTMLInputElement>('.field input')].map((el) => el.id)
    expect(ids).toEqual(['phone', 'code', 'activationCode', 'boxCode', 'nickname'])
  })

  it('hides both credentials on the second login', () => {
    productState.set({ ...firstActivationState(), loggedIn: true, activated: true } as never)
    render()

    expect(target.querySelector('#activationCode')).toBeNull()
    expect(target.querySelector('#boxCode')).toBeNull()
  })

  it('submits the trimmed box code alongside the activation code', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => ({
      ok: true,
      status: 200,
      json: async () => ({
        state: { ...firstActivationState(), loggedIn: true, activated: true },
      }),
    }))
    vi.stubGlobal('fetch', fetchMock)
    render()

    type('phone', '13800001234')
    type('code', '123456')
    type('activationCode', '  BUDING-DEMO-0001  ')
    type('boxCode', '  BOX-DEMO-0001  ')
    submit()
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled())

    const body = JSON.parse(String(fetchMock.mock.calls[0][1]?.body))
    expect(body.boxCode).toBe('BOX-DEMO-0001')
    expect(body.activationCode).toBe('BUDING-DEMO-0001')
  })

  it('refuses to submit without the box code', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    render()

    type('phone', '13800001234')
    type('code', '123456')
    type('activationCode', 'BUDING-DEMO-0001')
    submit()

    // Nothing leaves the browser: a missing box code is a round-one format
    // error, so it costs no request and cannot burn the activation code.
    expect(fetchMock).not.toHaveBeenCalled()
    expect(target.textContent).toContain('请输入盒子编号')
  })

  it('renders the copy for the exact activation failure code the server sent', async () => {
    const failWith = (code: string) => {
      vi.stubGlobal(
        'fetch',
        vi.fn(async () => ({ ok: false, status: 400, json: async () => ({ code }) })),
      )
    }

    for (const [code, copy] of [
      ['activation_code_used', '该激活码已被使用'],
      ['box_code_unknown', '盒子编号不存在'],
      ['box_code_mismatch', '激活码与盒子编号不匹配'],
    ] as const) {
      if (app) { unmount(app); app = null }
      failWith(code)
      render()

      type('phone', '13800001234')
      type('code', '123456')
      type('activationCode', 'BUDING-DEMO-0001')
      type('boxCode', 'BOX-DEMO-0001')
      submit()
      await vi.waitFor(() => expect(target.textContent).toContain(copy))

      // The three codes must not collapse into one shared message: the user
      // has to know which of the two credentials to re-check, and which one
      // only support can undo.
      expect(target.textContent).not.toContain('激活码不正确')
    }
  })
})
