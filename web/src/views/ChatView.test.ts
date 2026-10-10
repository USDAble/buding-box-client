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
afterEach(() => { if (app) unmount(app); target.remove(); handlers.clear(); vi.unstubAllGlobals(); vi.useRealTimers() })

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

// OCTO-FORK: reopening history must not display the page-open time on assistant headers.
it('shows each historical reply duration and leaves unmeasured legacy replies blank', async () => {
  locale.set('zh')
  activeSessionId.set('history')
  vi.mocked(api.getSessionMessages).mockResolvedValueOnce({ events: [
    { type: 'history_user_message', content: 'first', created_at: 1_700_000_000_000 },
    { type: 'assistant_message', content: 'first answer', duration_ms: 80_000, message_index: 1 },
    { type: 'history_user_message', content: 'second', created_at: 1_700_000_100_000 },
    { type: 'assistant_message', content: 'second answer', duration_ms: 3_900_000, message_index: 3 },
    { type: 'history_user_message', content: 'old question' },
    { type: 'assistant_message', content: 'old answer', message_index: 5 },
  ] })
  app = mount(ChatView, { target }); flushSync(); await settle(); await settle()
  const elapsed = [...target.querySelectorAll('.msg-meta .meta-time')].map(node => node.textContent).filter(text => text?.startsWith('耗时'))
  expect(elapsed).toEqual(['耗时 1分20秒', '耗时 1时5分'])
  expect(get(chatMessages).history.filter((message: any) => message.type === 'assistant').every((message: any) => !message.createdAt)).toBe(true)
})

it('uses live completion duration without replacing the previous turn or waiting for usage', async () => {
  activeSessionId.set('live')
  app = mount(ChatView, { target }); flushSync(); await settle(); await settle()
  for (const [content, duration] of [['first answer', 12_000], ['second answer', 80_000]] as const) {
    emit('history_user_message', { session_id: 'live', content: `question for ${content}` })
    emit('assistant_message', { session_id: 'live', content })
    emit('complete', { session_id: 'live', duration_ms: duration })
    await settle()
  }
  const elapsed = [...target.querySelectorAll('.msg-meta .meta-time')].map(node => node.textContent).filter(text => text?.startsWith('Took'))
  expect(elapsed).toEqual(['Took 12s', 'Took 1m 20s'])
})

// OCTO-FORK: the assistant header ticks during silence, text and tools, then freezes on completion.
it('refreshes live elapsed metadata every second without changing finished turns', async () => {
  locale.set('zh'); activeSessionId.set('live')
  vi.useFakeTimers(); vi.setSystemTime(new Date('2026-10-10T00:00:00Z'))
  app = mount(ChatView, { target }); flushSync()
  await vi.advanceTimersByTimeAsync(0); flushSync()
  emit('history_user_message', { session_id: 'live', content: 'previous question' })
  emit('assistant_message', { session_id: 'live', content: 'previous answer' })
  emit('complete', { session_id: 'live', duration_ms: 12_000 }); flushSync()
  emit('history_user_message', { session_id: 'live', content: 'new question' })
  emit('progress', { session_id: 'live', phase: 'active' }); flushSync()
  const elapsed = () => [...target.querySelectorAll('.msg-meta .meta-time')]
    .map(node => node.textContent).filter(text => text?.startsWith('耗时'))
  expect(elapsed()).toEqual(['耗时 12秒', '耗时 0秒'])
  await vi.advanceTimersByTimeAsync(1000); flushSync()
  expect(elapsed()).toEqual(['耗时 12秒', '耗时 1秒'])
  await vi.advanceTimersByTimeAsync(2000); flushSync()
  expect(elapsed()).toEqual(['耗时 12秒', '耗时 3秒'])
  emit('text_delta', { session_id: 'live', text: 'working answer' }); flushSync()
  expect(elapsed()).toEqual(['耗时 12秒', '耗时 3秒'])
  emit('tool_call', { session_id: 'live', tool_id: 't1', name: 'terminal', args: '{}' }); flushSync()
  await vi.advanceTimersByTimeAsync(1000); flushSync()
  expect(elapsed()).toEqual(['耗时 12秒', '耗时 4秒'])
  emit('complete', { session_id: 'live', duration_ms: 80_000 }); flushSync()
  await vi.advanceTimersByTimeAsync(3000); flushSync()
  expect(elapsed()).toEqual(['耗时 12秒', '耗时 1分20秒'])
})

// OCTO-FORK: live and replayed runtime notes are display-only translations that follow language changes.
it('localizes interrupted replies without rewriting the raw assistant content', async () => {
  locale.set('zh'); activeSessionId.set('live')
  app = mount(ChatView, { target }); flushSync(); await settle(); await settle()
  emit('history_user_message', { session_id: 'live', content: 'hello' })
  emit('assistant_message', { session_id: 'live', content: '[Interrupted by user.]' })
  flushSync()
  expect(target.textContent).toContain('[本轮已中断]')
  expect(get(chatMessages).live.at(-1).content).toBe('[Interrupted by user.]')
  locale.set('en'); flushSync()
  expect(target.textContent).toContain('[Turn interrupted]')
})

it('localizes incomplete runtime notes when reopening historical replies', async () => {
  locale.set('zh'); activeSessionId.set('history')
  const content = 'Partial reply\n\n[Reply interrupted by an error — the text above is incomplete.]'
  vi.mocked(api.getSessionMessages).mockResolvedValueOnce({ events: [
    { type: 'history_user_message', content: 'question' },
    { type: 'assistant_message', content },
  ] })
  app = mount(ChatView, { target }); flushSync(); await settle(); await settle()
  expect(target.textContent).toContain('[回复因错误中断，以上内容尚未完成。]')
  expect(get(chatMessages).history.at(-1).content).toBe(content)
})

it('localizes frontend-generated task outcomes and error headings', async () => {
  locale.set('zh'); activeSessionId.set('live')
  app = mount(ChatView, { target }); flushSync(); await settle(); await settle()
  emit('background_task_notice', { session_id: 'live', command: 'npm test', status: 'cancelled' })
  emit('loop_tick_notice', { session_id: 'live' })
  emit('sub_agent_notice', { session_id: 'live', agent_id: 'a1', status: 'warning' })
  emit('workflow_event', { session_id: 'live', run_id: 'wf1', kind: 'done', status: 'error' })
  emit('goal_notice', { session_id: 'live', kind: 'continue' })
  emit('turn_error', { session_id: 'live', code: 'internal_error', error: 'English server diagnostic' })
  flushSync()
  expect(target.textContent).toContain('后台进程 npm test 已取消')
  expect(target.textContent).toContain('定时循环已触发')
  expect(target.textContent).toContain('子代理 a1 未完成')
  expect(target.textContent).toContain('工作流 wf1 执行失败')
  expect(target.textContent).toContain('目标任务继续')
  expect(target.textContent).not.toContain('Error:')
  expect(target.querySelector('.notice-line strong')?.textContent).toBe('错误:')
})
