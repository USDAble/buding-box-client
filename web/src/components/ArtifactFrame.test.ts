// OCTO-FORK: exercise the real preview component without relaxing frame isolation.
import { afterEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import type { Artifact } from '../lib/types'
import ArtifactFrame from './ArtifactFrame.svelte'

vi.mock('../lib/artifacts', async () => {
  const { writable } = await import('svelte/store')
  return { ARTIFACT_SANDBOX: 'allow-scripts', ARTIFACT_ORIGIN_SANDBOX: 'allow-scripts allow-same-origin', probeArtifactOrigin: vi.fn(), themeRev: writable(0) }
})

let app: ReturnType<typeof mount> | undefined
let target: HTMLDivElement
afterEach(() => { if (app) unmount(app); app = undefined; target?.remove(); vi.unstubAllGlobals() })

it('fits measured overflow, resets on panel resize and retains the isolated source', () => {
  let resize!: ResizeObserverCallback
  const disconnect = vi.fn()
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: ResizeObserverCallback) { resize = callback }
    observe() {}
    disconnect = disconnect
  })
  target = document.createElement('div'); document.body.appendChild(target)
  app = mount(ArtifactFrame, { target, props: { artifact: { name: 'poster.html', originURL: 'http://preview.artifacts.localhost:8088/', rev: 1 } as Artifact } })
  flushSync()
  const frame = target.querySelector('iframe')!
  const measure = (width: number, height: number) => {
    resize([{ contentRect: { width, height } } as ResizeObserverEntry], {} as ResizeObserver); flushSync()
  }
  const report = (viewportWidth: number) => {
    window.dispatchEvent(new MessageEvent('message', { source: frame.contentWindow, origin: 'http://preview.artifacts.localhost:8088', data: { type: 'octo:artifact-layout', viewportWidth, width: 900, height: 1350, clipped: true } })); flushSync()
  }
  measure(400, 800); report(400)
  expect(frame.style.width).toBe('900px')
  expect(frame.style.height).toBe('1350px')
  expect(frame.style.transform).toBe(`scale(${400 / 900})`)
  expect(frame.src).toContain('http://preview.artifacts.localhost:8088/')
  expect(frame.hasAttribute('srcdoc')).toBe(false)
  expect(frame.getAttribute('sandbox')).toBe('allow-scripts allow-same-origin')
  measure(1200, 700)
  expect(frame.style.width).toBe('1200px')
  expect(frame.style.transform).toBe('scale(1)')
  report(400)
  expect(frame.style.transform).toBe('scale(1)')
})
