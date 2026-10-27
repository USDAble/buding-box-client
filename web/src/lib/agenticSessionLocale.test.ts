// OCTO-FORK: automatic setup prompts must carry the click-time UI language to the actual send queue.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import { locale } from './i18n'
import {
  activeSessionId, agenticSessions, openAgentSession, pendingModel,
  pendingPrompt, sessions, view,
} from './stores'
import { defaultSelectableModel, selectableModels, type SelectableModel } from './selectableModels'

const model: SelectableModel = {
  id: 'gateway::ready', modelId: 'ready', vendorId: 'gateway', vendorName: 'Gateway',
  displayName: 'Ready', source: 'catalog', trust: 'signed-catalog', confidential: false,
  sourceOrder: 0, eligible: true,
}
const commands = ['/cron-task-creator', '/expert-agent-manager', '/skill-creator']

beforeEach(() => {
  selectableModels.set([model])
  defaultSelectableModel.set(model.id)
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ id: 'setup' }), { status: 200 })))
})

afterEach(() => {
  vi.unstubAllGlobals()
  locale.set('en')
  selectableModels.set([])
  defaultSelectableModel.set('')
  pendingModel.set('')
  pendingPrompt.set(null)
  activeSessionId.set(null)
  sessions.set([])
  agenticSessions.clear()
  view.set('chat')
})

describe.each([
  ['en', 'Use English'],
  ['zh', '使用简体中文'],
  ['zh-Hant', '使用繁體中文'],
])('button-launched setup in %s', (language, instruction) => {
  it.each(commands)('adds the language to %s without changing its command', async (command) => {
    locale.set(language)
    await openAgentSession(command, 'Setup')

    const queued = get(pendingPrompt)!
    expect(queued.sessionId).toBe('setup')
    expect(queued.content).toMatch(new RegExp(`^${command}\\n\\n`))
    expect(queued.content).toContain(instruction)
    expect(queued.content).toContain('API')
    expect(get(activeSessionId)).toBe('setup')
    expect(get(view)).toBe('chat')
    expect(vi.mocked(fetch)).toHaveBeenCalledTimes(1)
    expect(JSON.parse(vi.mocked(fetch).mock.calls[0][1]?.body as string)).toMatchObject({
      model: model.id, name: 'Setup', source: 'manual',
    })
  })
})

it('preserves edit arguments and the clicked language while session creation is pending', async () => {
  let finishCreation!: (response: Response) => void
  vi.mocked(fetch).mockImplementationOnce(() => new Promise<Response>(resolve => { finishCreation = resolve }))
  locale.set('zh-Hant')
  const command = '/cron-task-creator edit task_123 "Daily report"'
  const opening = openAgentSession(command, 'Setup')
  await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(1))
  locale.set('en')
  finishCreation(new Response(JSON.stringify({ id: 'setup' }), { status: 200 }))
  await opening

  expect(get(pendingPrompt)?.content).toContain(`${command}\n\n`)
  expect(get(pendingPrompt)?.content).toContain('使用繁體中文')
  expect(get(pendingPrompt)?.content).not.toContain('Use English')
})
