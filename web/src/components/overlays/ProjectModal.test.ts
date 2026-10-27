// OCTO-FORK: project cwd edits must follow the server's pre-message boundary.
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, cleanup } from '@testing-library/svelte'
import { get } from 'svelte/store'
import ProjectModal from './ProjectModal.svelte'
import * as api from '../../lib/api'
import { locale, tr } from '../../lib/i18n'
import { nativeShell, sessionGroups, sessions } from '../../lib/stores'
import type { SessionGroup, Session } from '../../lib/types'

vi.mock('../../lib/api', () => ({
  nativePickFolder: vi.fn(), updateSessionGroup: vi.fn(), createSessionGroup: vi.fn(), fsList: vi.fn(),
}))

let group: SessionGroup
beforeEach(() => {
  vi.resetAllMocks()
  locale.set('zh'); nativeShell.set(true); sessions.set([])
  group = { id: 'project', name: 'Work', working_dir: '/old', session_ids: [], source_dirs: [], working_dir_locked: false }
  sessionGroups.set([group])
  vi.mocked(api.nativePickFolder).mockResolvedValue({ path: '/chosen', cancelled: false })
  vi.mocked(api.updateSessionGroup).mockImplementation(async (_id, patch) => ({ ...group, ...patch }))
})
afterEach(() => { cleanup(); sessionGroups.set([]); sessions.set([]); nativeShell.set(false) })

it('chooses and saves a different directory for an empty project', async () => {
  const onSaved = vi.fn()
  render(ProjectModal, { group, onClose: vi.fn(), onSaved })
  await fireEvent.click(screen.getByRole('button', { name: tr('chat.dir_change') }))
  await waitFor(() => expect(screen.getByTitle('/chosen')).toBeInTheDocument())
  await fireEvent.click(screen.getByRole('button', { name: tr('project.save') }))
  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  expect(api.nativePickFolder).toHaveBeenCalledWith('/old')
  expect(api.updateSessionGroup).toHaveBeenCalledWith('project', { name: 'Work', source_dirs: [], working_dir: '/chosen' })
  expect(get(sessionGroups)[0].working_dir).toBe('/chosen')
})

it('uses the browser folder picker to change the working directory', async () => {
  nativeShell.set(false)
  vi.mocked(api.fsList).mockResolvedValue({ path: '/chosen', parent: '/', entries: [], truncated: false, is_this_pc: false })
  render(ProjectModal, { group, onClose: vi.fn() })
  await fireEvent.click(screen.getByRole('button', { name: tr('chat.dir_change') }))
  const select = await screen.findByRole('button', { name: tr('folder.select') })
  await waitFor(() => expect(select).toBeEnabled())
  await fireEvent.click(select)
  await fireEvent.click(screen.getByRole('button', { name: tr('project.save') }))
  await waitFor(() => expect(api.updateSessionGroup).toHaveBeenCalledWith('project', expect.objectContaining({ working_dir: '/chosen' })))
  expect(api.fsList).toHaveBeenCalledWith('/old')
})

it('keeps a used project locked while allowing its name and source folders to save', async () => {
  group.working_dir_locked = true
  render(ProjectModal, { group, onClose: vi.fn() })
  expect(screen.getByRole('button', { name: tr('chat.dir_change') })).toBeDisabled()
  expect(screen.getByText(tr('project.directory_locked'))).toBeInTheDocument()
  await fireEvent.click(screen.getByRole('button', { name: tr('project.save') }))
  await waitFor(() => expect(api.updateSessionGroup).toHaveBeenCalledWith('project', { name: 'Work', source_dirs: [] }))
  expect(api.nativePickFolder).not.toHaveBeenCalled()
})

it('drops a pending directory change if another window starts the first message', async () => {
  group.session_ids = ['chat']
  render(ProjectModal, { group, onClose: vi.fn() })
  await fireEvent.click(screen.getByRole('button', { name: tr('chat.dir_change') }))
  await waitFor(() => expect(screen.getByTitle('/chosen')).toBeInTheDocument())
  sessions.set([{ id: 'chat', status: 'running', protection_policy: { locked: true } } as Session])
  await waitFor(() => expect(screen.getByRole('button', { name: tr('chat.dir_change') })).toBeDisabled())
  expect(screen.getByTitle('/old')).toBeInTheDocument()
  await fireEvent.click(screen.getByRole('button', { name: tr('project.save') }))
  await waitFor(() => expect(api.updateSessionGroup).toHaveBeenCalledWith('project', { name: 'Work', source_dirs: [] }))
})

it('keeps the existing project creation request unchanged', async () => {
  vi.mocked(api.createSessionGroup).mockResolvedValue(group)
  render(ProjectModal, { onClose: vi.fn() })
  expect(screen.queryByRole('button', { name: tr('chat.dir_change') })).not.toBeInTheDocument()
  await fireEvent.input(screen.getByPlaceholderText(tr('project.name_ph')), { target: { value: 'Work' } })
  await fireEvent.click(screen.getByRole('button', { name: tr('project.create') }))
  await waitFor(() => expect(api.createSessionGroup).toHaveBeenCalledWith('Work', { source_dirs: [] }))
})
