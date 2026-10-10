// OCTO-FORK: first-send regression exercises the real landing-to-session render.
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { get, writable } from 'svelte/store'
import { activeSessionId, chatMessages, chatStreaming, pendingPrompt, sessions, pendingModel, pendingPermissionMode } from '../lib/stores'
import { allowEnvironmentModelSource, productState } from '../lib/product'
import { locale } from '../lib/i18n'
import ChatView from './ChatView.svelte'
import * as api from '../lib/api'
import { ws } from '../lib/ws'

const handlers = vi.hoisted(() => new Map<string, Set<(event: any) => void>>())
vi.mock('../lib/ws', () => ({
  wsState: writable('connected'), wsReconnect: writable(null),
  ws: {
    connected: writable(true),
    on: vi.fn((type, handler) => {
      if (!handlers.has(type)) handlers.set(type, new Set())
      handlers.get(type)!.add(handler)
      return () => handlers.get(type)!.delete(handler)
    }),
    subscribe: vi.fn(), unsubscribe: vi.fn(), sendMessage: vi.fn(), send: vi.fn(), interrupt: vi.fn(),
  },
}))
vi.mock('../lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof api>(),
  createSession: vi.fn(async () => ({ id: 'new-session', model: 'chosen', model_id: 'local::chosen' })),
  updateSessionPermissionMode: vi.fn(async () => ({})),
  listAgents: vi.fn(async () => []), listSkills: vi.fn(async () => []),
  listWorkflows: vi.fn(async () => []), listMcpServers: vi.fn(async () => ({ servers: [] })),
  getSessionMessages: vi.fn(async () => ({ events: [] })),
  getProductModels: vi.fn(async () => ({ state: 'absent', vendors: [] })),
  getEndpoints: vi.fn(async () => ({ endpoints: [] })),
  transformPersonalInfo: vi.fn(async (text) => ({ hit: false, masked: text, matches: [] })),
}))
vi.mock('../lib/sensitive', () => ({ checkSensitive: vi.fn(async () => ({ hit: false })) }))

let app: ReturnType<typeof mount>
let target: HTMLElement
const settle = async () => { await new Promise(resolve => setTimeout(resolve, 0)); flushSync() }
function emit(type: string, event: any) { for (const handler of handlers.get(type) ?? []) handler(event) }

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 200 })))
  activeSessionId.set(null); sessions.set([]); chatMessages.set({}); chatStreaming.set({}); pendingPrompt.set(null)
  pendingModel.set('local::chosen'); allowEnvironmentModelSource.set(true); productState.set(null); locale.set('en')
  pendingPermissionMode.set('')
  vi.clearAllMocks()
  target = document.createElement('div'); document.body.appendChild(target)
})
afterEach(() => { if (app) unmount(app); target.remove(); handlers.clear(); vi.unstubAllGlobals() })

it('renders the first message without leaving and reopening the new session', async () => {
  app = mount(ChatView, { target }); flushSync(); await settle()
  const input = target.querySelector('textarea')!
  input.value = 'hello'; input.dispatchEvent(new Event('input', { bubbles: true })); flushSync()
  target.querySelector<HTMLButtonElement>('button.send-btn')!.click()
  await settle(); await settle()
  expect(api.createSession).toHaveBeenCalledWith(expect.objectContaining({ model: 'local::chosen' }))
  expect(get(activeSessionId)).toBe('new-session')
  emit('subscribed', { session_id: 'new-session' }); await settle()
  expect(ws.sendMessage).toHaveBeenCalledWith('new-session', 'hello', undefined, false, false)
  emit('history_user_message', { session_id: 'new-session', content: 'hello', message_index: 0 })
  await settle()
  expect(target.querySelector('.chat-view')).toBeTruthy()
  expect(target.textContent).toContain('hello')
})

it('queues the first message before subscribing even when permission persistence is slow', async () => {
  let finishPermission!: () => void
  vi.mocked(api.updateSessionPermissionMode).mockImplementationOnce(() => new Promise(resolve => {
    finishPermission = () => resolve({} as never)
  }))
  pendingPermissionMode.set('auto')
  vi.mocked(ws.subscribe).mockImplementationOnce((sid) => queueMicrotask(() => emit('subscribed', { session_id: sid })))
  app = mount(ChatView, { target }); flushSync(); await settle()
  const input = target.querySelector('textarea')!
  input.value = 'first message'; input.dispatchEvent(new Event('input', { bubbles: true })); flushSync()
  target.querySelector<HTMLButtonElement>('button.send-btn')!.click()
  await settle(); await settle()
  finishPermission()
  await settle(); await settle()
  expect(ws.sendMessage).toHaveBeenCalledWith('new-session', 'first message', undefined, false, false)
  expect(target.textContent).toContain('first message')
})

it('retains the input and the created session without sending when permission persistence fails', async () => {
  vi.mocked(api.updateSessionPermissionMode).mockRejectedValueOnce(new Error('permission failed'))
  pendingPermissionMode.set('auto')
  app = mount(ChatView, { target }); flushSync(); await settle()
  const input = target.querySelector('textarea')!
  input.value = 'keep this draft'; input.dispatchEvent(new Event('input', { bubbles: true })); flushSync()
  target.querySelector<HTMLButtonElement>('button.send-btn')!.click()
  await settle(); await settle()
  expect(get(activeSessionId)).toBe('new-session')
  expect(get(pendingPrompt)).toBeNull()
  expect(ws.sendMessage).not.toHaveBeenCalled()
  expect(input.value).toBe('keep this draft')
})
