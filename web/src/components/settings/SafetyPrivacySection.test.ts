import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, tick, unmount } from 'svelte'
import { locale } from '../../lib/i18n'
import SafetyPrivacySection from './SafetyPrivacySection.svelte'

const getPersonalInfoRules = vi.fn()
vi.mock('../../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/api')>()
  return { ...actual, getPersonalInfoRules: () => getPersonalInfoRules() }
})

let target: HTMLElement
let app: Record<string, unknown> | null = null

beforeEach(() => {
  locale.set('zh')
  getPersonalInfoRules.mockResolvedValue({ ruleVersion: 'builtin-1', rules: ['cn_mobile', 'email'] })
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
})

describe('SafetyPrivacySection', () => {
  it('renders the registered rule IDs as localized, read-only personal-information rules', async () => {
    app = mount(SafetyPrivacySection, { target }) as Record<string, unknown>
    await tick()
    await new Promise((resolve) => setTimeout(resolve, 0))
    flushSync()

    expect(target.textContent).toContain('规则集 builtin-1')
    expect(target.textContent).toContain('手机号')
    expect(target.textContent).toContain('邮箱')
    expect(target.textContent).toContain('内置规则只读')
    expect(target.textContent).toContain('敏感词库')
  })
})
