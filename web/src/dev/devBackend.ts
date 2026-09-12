// devBackend.ts — TEMPORARY development stand-in for the product backend.
//
// ============================================================================
// THIS FILE IS A STOPGAP AND WILL BE DELETED. The real backend replaces it.
// It exists for one reason: so the P1-P13 screens can be clicked through on
// this branch, where that backend is not present. It is not a design, and no
// implementation detail in it should be carried into the real code.
// See dev-docs-usdable/需求/2260906/技术方案/开发期假后端说明.md.
// ============================================================================
//
// WHY IT IS NEEDED
//   The screens call /api/product/*, served by internal/server plus the
//   packages internal/productgate / productstate / sensitive / chatmode. That
//   code is on the 20260909 line, NOT on this branch. So without a responder:
//     - GET /api/product/state 404s, refreshProductState() falls back to
//       "blocked", and
//     - send-code / login 404, so the user never gets past the login form.
//   In short: with nothing answering, the flow cannot be walked at all.
//
// WHAT REPLACES IT
//   The real handlers (P3/P4 login, P5 account panel, P8 sensitive gate, P9
//   chat modes, P13 dictionary). When they land, DELETE THIS FILE and the two
//   lines in src/main.ts that install it — nothing else references it
//   (`rg devBackend web/` proves it).
//
// WHAT IT DELIBERATELY DOES NOT DO
//   Field validation, phone binding/masking, dictionary de-duplication, real
//   persistence, and the WebSocket event stream. Those are the real backend's
//   job; reproducing them here would create a SECOND definition of the contract
//   that drifts from the first (开发规范 §3.8). The only credentials it accepts
//   are the demo pins in 需求20260906 §5.3.3 / §9, so the error paths stay
//   walkable.
//
// SCOPE (开发规范 §3.10): DEVELOPMENT, browser-side, run-time only. main.ts
// installs it behind `import.meta.env.DEV`, so a production build never runs it
// and tree-shakes it out of the bundle (verified against the built output).

import { chatModes } from '../lib/chatMode'
import { productPhase, productState } from '../lib/product'
import { accountPanelOpen, accountPanelPage, chatMode, frozen } from '../lib/stores'

/** The credentials the demo build pins (需求20260906 §5.3.3 / §9). */
export const DEMO_SMS_CODE = '123456'
export const DEMO_ACTIVATION_CODE = 'BUDING-DEMO-0001'
export const DEMO_BOX_CODE = 'BOX-DEMO-0001'
/** lib/product.ts's sessionStorage key — a token is what makes the gate active. */
const TOKEN_KEY = 'octo_window_token'
/** Built-in words the nickname check and the composer's input gate run against (P7/P8). */
const BUILTIN_WORDS = ['赌博', '毒品', '发票', 'gambling', 'drugs', 'invoice']

let loggedIn = false
let dict = { builtin: [...BUILTIN_WORDS], user: ['内部项目', '测试词'] }

// One session so the chat pane renders a conversation instead of the landing,
// and createSession has the record shape to return (the create endpoint wraps
// it as { session } — api.ts unwraps it for a usable .id).
let seq = 0
function newSession() {
  const now = new Date().toISOString()
  return {
    id: `demo-${++seq}`, name: '演示会话', title: '演示会话',
    created_at: now, updated_at: now,
    model: 'buding-cloud-plus', model_id: DEFAULT_MODEL, status: 'idle', source: 'manual',
    agent_profile: '', pinned: false, total_tasks: 0, turn_count: 0, working_dir: '/tmp',
    permission_mode: 'interactive', chat_mode: 'default', reasoning_effort: 'medium',
    show_reasoning: true, context_usage: 0,
  }
}

// The configured endpoints. These are the Settings → Endpoints list; they no
// longer have to agree with the mode menu below. Until PR-4d a row was only
// selectable once it resolved to a composite "<endpoint>::<model>" id against
// this list (P9 §3.2), and the menu's ids had to mirror it. The menu now reads
// the catalog projection, whose composite ids carry the gateway prefix, so the
// two lists answer different questions and are free to differ.
const ENDPOINTS = [
  {
    id: 'cloud', name: 'Cloud', protocol: 'anthropic-messages', base_url: 'https://example.invalid',
    models: [{ model: 'buding-cloud-plus' }, { model: 'buding-cloud-pro' }],
  },
  {
    id: 'local', name: 'Local', protocol: 'openai', base_url: 'http://127.0.0.1:1234',
    models: [{ model: 'buding-local-general' }, { model: 'buding-local-fast' }],
  },
]
const DEFAULT_MODEL = 'cloud::buding-cloud-plus'

// GATEWAY_ENDPOINT_ID mirrors internal/productprofile.GatewayEndpointID, which
// the real projection prefixes every composite id with. It is duplicated rather
// than imported because this file is a frontend stand-in for a backend that does
// not exist in this build, so there is no Go value to import — and the whole
// file is deleted by PR-3, which is also why the duplication is not worth a
// shared constant.
//
// A stand-in that kept emitting "local::"/"cloud::" would demo the shape the real
// backend no longer produces, which is what V-24 caught and this line prevents:
// the point of the DEV backend is to show the contract, not a previous one.
const GATEWAY_ENDPOINT_ID = 'buding-gateway'

// 需求 §5.6.3: privacy holds only local models, smart only cloud ones.
//
// Shaped as the real endpoint answers (本地API契约 §2.8): displayName comes from
// the catalog and there is no `fallback` field. The four names below used to be
// i18n keys `model.<id>` (中英各一份); PR-4d deleted them because a local name
// table shadows the server's copy (需求基线 B6) — so the names live here, in the
// data, exactly where the platform's would.
//
// 需求 §5.6.3 的分组在这里是演示数据。真实分组由中台目录的
// models[].modeIds 决定（中台交付包 §4.3），前端不再推导。
const MODES = [
  {
    id: 'privacy',
    models: [
      named('buding-local-general', '本地通用', 'Local General'),
      named('buding-local-fast', '本地极速', 'Local Fast'),
    ],
    defaultModel: `${GATEWAY_ENDPOINT_ID}::buding-local-general`,
  },
  {
    id: 'smart',
    models: [
      named('buding-cloud-plus', '云端智能', 'Cloud Plus'),
      named('buding-cloud-pro', '云端旗舰', 'Cloud Pro'),
    ],
    defaultModel: `${GATEWAY_ENDPOINT_ID}::buding-cloud-plus`,
  },
  {
    id: 'default',
    models: [
      named('buding-cloud-plus', '云端智能', 'Cloud Plus'),
      named('buding-local-general', '本地通用', 'Local General'),
    ],
    defaultModel: `${GATEWAY_ENDPOINT_ID}::buding-cloud-plus`,
  },
]

/** One menu row in the shape the real projection produces. */
function named(id: string, zh: string, en: string) {
  return { id, displayName: { zh, en }, compositeId: `${GATEWAY_ENDPOINT_ID}::${id}` }
}

const CATALOG_VERSION = '2026-09-11.1'
const POLICY_VERSION = '2026-09-11.1'

// Seeded after MODES/DEFAULT_MODEL: newSession() reads DEFAULT_MODEL, so this
// must not run earlier in module evaluation.
const sessions = [newSession()]

/** The de-identified state GET /api/product/state serves, pre- and post-login. */
function state() {
  return {
    schemaVersion: 1,
    loggedIn,
    activated: loggedIn,
    activation: loggedIn ? { activatedAt: '2026-01-01T00:00:00Z', expiresAt: '2027-01-01T00:00:00Z', boxCode: DEMO_BOX_CODE } : null,
    account: loggedIn ? { phoneMasked: '138****1234', nickname: '测试用户', lastLoginAt: new Date().toISOString() } : null,
    credits: { balance: 1280, monthUsed: 0, monthKey: '2026-09' },
    plan: { name: 'trial' },
    prefs: { locale: 'zh', inputSensitiveCheck: true, defaultChatMode: 'default' },
    // Desktop builds skip the first-run API-key wizard (P9 §3.1).
    suppressOnboarding: true,
  }
}

// WsManager opens /ws on boot; with no server that fails and retries on a
// backoff, leaving the UI under a "connection lost" banner. This opens cleanly
// and carries no events of its own — `__dev.push(e)` injects one, which is how
// the server-pushed surfaces (FrozenOverlay, credits_update) are reachable.
const sockets: StubSocket[] = []
class StubSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  readyState = 0
  onopen: ((e: unknown) => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: ((e: unknown) => void) | null = null
  onerror: ((e: unknown) => void) | null = null
  constructor() {
    sockets.push(this)
    setTimeout(() => {
      if (this.readyState !== 0) return
      this.readyState = 1
      this.onopen?.({})
    }, 0)
  }
  send(data?: unknown): void {
    // The app parks a brand-new session's first message in pendingPrompt and
    // only sends it once the server acks the subscribe (ChatView's
    // flush-on-subscribe). Without this ack that message never leaves, so the
    // first thing typed in a new session silently does nothing.
    try {
      const msg = JSON.parse(String(data ?? ''))
      if (msg?.type === 'subscribe' && msg.session_id) {
        const sid = String(msg.session_id)
        setTimeout(() => this.onmessage?.({ data: JSON.stringify({ type: 'subscribed', session_id: sid }) }), 0)
      }
    } catch {
      /* non-JSON frame: dropped either way */
    }
  }
  close(): void {
    this.readyState = 3
    setTimeout(() => this.onclose?.({ code: 1000 }), 0)
  }
  static push(event: unknown): void {
    for (const s of sockets) if (s.readyState === 1) s.onmessage?.({ data: JSON.stringify(event) })
  }
}

const realFetch = globalThis.fetch.bind(globalThis)

/** Seeds the stores and replaces fetch/WebSocket with local stubs. */
export function installDevBackend(): void {
  // Without a token the product gate is inert and the app boots straight past
  // the login screen. Plant one so the desktop-shell path is the one exercised.
  sessionStorage.setItem(TOKEN_KEY, 'dev-window-token')

  productState.set(state())
  productPhase.set('blocked')
  chatModes.set(MODES)
  chatMode.set({})
  accountPanelOpen.set(false)
  accountPanelPage.set('root')
  frozen.set(false)

  const json = (payload: unknown, status = 200) =>
    new Response(JSON.stringify(payload), { status, headers: { 'Content-Type': 'application/json' } })
  const ok = () => ({ ok: true })

  // Reads. Anything absent falls through to `{}` so an unmocked read degrades
  // to "no data" instead of an unhandled rejection that blanks a pane.
  const GET: Record<string, () => unknown> = {
    '/api/product/state': state,
    '/api/product/chat-modes': () => ({ modes: MODES, catalogVersion: CATALOG_VERSION, policyVersion: POLICY_VERSION }),
    '/api/product/sensitive/dict': () => dict,
    '/api/onboard/status': () => ({ needs_onboard: false, phase: '' }),
    '/api/config': () => ({
      language: 'zh', permission_mode: 'interactive', reasoning_effort: 'medium',
      show_reasoning: true, workspace_dir: '', workspace_dir_default: '/tmp',
    }),
    '/api/config/endpoints': () => ({ endpoints: ENDPOINTS, default: DEFAULT_MODEL }),
    '/api/sessions': () => ({ sessions, has_more: false, cron_count: 0 }),
    '/api/session-groups': () => ({ groups: [], pinned_session_ids: [], collapsed_session_ids: [] }),
    '/api/skills': () => ({ skills: [] }),
    '/api/workflows': () => ({ workflows: [] }),
    '/api/version': () => ({ version: 'dev' }),
    '/api/browser/status': () => ({ running: false }),
  }
  for (const p of ['/api/agents', '/api/mcp/servers', '/api/tasks', '/api/channels', '/api/light-apps', '/api/memories', '/api/trash', '/api/providers']) {
    GET[p] = () => []
  }

  // Writes. A handler may return a Response to fail; otherwise its value is
  // serialised with 200.
  const POST: Record<string, (b: any) => unknown> = {
    '/api/product/send-code': () => ({ cooldownSec: 60 }),
    '/api/product/login': (b) => {
      if (String(b.code ?? '') !== DEMO_SMS_CODE) return json({ code: 'invalid_code' }, 400)
      if (!loggedIn) {
        // Activation takes two independent credentials. The stand-in mirrors
        // the real server's code split so every failure branch stays walkable:
        // a wrong activation code, an unknown box code, a code/box mismatch,
        // and a code that was already spent (POLICY-1).
        const code = String(b.activationCode ?? '').trim().toLowerCase()
        const box = String(b.boxCode ?? '').trim().toLowerCase()
        if (code !== DEMO_ACTIVATION_CODE.toLowerCase()) return json({ code: 'activation_invalid' }, 400)
        if (box !== DEMO_BOX_CODE.toLowerCase()) {
          if (box === 'box-demo-mismatch') return json({ code: 'box_code_mismatch' }, 400)
          if (box === 'box-demo-used') return json({ code: 'activation_code_used' }, 400)
          return json({ code: 'box_code_unknown' }, 400)
        }
      }
      loggedIn = true
      const next = state()
      productState.set(next)
      productPhase.set('ready')
      return { state: next }
    },
    '/api/product/logout': () => {
      loggedIn = false
      productState.set(state())
      productPhase.set('blocked')
      return ok()
    },
    '/api/product/locale': () => ok(),
    '/api/product/nickname': () => ({ state: state() }),
    '/api/product/prefs': () => ({ state: state() }),
    '/api/product/sensitive/check': (b) => {
      const text = String(b.text ?? '')
      const word = [...dict.builtin, ...dict.user].find((w) => w && text.includes(w))
      return word ? { hit: true, masked: text.split(word).join('*'.repeat(word.length)) } : { hit: false, masked: '' }
    },
    '/api/product/sensitive/dict/import': (b) => ({ added: (b.words ?? []).length, skipped: 0 }),
    '/api/sessions': () => {
      const s = newSession()
      sessions.push(s)
      return { session: s }
    },
  }

  globalThis.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const path = raw.replace(/^https?:\/\/[^/]+/, '')
    if (!path.startsWith('/api')) return realFetch(input as RequestInfo, init)
    const method = (init?.method ?? 'GET').toUpperCase()
    const body = typeof init?.body === 'string' ? JSON.parse(init.body) : {}

    let hit: unknown
    if (method === 'GET') {
      hit = GET[path]?.()
    } else if (method === 'PUT' && path === '/api/product/sensitive/dict') {
      dict = { ...dict, user: body.user ?? [] }
      hit = { user: dict.user }
    } else if (path.endsWith('/chat-mode')) {
      hit = { ok: true, chat_mode: String(body.mode ?? '') }
    } else if (path.endsWith('/model')) {
      const id = String(body.model_id ?? body.model ?? '')
      hit = { model: id, model_id: id }
    } else {
      hit = POST[path]?.(body)
    }
    if (hit instanceof Response) return hit
    return json(hit ?? {})
  }

  globalThis.WebSocket = StubSocket as unknown as typeof WebSocket
  // Console handles for the surfaces that are server-pushed, not clickable.
  ;(globalThis as any).__dev = {
    push: (event: unknown) => StubSocket.push(event),
    codes: { sms: DEMO_SMS_CODE, activation: DEMO_ACTIVATION_CODE, box: DEMO_BOX_CODE },
  }
}
