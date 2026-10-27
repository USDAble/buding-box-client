import { afterEach, expect, it, vi } from 'vitest'
import { mount, unmount, flushSync } from 'svelte'
import { waitFor } from '@testing-library/svelte'
import AgentsView from './AgentsView.svelte'
import { listAgentDirectory } from '../lib/api'
// OCTO-FORK: exercise setup failures at the expert gallery's actual buttons.
import { summonAgent, openAgentSession, showToast } from '../lib/stores'
import { locale, tr } from '../lib/i18n'
vi.mock('../lib/api', () => ({ listAgentDirectory: vi.fn() }))
vi.mock('../lib/stores', () => ({ summonAgent: vi.fn(), showToast: vi.fn(), openAgentSession: vi.fn() }))
const expert = { id: 'platform:internal_uuid:2', name: 'Writing guide', description: 'Prepare clear reports', source: 'platform' as const, version: 2, platform_skills: [{ id: 'skill_internal', code: 'draft', name: 'Draft', description: '', content: '', version: 1 }], enabled: true }
let app: ReturnType<typeof mount> | undefined
let target: HTMLElement
// OCTO-FORK: cached expert cards retain the real session entry point during refresh failures.
afterEach(() => { if (app) unmount(app); target?.remove(); vi.resetAllMocks(); locale.set('en') })
it('keeps cached cards on refresh failure, hides IDs and uses the real expert action', async () => {
  vi.mocked(listAgentDirectory).mockResolvedValueOnce({ agents: [expert], cached: true, updatedAt: '' }).mockRejectedValueOnce(new Error('offline'))
  target = document.createElement('div'); document.body.appendChild(target)
  app = mount(AgentsView, { target }); flushSync()
  await waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toContain('offline'))
  expect(target.textContent).toContain('Writing guide')
  expect(target.textContent).not.toContain('internal_uuid')
  expect(target.textContent).not.toContain('skill_internal')
  ;(target.querySelector('.card-footer .btn-primary') as HTMLButtonElement).click()
  await waitFor(() => expect(summonAgent).toHaveBeenCalledWith(expert.id, expert.name))
  expect(listAgentDirectory).toHaveBeenNthCalledWith(2, true)
  const input = target.querySelector('input')!
  input.value = 'missing'; input.dispatchEvent(new Event('input', { bubbles: true })); flushSync()
  expect(target.querySelector('.agent-card')).toBeNull()
})

// OCTO-FORK: model failures stay visible, pending launches cannot duplicate, and retry recovers.
it.each(['en', 'zh', 'zh-Hant'])('shows unavailable-model feedback and permits retry in %s', async (language) => {
  locale.set(language)
  vi.mocked(listAgentDirectory).mockResolvedValue({ agents: [], cached: false, updatedAt: '' })
  let rejectLaunch!: (error: Error) => void
  vi.mocked(openAgentSession).mockImplementationOnce(() => new Promise<void>((_resolve, reject) => { rejectLaunch = reject }))
  target = document.createElement('div'); document.body.appendChild(target)
  app = mount(AgentsView, { target }); flushSync()
  await waitFor(() => expect(target.textContent).toContain(tr('agents.empty')))
  const headerButton = target.querySelector<HTMLButtonElement>('.header-actions button')!
  const emptyButton = target.querySelector<HTMLButtonElement>('.empty-state button')!
  headerButton.click(); headerButton.click(); flushSync()
  expect(openAgentSession).toHaveBeenCalledTimes(1)
  expect(openAgentSession).toHaveBeenCalledWith('/expert-agent-manager', tr('agents.session_new'))
  expect(headerButton.disabled).toBe(true)
  expect(emptyButton.disabled).toBe(true)
  expect(headerButton.getAttribute('aria-busy')).toBe('true')
  expect(headerButton.textContent).toContain(tr('common.loading'))

  rejectLaunch(new Error(tr('models.none_available')))
  const message = tr('agents.setup_failed').replace('{error}', tr('models.none_available'))
  await waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toBe(message))
  expect(showToast).toHaveBeenCalledWith(message, 'error')
  expect(headerButton.disabled).toBe(false)
  expect(emptyButton.disabled).toBe(false)

  vi.mocked(openAgentSession).mockResolvedValueOnce()
  emptyButton.click()
  await waitFor(() => expect(openAgentSession).toHaveBeenCalledTimes(2))
  await waitFor(() => expect(headerButton.disabled).toBe(false))
  expect(target.querySelector('[role="alert"]')).toBeNull()
  expect(showToast).toHaveBeenCalledTimes(1)
})

it('keeps the session-creation failure visible without reporting success', async () => {
  vi.mocked(listAgentDirectory).mockResolvedValue({ agents: [], cached: false, updatedAt: '' })
  vi.mocked(openAgentSession).mockRejectedValueOnce(new Error('503 Service Unavailable'))
  target = document.createElement('div'); document.body.appendChild(target)
  app = mount(AgentsView, { target }); flushSync()
  target.querySelector<HTMLButtonElement>('.header-actions button')!.click()
  const message = tr('agents.setup_failed').replace('{error}', '503 Service Unavailable')
  await waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toBe(message))
  expect(showToast).toHaveBeenCalledExactlyOnceWith(message, 'error')
})

it('shows localized network feedback when editing an expert fails', async () => {
  locale.set('zh')
  vi.mocked(listAgentDirectory).mockResolvedValue({ agents: [{ ...expert, id: 'mine', source: 'user' }], cached: false, updatedAt: '' })
  vi.mocked(openAgentSession).mockRejectedValueOnce(new TypeError('Failed to fetch'))
  target = document.createElement('div'); document.body.appendChild(target)
  app = mount(AgentsView, { target }); flushSync()
  await waitFor(() => expect(target.querySelector('.agent-card')).not.toBeNull())
  target.querySelector<HTMLButtonElement>(`button[title="${tr('agents.edit_with_agent')}"]`)!.click()
  const message = tr('agents.setup_failed').replace('{error}', tr('product.tier.network_unavailable'))
  await waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toBe(message))
  expect(openAgentSession).toHaveBeenCalledWith('/expert-agent-manager edit mine', tr('agents.session_edit').replace('{name}', expert.name))
  expect(showToast).toHaveBeenCalledExactlyOnceWith(message, 'error')
  expect(target.textContent).not.toContain('Failed to fetch')
})
