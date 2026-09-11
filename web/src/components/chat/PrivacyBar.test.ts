import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { en, locale, zh } from '../../lib/i18n'
import PrivacyBar from './PrivacyBar.svelte'

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

function render(mode: string) {
  app = mount(PrivacyBar, { target, props: { mode } }) as Record<string, unknown>
  flushSync()
}

describe('PrivacyBar', () => {
  it('shows the required notice only in privacy mode and expands details', () => {
    render('privacy')
    expect(target.textContent).toContain('隐私模式：发送前会处理手机号等个人信息。')
    expect(target.textContent).not.toContain('只处理随后发给模型的内容')
    expect(target.querySelector('[data-dismiss]')).toBeNull()

    target.querySelector<HTMLButtonElement>('.details-toggle')!.click()
    flushSync()
    expect(target.textContent).toContain('只处理随后发给模型的内容')
  })

  it('stays hidden outside privacy mode', () => {
    render('default')
    expect(target.querySelector('[data-privacy-bar]')).toBeNull()
  })

  it('keeps the required copy in both dictionaries', () => {
    expect(zh['privacy.notice']).toBe('隐私模式：发送前会处理手机号等个人信息。')
    expect(zh['privacy.notice_detail']).toContain('本期演示 11 位手机号')
    expect(en['privacy.notice']).toContain('Privacy mode')
    expect(en['privacy.notice_detail']).toContain('11-digit phone numbers')
    for (const text of [zh['privacy.notice'], zh['privacy.notice_detail']]) {
      expect(text).not.toMatch(/身份证|银行卡/)
    }
    for (const text of [en['privacy.notice'], en['privacy.notice_detail']]) {
      expect(text).not.toMatch(/ID card|bank card/i)
    }
  })
})
