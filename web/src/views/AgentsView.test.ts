import { afterEach, expect, it, vi } from 'vitest'
import { mount, unmount, flushSync } from 'svelte'
import { waitFor } from '@testing-library/svelte'
import AgentsView from './AgentsView.svelte'
import { listAgentDirectory } from '../lib/api'
import { summonAgent } from '../lib/stores'
vi.mock('../lib/api', () => ({ listAgentDirectory: vi.fn() }))
vi.mock('../lib/stores', () => ({ summonAgent: vi.fn(), showToast: vi.fn(), openAgentSession: vi.fn() }))
const expert = { id: 'platform:internal_uuid:2', name: 'Writing guide', description: 'Prepare clear reports', source: 'platform' as const, version: 2, platform_skills: [{ id: 'skill_internal', code: 'draft', name: 'Draft', description: '', content: '', version: 1 }], enabled: true }
let app: ReturnType<typeof mount> | undefined
let target: HTMLElement
// OCTO-FORK: cached expert cards retain the real session entry point during refresh failures.
afterEach(() => { if (app) unmount(app); target?.remove(); vi.clearAllMocks() })
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
