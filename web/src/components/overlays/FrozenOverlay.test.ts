import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'

// The last hop of the freeze chain: the event reaches the `frozen` store (covered in
// lib/productEvents.test.ts) and this component is what the user actually sees. V-97
// registered that the whole chain had no nail, so the server could emit a correct
// datastore:lost, the store could be set, and a wrong binding here would still leave
// every other nail green — with the write gate refusing and no overlay explaining why.
//
// The title/description are asserted through `tr` rather than as literal Chinese: the
// copy's owner is i18n (frozen.title / frozen.desc), and a literal here would go stale
// the moment the wording changes.

const { nativeQuit } = vi.hoisted(() => ({ nativeQuit: vi.fn() }))
vi.mock('../../lib/api', () => ({ nativeQuit: () => nativeQuit() }))

import { frozen } from '../../lib/stores'
import { locale, tr } from '../../lib/i18n'
import FrozenOverlay from './FrozenOverlay.svelte'

let target: HTMLElement
let app: Record<string, unknown> | null = null

beforeEach(() => {
  locale.set('zh')
  frozen.set(false)
  nativeQuit.mockReset()
  target = document.createElement('div')
  document.body.appendChild(target)
})

afterEach(() => {
  if (app) unmount(app)
  app = null
  target.remove()
  frozen.set(false)
  locale.set('en')
})

function render() {
  app = mount(FrozenOverlay, { target }) as Record<string, unknown>
  flushSync()
}

function setFrozen(value: boolean) {
  frozen.set(value)
  flushSync()
}

function overlay(): Element | null {
  return target.querySelector('.frozen-backdrop')
}

function quitButton(): HTMLButtonElement {
  const button = target.querySelector<HTMLButtonElement>('.frozen-quit')
  if (!button) throw new Error('no quit control — a frozen window would have no way out')
  return button
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0))

describe('FrozenOverlay', () => {
  it('stays invisible while the data root is healthy', () => {
    render()
    expect(overlay()).toBeNull()
  })

  it('covers the window once the store goes true, with the registered copy', () => {
    render()
    setFrozen(true)

    expect(overlay()).not.toBeNull()
    expect(target.textContent).toContain(tr('frozen.title'))
    expect(target.textContent).toContain(tr('frozen.desc'))
  })

  it('clears when the root comes back', () => {
    // The counter-nail for a one-way binding: a build that rendered on the store but
    // never re-evaluated it would leave a recovered window permanently covered.
    frozen.set(true)
    render()
    expect(overlay()).not.toBeNull()

    setFrozen(false)
    expect(overlay()).toBeNull()
  })

  it('asks the shell to quit once, even if the user clicks twice', async () => {
    nativeQuit.mockImplementation(() => new Promise<void>(() => {}))
    frozen.set(true)
    render()

    quitButton().click()
    flushSync()
    quitButton().click()

    expect(nativeQuit).toHaveBeenCalledTimes(1)
  })

  it('gives the control back when quitting fails, rather than leaving a dead end', async () => {
    nativeQuit.mockRejectedValue(new Error('native bridge gone'))
    frozen.set(true)
    render()

    const button = quitButton()
    button.click()
    flushSync()
    expect(button.disabled).toBe(true)

    await tick()
    flushSync()

    expect(button.disabled).toBe(false)
    expect(overlay()).not.toBeNull()
  })
})
