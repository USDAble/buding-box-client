import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { chatModes } from '../../lib/chatMode'
import { locale } from '../../lib/i18n'
import ModeMenu from './ModeMenu.svelte'

let target: HTMLElement
let app: Record<string, unknown> | null = null

beforeEach(() => {
  locale.set('zh')
  chatModes.set([
    { id: 'privacy', models: [], defaultModel: '' },
    { id: 'default', models: [], defaultModel: '' },
  ])
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  chatModes.set([])
  locale.set('en')
})

function render(currentMode: string) {
  app = mount(ModeMenu, {
    target,
    props: { currentMode, onPick: vi.fn(), onClose: vi.fn() },
  }) as Record<string, unknown>
  flushSync()
}

describe('ModeMenu privacy marker', () => {
  it('shows one shield when privacy is the current group', () => {
    render('privacy')
    expect(target.querySelectorAll('[data-privacy-mark]')).toHaveLength(1)
    expect(target.querySelector('.mode-header')?.textContent).toContain('隐私')
  })

  it('does not mark an inactive privacy group', () => {
    render('default')
    expect(target.querySelector('[data-privacy-mark]')).toBeNull()
  })
})
