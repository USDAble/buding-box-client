// OCTO-FORK: exercise the actual inline startup screen without importing the app.
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { en, zh } from './i18n'

const html = readFileSync('index.html', 'utf8')
const script = html.match(/<script>([\s\S]*?)<\/script>/)![1]
const entry = readFileSync('src/main.ts', 'utf8')

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('performance', { mark: vi.fn(), now: () => 0 })
  document.documentElement.lang = 'en'
  document.body.innerHTML = new DOMParser().parseFromString(html, 'text/html').body.innerHTML
  new Function(script.replace('__STARTUP_COPY__', JSON.stringify({ en, zh })))()
})

afterEach(() => {
  window.dispatchEvent(new Event('app:mounted'))
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

it('shows loading before app code runs and hands off to the mounted app', () => {
  expect(document.getElementById('startup-message')?.textContent).toBe(en['common.loading'])
  expect(document.getElementById('startup-screen')).toHaveAttribute('aria-busy', 'true')
  document.getElementById('app')!.textContent = 'Login screen'
  window.dispatchEvent(new Event('app:mounted'))
  vi.advanceTimersByTime(15000)
  expect(document.getElementById('startup-screen')).toBeNull()
  expect(document.getElementById('app')?.textContent).toBe('Login screen')
})

it('offers a retry when the entry module fails', () => {
  const module = document.createElement('script')
  module.type = 'module'
  document.body.appendChild(module)
  module.dispatchEvent(new Event('error'))
  expect(document.getElementById('startup-message')?.textContent).toBe(en['startup.failed'])
  expect(document.getElementById('startup-retry')).toBeVisible()
  expect(document.getElementById('startup-screen')).toHaveAttribute('aria-busy', 'false')
  vi.advanceTimersByTime(15000)
  expect(document.getElementById('startup-message')?.textContent).toBe(en['startup.failed'])
})

it('offers a retry without declaring a slow startup failed', () => {
  vi.advanceTimersByTime(15000)
  expect(document.getElementById('startup-message')?.textContent).toBe(en['startup.slow'])
  expect(document.getElementById('startup-retry')).toBeVisible()
})

it('waits for a paint opportunity before importing the application graph', () => {
  let frame!: FrameRequestCallback
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frame = callback })
  const loadApp = vi.fn().mockResolvedValue({})
  new Function('loadApp', entry.replace("import('./bootstrap')", 'loadApp()'))(loadApp)
  expect(loadApp).not.toHaveBeenCalled()
  frame(0)
  expect(loadApp).not.toHaveBeenCalled()
  vi.advanceTimersByTime(0)
  expect(loadApp).toHaveBeenCalledOnce()
})

it('reports deferred application import errors through the loading screen', async () => {
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => callback(0))
  const loadApp = vi.fn().mockRejectedValue(new Error('chunk unavailable'))
  const log = vi.spyOn(console, 'error').mockImplementation(() => {})
  new Function('loadApp', entry.replace("import('./bootstrap')", 'loadApp()'))(loadApp)
  await vi.advanceTimersByTimeAsync(0)
  expect(document.getElementById('startup-message')?.textContent).toBe(en['startup.failed'])
  expect(document.getElementById('startup-retry')).toBeVisible()
  log.mockRestore()
})
