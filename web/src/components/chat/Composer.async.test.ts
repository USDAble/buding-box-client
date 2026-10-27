// OCTO-FORK: delayed preflight/settings responses must stay with their originating draft.
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { get } from 'svelte/store'
import Composer from './Composer.svelte'
import { activeSessionId, sessions, chatModel, chatShowReasoning, pendingPersonalInfoProtection, nativeShell, sessionGroups, pendingGroupId, pendingWorkingDir } from '../../lib/stores'
import { productState, allowEnvironmentModelSource } from '../../lib/product'
import { selectableModels } from '../../lib/selectableModels'
import { parkDraft, takeDraft } from '../../lib/composerDrafts'
import { locale, tr } from '../../lib/i18n'
import { checkSensitive } from '../../lib/sensitive'
import { getProductModels, updateSessionModel, setSessionProtection, updateSessionShowReasoning, nativePickFolder, updateSessionGroup, fsList } from '../../lib/api'
import type { Session, SessionGroup } from '../../lib/types'

vi.mock('../../lib/api', () => ({
  DEFAULT_WEB_PERMISSION_MODE: 'auto',
  listEndpoints: vi.fn(async () => ({ endpoints: [] })),
  getEndpoints: vi.fn(async () => ({ endpoints: [] })),
  listSkills: vi.fn(async () => []), listAgents: vi.fn(async () => []),
  listWorkflows: vi.fn(async () => []), listMcpServers: vi.fn(async () => ({ servers: [] })),
  getProductModels: vi.fn(async () => ({ state: 'absent', catalogVersion: '', vendors: [] })),
  updateSessionModel: vi.fn(),
  setSessionProtection: vi.fn(), updateSessionShowReasoning: vi.fn(),
  nativePickFolder: vi.fn(), updateSessionGroup: vi.fn(), fsList: vi.fn(),
}))
vi.mock('../../lib/sensitive', () => ({ checkSensitive: vi.fn() }))
vi.mock('../../lib/ws', () => ({ ws: { on: vi.fn(() => () => {}), interrupt: vi.fn() } }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function session(id: string): Session {
  return {
    id, name: id, title: id, created_at: '', updated_at: '', model: 'first', model_id: 'gateway::first',
    status: 'idle', source: 'web', agent_profile: 'default', pinned: false, total_tasks: 0, turn_count: 0,
    working_dir: '', permission_mode: 'interactive', reasoning_effort: 'default', context_usage: 0,
    protection_policy: { version: 1, personal_info_protection: false, confidential_session: false, locked: false },
  }
}
let target: HTMLElement
let app: ReturnType<typeof mount> | undefined
async function settle() { await new Promise(r => setTimeout(r, 0)); flushSync() }
function type(value: string) {
  const box = target.querySelector('textarea')!
  box.value = value; box.dispatchEvent(new Event('input', { bubbles: true })); flushSync()
}
function send() { (target.querySelector('.send-btn') as HTMLButtonElement).click(); flushSync() }
beforeEach(() => {
  locale.set('zh'); activeSessionId.set('async-a'); productState.set(null)
  sessions.set([]); chatModel.set({}); chatShowReasoning.set({}); selectableModels.set([])
  nativeShell.set(false); sessionGroups.set([]); pendingGroupId.set(''); pendingWorkingDir.set('')
  vi.mocked(nativePickFolder).mockReset(); vi.mocked(updateSessionGroup).mockReset(); vi.mocked(fsList).mockReset()
  allowEnvironmentModelSource.set(true); pendingPersonalInfoProtection.set(false)
  for (const sid of ['async-a', 'async-b', '']) takeDraft(sid)
  vi.mocked(checkSensitive).mockReset()
  vi.mocked(checkSensitive).mockResolvedValue({ hit: false, masked: '' } as never)
  vi.mocked(getProductModels).mockResolvedValue({ state: 'absent', catalogVersion: '', vendors: [] })
  target = document.createElement('div'); document.body.appendChild(target)
})
afterEach(async () => {
  if (app) await unmount(app); app = undefined; target.remove()
  for (const sid of ['async-a', 'async-b', '']) takeDraft(sid)
  productState.set(null); activeSessionId.set(null)
  nativeShell.set(false); sessionGroups.set([]); pendingGroupId.set(''); pendingWorkingDir.set('')
})

it('keeps the text and attachments of both sessions when navigation interrupts preflight', async () => {
  const pending = deferred<any>(); vi.mocked(checkSensitive).mockReturnValue(pending.promise)
  const aFile = { name: 'a.txt', path: '/api/uploads/a.txt' }
  const bFile = { name: 'b.txt', path: '/api/uploads/b.txt' }
  parkDraft('async-a', 'A message', [aFile]); parkDraft('async-b', 'B message', [bFile])
  const onSend = vi.fn(); app = mount(Composer, { target, props: { onSend } }); flushSync()
  send(); activeSessionId.set('async-b'); flushSync()
  pending.resolve({ hit: false }); await settle()
  expect(onSend).not.toHaveBeenCalled()
  expect(target.querySelector('textarea')!.value).toBe('B message')
  expect(takeDraft('async-a')).toEqual({ text: 'A message', attachments: [aFile] })
  expect(target.textContent).toContain('b.txt')
})

it('does not overwrite newer typing with a delayed sensitive result', async () => {
  const pending = deferred<any>(); vi.mocked(checkSensitive).mockReturnValue(pending.promise)
  const onSend = vi.fn(); app = mount(Composer, { target, props: { onSend } }); flushSync()
  type('original'); send(); type('new draft')
  pending.resolve({ hit: true, masked: '***' }); await settle()
  expect(target.querySelector('textarea')!.value).toBe('new draft')
  expect(onSend).not.toHaveBeenCalled()
})

it('submits only once and restores both text and attachments after an unexpected send failure', async () => {
  const pending = deferred<void>()
  const file = { name: 'report.txt', path: '/api/uploads/report.txt' }
  parkDraft('async-a', 'deliver', [file])
  const onSend = vi.fn(() => pending.promise)
  app = mount(Composer, { target, props: { onSend } }); flushSync()
  send(); send(); await settle(); expect(onSend).toHaveBeenCalledTimes(1)
  pending.reject(new Error('offline')); await settle()
  expect(target.querySelector('textarea')!.value).toBe('deliver')
  expect(target.textContent).toContain('report.txt')
})

it('writes a delayed model response to its original session and blocks sends until it settles', async () => {
  allowEnvironmentModelSource.set(false)
  sessions.set([session('async-a'), session('async-b')])
  vi.mocked(getProductModels).mockResolvedValue({ state: 'ready', catalogVersion: 'async-test', vendors: [{ id: 'gateway', displayName: 'Gateway', models: [
    { id: 'first', displayName: 'First', compositeId: 'gateway::first', confidential: false },
    { id: 'chosen', displayName: 'Chosen', compositeId: 'gateway::chosen', confidential: false },
  ] }] })
  const pending = deferred<any>(); vi.mocked(updateSessionModel).mockReturnValue(pending.promise)
  const onSend = vi.fn(); app = mount(Composer, { target, props: { onSend } }); flushSync(); await settle()
  const chip = [...target.querySelectorAll<HTMLButtonElement>('button.meta-chip')].find(b => b.querySelector('.mono'))!
  chip.click(); flushSync(); await settle()
  ;[...target.querySelectorAll<HTMLButtonElement>('button.menu-item')].find(b => b.textContent?.trim() === 'Chosen')!.click()
  flushSync(); type('wait'); send(); await settle(); expect(onSend).not.toHaveBeenCalled()
  activeSessionId.set('async-b'); flushSync()
  pending.resolve({ model: 'chosen', model_id: 'gateway::chosen' }); await settle()
  expect(get(chatModel)['async-a']).toBe('chosen')
  expect(get(chatModel)['async-b']).toBeUndefined()
  expect(get(sessions).find(s => s.id === 'async-b')?.model).toBe('first')
})

it('applies delayed privacy and reasoning visibility responses only to the original session', async () => {
  sessions.set([session('async-a'), session('async-b')])
  const privacy = deferred<Awaited<ReturnType<typeof setSessionProtection>>>()
  vi.mocked(setSessionProtection).mockReturnValue(privacy.promise)
  const visibility = deferred<any>(); vi.mocked(updateSessionShowReasoning).mockReturnValue(visibility.promise)
  app = mount(Composer, { target }); flushSync(); await settle()
  const security = [...target.querySelectorAll<HTMLButtonElement>('button.meta-chip')].find(b => b.textContent?.includes('安全与隐私'))!
  security.click(); flushSync()
  ;(target.querySelector('button.security-row') as HTMLButtonElement).click(); flushSync()
  const reasonChip = [...target.querySelectorAll<HTMLButtonElement>('button.meta-chip')].find(b => b.title === tr('chat.reasoning_default_hint'))!
  expect(reasonChip).toBeTruthy(); reasonChip.click(); flushSync()
  ;(target.querySelector('.toggle-item') as HTMLButtonElement).click(); flushSync()
  activeSessionId.set('async-b'); flushSync()
  const protection_policy = { version: 1, personal_info_protection: true, confidential_session: false, locked: false }
  privacy.resolve({ ok: true, protection_policy, session: { ...session('async-a'), protection_policy }, model: 'private', model_id: 'local::private' })
  visibility.resolve({}); await settle()
  expect(get(sessions).find(s => s.id === 'async-a')?.protection_policy.personal_info_protection).toBe(true)
  expect(get(sessions).find(s => s.id === 'async-b')?.protection_policy.personal_info_protection).toBe(false)
  expect(get(chatModel)['async-a']).toBe('private')
  expect(get(chatModel)['async-b']).toBeUndefined()
  expect(get(chatShowReasoning)['async-a']).toBe(false)
  expect(get(chatShowReasoning)['async-b']).toBeUndefined()
})

it('parks a failed send after navigation without overwriting a newer original-session draft', async () => {
  const pending = deferred<void>()
  const onSend = vi.fn(() => pending.promise)
  app = mount(Composer, { target, props: { onSend } }); flushSync()
  type('original'); send(); await settle()
  type('newer A draft'); activeSessionId.set('async-b'); flushSync()
  pending.reject(new Error('cancelled after session switch')); await settle()
  expect(target.querySelector('textarea')!.value).toBe('')
  expect(takeDraft('async-a').text).toBe('newer A draft')
})

function project(id = 'project-a'): SessionGroup {
  return { id, name: id, session_ids: [], working_dir: `/old/${id}`, source_dirs: [], working_dir_locked: false }
}
function directoryChip() {
  return target.querySelector<HTMLButtonElement>(`button[aria-label="${tr('chat.dir_change')}"]`)!
}

// OCTO-FORK: an unsent draft can leave a project that already has history.
it.each([true, false, undefined])('reselects the draft directory regardless of project lock %s', async locked => {
  const group = { ...project(), working_dir_locked: locked, session_ids: ['async-b'] }
  activeSessionId.set(null); pendingGroupId.set(group.id)
  sessions.set([{ ...session('async-b'), protection_policy: { ...session('async-b').protection_policy, locked: true } }])
  sessionGroups.set([group]); nativeShell.set(true)
  vi.mocked(nativePickFolder).mockResolvedValue({ path: '/chosen', cancelled: false })
  app = mount(Composer, { target }); flushSync()
  type('draft stays here')
  expect(directoryChip().disabled).toBe(false)
  directoryChip().click(); await settle()
  expect(nativePickFolder).toHaveBeenCalledWith(group.working_dir)
  expect(updateSessionGroup).not.toHaveBeenCalled()
  expect(get(sessionGroups)).toEqual([group])
  expect(target.querySelector('.dir-bar')?.textContent).toContain('/chosen')
  expect(target.querySelector('textarea')!.value).toBe('draft stays here')
  expect(get(pendingGroupId)).toBe('')
  expect(get(pendingWorkingDir)).toBe('/chosen')
})

it('uses the browser folder picker from the project directory chip', async () => {
  const group = project(); activeSessionId.set(null); pendingGroupId.set(group.id); sessionGroups.set([group])
  vi.mocked(fsList).mockResolvedValue({ path: '/chosen', parent: '/', entries: [], truncated: false, is_this_pc: false })
  app = mount(Composer, { target }); flushSync()
  directoryChip().click(); await settle()
  const select = [...target.querySelectorAll<HTMLButtonElement>('button')].find(b => b.textContent?.trim() === tr('folder.select'))!
  expect(select.disabled).toBe(false); select.click(); await settle()
  expect(fsList).toHaveBeenCalledWith(group.working_dir)
  expect(updateSessionGroup).not.toHaveBeenCalled()
  expect(target.querySelector('[role="dialog"]')).toBeNull()
  expect(get(pendingWorkingDir)).toBe('/chosen')
  expect(get(pendingGroupId)).toBe('')
})

it('keeps the directory chip read-only for an existing conversation', async () => {
  const group = project(); group.session_ids = ['async-a']
  sessions.set([{ ...session('async-a'), protection_policy: { ...session('async-a').protection_policy, locked: true } }])
  sessionGroups.set([group]); nativeShell.set(true)
  app = mount(Composer, { target }); flushSync()
  expect(directoryChip().disabled).toBe(true); directoryChip().click(); await settle()
  expect(nativePickFolder).not.toHaveBeenCalled()
})

it('discards a delayed native directory selection after switching the draft project', async () => {
  const original = project(); const other = project('project-b')
  activeSessionId.set(null); pendingGroupId.set(original.id); sessionGroups.set([original, other]); nativeShell.set(true)
  const pending = deferred<Awaited<ReturnType<typeof nativePickFolder>>>()
  vi.mocked(nativePickFolder).mockReturnValue(pending.promise)
  app = mount(Composer, { target }); flushSync(); directoryChip().click(); flushSync()
  pendingGroupId.set(other.id); flushSync()
  pending.resolve({ path: '/chosen', cancelled: false }); await settle()
  expect(updateSessionGroup).not.toHaveBeenCalled()
  expect(get(pendingGroupId)).toBe(other.id)
  expect(directoryChip().textContent).toContain('project-b')
  expect(get(sessionGroups).find(g => g.id === other.id)?.working_dir).toBe(other.working_dir)
})

it('selects an existing project without retargeting either project directory', async () => {
  const original = project(); const other = { ...project('project-b'), working_dir_locked: true, source_dirs: ['/chosen'] }
  activeSessionId.set(null); pendingGroupId.set(original.id); sessionGroups.set([original, other]); nativeShell.set(true)
  vi.mocked(nativePickFolder).mockResolvedValue({ path: '/chosen', cancelled: false })
  app = mount(Composer, { target }); flushSync(); directoryChip().click(); await settle()
  expect(get(pendingGroupId)).toBe(other.id)
  expect(get(pendingWorkingDir)).toBe('')
  expect(get(sessionGroups)).toEqual([original, other])
  expect(directoryChip().disabled).toBe(false)
  expect(directoryChip().textContent).toContain('project-b')
})

it('shows the project chooser when the new folder is referenced by several projects', async () => {
  const original = project()
  const b = { ...project('project-b'), source_dirs: ['/shared'] }
  const c = { ...project('project-c'), source_dirs: ['/shared'] }
  activeSessionId.set(null); pendingGroupId.set(original.id); sessionGroups.set([original, b, c]); nativeShell.set(true)
  vi.mocked(nativePickFolder).mockResolvedValue({ path: '/shared', cancelled: false })
  app = mount(Composer, { target }); flushSync(); directoryChip().click(); await settle()
  expect(get(pendingGroupId)).toBe('')
  expect(get(pendingWorkingDir)).toBe('/shared')
  expect(target.querySelector('.proj-menu')?.textContent).toContain(tr('chat.project_ambiguous'))
  expect(target.querySelector('.proj-menu')?.textContent).toContain('project-b')
  expect(target.querySelector('.proj-menu')?.textContent).toContain('project-c')
  expect(updateSessionGroup).not.toHaveBeenCalled()
})

it('blocks sending until the native directory selection completes', async () => {
  const group = project(); activeSessionId.set(null); pendingGroupId.set(group.id); sessionGroups.set([group]); nativeShell.set(true)
  const pending = deferred<Awaited<ReturnType<typeof nativePickFolder>>>(); vi.mocked(nativePickFolder).mockReturnValue(pending.promise)
  const onSend = vi.fn(); app = mount(Composer, { target, props: { onSend } }); flushSync()
  directoryChip().click(); await settle(); type('hello'); send(); await settle()
  expect(onSend).not.toHaveBeenCalled(); expect((target.querySelector('.send-btn') as HTMLButtonElement).disabled).toBe(true)
  pending.resolve({ path: '/chosen', cancelled: false }); await settle(); send(); await settle()
  expect(onSend).toHaveBeenCalledOnce()
})

it('leaves the draft destination unchanged when the picker is cancelled or fails', async () => {
  const group = project(); activeSessionId.set(null); pendingGroupId.set(group.id); sessionGroups.set([group]); nativeShell.set(true)
  vi.mocked(nativePickFolder).mockResolvedValueOnce({ path: '', cancelled: true }).mockRejectedValueOnce(new Error('dialog failed'))
  app = mount(Composer, { target }); flushSync(); directoryChip().click(); await settle()
  expect(updateSessionGroup).not.toHaveBeenCalled(); expect(get(sessionGroups)[0].working_dir).toBe(group.working_dir)
  directoryChip().click(); await settle()
  expect(get(sessionGroups)[0].working_dir).toBe(group.working_dir)
  expect(get(pendingGroupId)).toBe(group.id)
  expect(get(pendingWorkingDir)).toBe('')
  expect(directoryChip().disabled).toBe(false)
})
