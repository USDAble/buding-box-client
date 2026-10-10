// OCTO-FORK: verify skill visibility survives page remounts without changing skill enablement.
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mount, unmount, flushSync } from 'svelte'
import { waitFor } from '@testing-library/svelte'
import SkillsView from './SkillsView.svelte'
import { listSkills, toggleSkill } from '../lib/api'
import { showToast } from '../lib/stores'

vi.mock('../lib/api', () => ({ listSkills: vi.fn(), toggleSkill: vi.fn() }))
vi.mock('../lib/stores', async () => {
  const { writable } = await import('svelte/store')
  return { skills: writable([]), nativeShell: writable(false), showToast: vi.fn(), openAgentSession: vi.fn() }
})

const key = 'octo.skills.showSystem'
let app: ReturnType<typeof mount> | undefined
let target: HTMLElement

beforeEach(() => {
  const data = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => { data.set(key, value) },
  })
  vi.mocked(listSkills).mockResolvedValue(['default', 'expert', 'user'].map(source => ({
    name: `${source}-skill`, desc: '', icon: '', source, enabled: true, tagStatus: 'success', tagLabel: '',
  })))
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = undefined
  target.remove()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

async function enterPage() {
  app = mount(SkillsView, { target })
  flushSync()
  await waitFor(() => expect(target.textContent).toContain('user-skill'))
  return target.querySelector('.system-toggle [role="switch"]') as HTMLButtonElement
}

it('defaults to on, then remembers off and on across page visits', async () => {
  let toggle = await enterPage()
  expect(toggle).toHaveAttribute('aria-checked', 'true')
  expect(target.textContent).toContain('default-skill')
  expect(target.textContent).toContain('expert-skill')

  toggle.click()
  flushSync()
  expect(localStorage.getItem(key)).toBe('false')
  await unmount(app!)
  app = undefined

  toggle = await enterPage()
  expect(toggle).toHaveAttribute('aria-checked', 'false')
  expect(target.textContent).not.toContain('default-skill')
  expect(target.textContent).not.toContain('expert-skill')
  toggle.click()
  flushSync()
  expect(localStorage.getItem(key)).toBe('true')
  await unmount(app!)
  app = undefined

  toggle = await enterPage()
  expect(toggle).toHaveAttribute('aria-checked', 'true')
  expect(target.textContent).toContain('default-skill')
  expect(target.textContent).toContain('expert-skill')
  expect(toggleSkill).not.toHaveBeenCalled()
})

it('reports unavailable storage while keeping the current page filter usable', async () => {
  vi.stubGlobal('localStorage', {
    getItem: () => { throw new Error('storage unavailable') },
    setItem: () => { throw new Error('storage unavailable') },
  })
  const toggle = await enterPage()
  expect(toggle).toHaveAttribute('aria-checked', 'true')
  toggle.click()
  flushSync()
  expect(toggle).toHaveAttribute('aria-checked', 'false')
  expect(target.textContent).not.toContain('default-skill')
  expect(showToast).toHaveBeenCalledWith('storage unavailable', 'error')
})
