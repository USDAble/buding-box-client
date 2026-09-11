import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { locale } from '../../lib/i18n'
import PrivacyMark from './PrivacyMark.svelte'

let target: HTMLElement
let app: Record<string, unknown> | null = null

beforeEach(() => {
  locale.set('zh')
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  locale.set('en')
})

function render(mode: string, label = false) {
  app = mount(PrivacyMark, { target, props: { mode, label } }) as Record<string, unknown>
  flushSync()
}

describe('PrivacyMark', () => {
  it('renders the shield and optional label in privacy mode', () => {
    render('privacy', true)
    expect(target.querySelector('[data-privacy-mark]')).not.toBeNull()
    expect(target.textContent).toContain('隐私')
  })

  it('stays hidden outside privacy mode', () => {
    render('smart')
    expect(target.querySelector('[data-privacy-mark]')).toBeNull()
  })

  it('renders a shield without label text for the sidebar variant', () => {
    render('privacy')
    expect(target.querySelector('iconify-icon')?.getAttribute('icon')).toBe('lucide:shield-check')
    expect(target.textContent?.trim()).toBe('')
  })
})
