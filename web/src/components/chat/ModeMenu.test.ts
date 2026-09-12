import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { chatModes } from '../../lib/chatMode'
import { locale } from '../../lib/i18n'
import type { ChatModeModel } from '../../lib/api'
import ModeMenu from './ModeMenu.svelte'

let target: HTMLElement
let app: Record<string, unknown> | null = null
// Typed, not a bare vi.fn(): the component's onPick prop is a two-argument
// function, and Mock<Procedure> satisfies svelte-check only by accident of
// `any`. Cleared per test rather than rebuilt, so the assertion in the click
// test cannot pass against a spy from an earlier render.
let pick = vi.fn<(mode: string, model: ChatModeModel) => void>()

beforeEach(() => {
  pick.mockClear()
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
    props: { currentMode, onPick: pick, onClose: vi.fn() },
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

// PR-4d: a row's name and its selectability both come from the catalog. These
// three assertions are what replaced the `if (!model.compositeId) return` guard
// and the `model.<id>` i18n lookup (需求基线 B6).
describe('ModeMenu catalog rows', () => {
  // The id is deliberately one that used to have an i18n name ("云端旗舰"), and
  // the catalog name below is deliberately different from it. A reverted lookup
  // — reading i18n again and re-adding the key — would render "云端旗舰" and go
  // red, which is the only way this pair stays a guard rather than a snapshot.
  const rows = [
    {
      id: 'privacy',
      models: [
        {
          id: 'buding-cloud-pro',
          displayName: { zh: '目录名·旗舰', en: 'Catalog Pro' },
          compositeId: 'buding-gateway::buding-cloud-pro',
        },
      ],
      defaultModel: 'buding-gateway::buding-cloud-pro',
    },
  ]

  it('renders the name the catalog sent, not a local one', () => {
    chatModes.set(rows)
    render('privacy') // the current group starts expanded
    expect(target.querySelector('.mi-name')?.textContent).toBe('目录名·旗舰')
  })

  it('picks a catalog row on click', () => {
    // The composite id is built by the projection, so every row the menu can
    // draw is selectable — there is no "listed but not configured" state left
    // for a guard to refuse.
    chatModes.set(rows)
    render('privacy')

    target.querySelector<HTMLButtonElement>('.menu-item')?.click()
    flushSync()

    expect(pick).toHaveBeenCalledWith('privacy', rows[0].models[0])
  })

  it('re-renders the name when the interface language changes', () => {
    // No refetch and no reload: the row already carries both languages.
    chatModes.set(rows)
    locale.set('en')
    render('privacy')
    expect(target.querySelector('.mi-name')?.textContent).toBe('Catalog Pro')
  })
})
