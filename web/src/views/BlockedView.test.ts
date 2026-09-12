import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { get } from 'svelte/store'
import { locale } from '../lib/i18n'
import { blockedPage, productPhase, productState } from '../lib/product'
import BlockedView from './BlockedView.svelte'

// The activation form now carries two credentials, not one (需求基线 E1): the
// USB activation code and the box code, submitted together and validated
// independently by the server. The failure that matters is a form that quietly
// drops one of them — that is a one-shot code burned against the wrong box.

let target: HTMLElement
let app: Record<string, unknown> | null = null

/** The page renders no form at all in the two misconfiguration cases, so the
 *  helper that would throw on a missing #id cannot be used there. */
function input_(id: string): HTMLInputElement | null {
  return target.querySelector<HTMLInputElement>(`#${id}`)
}

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
  productPhase.set('blocked')
  blockedPage.set('login')
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  productState.set(null)
  blockedPage.set('login')
  locale.set('en')
  vi.unstubAllGlobals()
})

function render() {
  app = mount(BlockedView, { target }) as Record<string, unknown>
  flushSync()
}

/** Renders the wall in a chosen blocked-page state (L-B2). */
function renderWith(page: 'login' | 'unconfigured' | 'no_keys') {
  blockedPage.set(page)
  render()
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

describe('BlockedView blocked-page selection (L-B2)', () => {
  // The two misconfiguration pages. Both must be reachable without typing, and
  // they must be TELLINGLY DIFFERENT: "we do not know where to call" and "we can
  // call but cannot verify anything" lead to different next steps, and one
  // shared "something is wrong" page would leave the user with no way to tell
  // which package to ask for (P4-拦截页 §2.1).
  it('shows the unconfigured page instead of the form', () => {
    renderWith('unconfigured')

    expect(target.textContent).toContain('这个版本还没有配置服务地址')
    // No form at all: a field the user can fill in would be a dead end, because
    // there is nowhere for the request to go.
    expect(target.querySelector('form')).toBeNull()
    expect(input_('phone')).toBeNull()
  })

  it('shows the no-keys page, worded as a different problem', () => {
    renderWith('no_keys')

    expect(target.textContent).toContain('无法验证服务下发的数据')
    expect(target.textContent).not.toContain('这个版本还没有配置服务地址')
  })

  it('names no host, no file and no technical term on either page', () => {
    // A1 rule 6: these are not things the user can fix, so showing them only
    // suggests a setting that does not exist (P4-拦截页 §2.1).
    for (const page of ['unconfigured', 'no_keys'] as const) {
      if (app) { unmount(app); app = null }
      renderWith(page)
      const text = target.textContent ?? ''
      for (const leak of ['apiHost', 'http://', 'https://', 'trustedKeyIDs', 'production.json', 'developer.json', 'ed25519']) {
        expect(text).not.toContain(leak)
      }
    }
  })

  it('still renders the login form when the page is not a misconfiguration', () => {
    renderWith('login')

    expect(input_('phone')).toBeTruthy()
    expect(target.textContent).not.toContain('这个版本还没有配置服务地址')
  })
})

describe('BlockedView control-plane failure tiers (L-B3)', () => {
  // Before this, a 503 from the platform produced an EMPTY error area: the code
  // travelled to fieldErrors.phone / formError, and neither switch had a case
  // for it. So the four tiers are asserted by their copy, not by a class name.
  function failLoginWith(status: number, code: string) {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status, json: async () => ({ code }) })))
  }

  function fillAndSubmit() {
    type('phone', '13800001234')
    type('code', '123456')
    type('activationCode', 'BUDING-DEMO-0001')
    type('boxCode', 'BOX-DEMO-0001')
    submit()
  }

  it('tells the user the network is unreachable and offers a retry', async () => {
    failLoginWith(503, 'network_unavailable')
    renderWith('login')
    fillAndSubmit()

    await vi.waitFor(() => expect(target.textContent).toContain('网络连不上'))
    // A retry affordance is the tier's whole point: this failure can heal.
    expect(target.querySelector('[data-testid="tier-retry"]')).toBeTruthy()
  })

  it('does not blame the phone number for a transport failure', async () => {
    failLoginWith(503, 'network_unavailable')
    renderWith('login')
    fillAndSubmit()

    await vi.waitFor(() => expect(target.textContent).toContain('网络连不上'))
    // The old bug filed this under the phone field, whose error switch has no
    // case for it: the user saw a blank message under a number that was fine.
    // Asserted on the field's own error slot, not on the field's text - the
    // label "手机号" is part of the field either way and says nothing about this.
    expect(target.querySelector('#phone')?.parentElement?.querySelector('.field-err')).toBeNull()
  })

  it('distinguishes an upstream fault from the user’s own network', async () => {
    failLoginWith(503, 'upstream_unavailable')
    renderWith('login')
    fillAndSubmit()

    await vi.waitFor(() => expect(target.textContent).toContain('服务暂时不可用'))
    expect(target.textContent).not.toContain('网络连不上')
    // Retryable, but for a different reason: the user cannot fix this one.
    expect(target.querySelector('[data-testid="tier-retry"]')).toBeTruthy()
  })

  it('reports a restricted account without offering a pointless retry', async () => {
    const fetchMock = vi.fn(async () => ({ ok: false, status: 403, json: async () => ({ code: 'account_restricted' }) }))
    vi.stubGlobal('fetch', fetchMock)
    renderWith('login')
    fillAndSubmit()

    await vi.waitFor(() => expect(target.textContent).toContain('账号已被限制'))
    // Not retryable, and NOT a lost session either: P4-拦截页 §3.1 keeps the
    // credential here, because clearing it would lock out an account that is
    // merely restricted and can be restored. Asserted as "no second call": the
    // only way this page could clear a credential is by calling logout, so the
    // call count is what actually pins the rule.
    expect(target.querySelector('[data-testid="tier-retry"]')).toBeNull()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('gives the restricted account one definite action and no false promise', async () => {
    // P4-拦截页 §3.3 requires this tier to end in something the user can
    // actually do. The customer-service channel is still undecided (V-4 /
    // TODO-06), so the action cannot be "contact support" - the copy must not
    // name a channel that does not exist, and the button must do something real.
    failLoginWith(403, 'account_restricted')
    renderWith('login')
    fillAndSubmit()

    await vi.waitFor(() => expect(target.textContent).toContain('账号已被限制'))
    expect(target.textContent).not.toContain('联系客服')

    const dismiss = target.querySelector<HTMLButtonElement>('[data-testid="tier-dismiss"]')
    expect(dismiss).toBeTruthy()
    dismiss?.click()
    flushSync()

    // Dismissing clears the banner rather than leaving the user staring at a
    // message with no way to close it.
    expect(target.textContent).not.toContain('账号已被限制')
  })

  it('shows a refused session as a return to the gate, not as an error', async () => {
    // P4-拦截页 §3.4: `unauthorized` is L-A6's path - the credential is already
    // cleared and the page is a plain login form. An error banner on top of it
    // would describe a state the user is no longer in.
    failLoginWith(401, 'unauthorized')
    renderWith('login')
    fillAndSubmit()

    await vi.waitFor(() => expect(get(productPhase)).toBe('blocked'))
    expect(target.querySelector('[data-testid="tier-retry"]')).toBeNull()
    expect(target.textContent).not.toContain('网络连不上')
  })
})
