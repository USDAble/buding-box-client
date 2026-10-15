import { afterEach, beforeEach, expect, it, vi } from 'vitest'

vi.mock('./stores', () => ({ isDesktopShell: true }))

let header: HTMLElement
let invoke: ReturnType<typeof vi.fn>
let listeners: Array<[string, EventListenerOrEventListenerObject, boolean | AddEventListenerOptions | undefined]>

beforeEach(() => {
  vi.resetModules()
  invoke = vi.fn()
  vi.stubGlobal('_wails', { invoke })
  header = document.createElement('header')
  header.style.cssText = '--wails-draggable:drag; padding:0px'
  Object.defineProperties(header, { clientWidth: { value: 800 }, clientHeight: { value: 44 } })
  document.body.append(header)
  listeners = []
  const add = window.addEventListener.bind(window)
  vi.spyOn(window, 'addEventListener').mockImplementation((type, listener, options) => {
    listeners.push([type, listener, options])
    add(type, listener, options)
  })
})

afterEach(() => {
  for (const [type, listener, options] of listeners) window.removeEventListener(type, listener, options)
  header.remove()
  document.body.style.cursor = ''
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

async function init(platform = 'MacIntel') {
  vi.spyOn(navigator, 'platform', 'get').mockReturnValue(platform)
  const { initFramelessDrag } = await import('./framelessDrag')
  initFramelessDrag()
}

function mouse(type: string, init: MouseEventInit = {}, target = header) {
  target.dispatchEvent(new MouseEvent(type, {
    bubbles: true, cancelable: true, button: 0, detail: 1, clientX: 300, clientY: 27, ...init,
  }))
}

it('starts a Mac drag on the initial press, even when WebKit reports buttons=0', async () => {
  await init()
  mouse('mousedown', { buttons: 0 })
  expect(invoke).toHaveBeenCalledExactlyOnceWith('wails:drag')
  mouse('mousemove', { buttons: 1 })
  expect(invoke).toHaveBeenCalledTimes(1)
  mouse('mouseup', { buttons: 0 })
  mouse('mousedown', { buttons: 0 })
  expect(invoke).toHaveBeenCalledTimes(2)
})

it('leaves Mac edge resizing to AppKit instead of stealing the titlebar press', async () => {
  await init()
  mouse('mousemove', { clientY: 2 })
  expect(document.body.style.cursor).toBe('')
  mouse('mousedown', { buttons: 1, clientY: 2 })
  expect(invoke).toHaveBeenCalledExactlyOnceWith('wails:drag')
})

it('keeps language controls clickable and preserves the second press of a double-click', async () => {
  await init()
  const language = document.createElement('button')
  language.style.setProperty('--wails-draggable', 'no-drag')
  header.append(language)
  const click = vi.fn()
  language.addEventListener('click', click)
  mouse('mousedown', { buttons: 1 }, language)
  mouse('mousemove', { buttons: 1 }, language)
  mouse('mouseup', { buttons: 0 }, language)
  mouse('click', {}, language)
  expect(click).toHaveBeenCalledOnce()
  mouse('mousedown', { buttons: 1, detail: 2 })
  mouse('mousemove', { buttons: 1 })
  expect(invoke).not.toHaveBeenCalled()
})

it('continues to start Windows dragging on movement', async () => {
  await init('Win32')
  mouse('mousedown', { buttons: 1 })
  expect(invoke).not.toHaveBeenCalled()
  mouse('mousemove', { buttons: 1 })
  expect(invoke).toHaveBeenCalledExactlyOnceWith('wails:drag')
})
