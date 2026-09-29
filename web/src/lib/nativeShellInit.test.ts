import { describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

describe('nativeShell initialization', () => {
  it('uses native file dialogs in the desktop shell', async () => {
    vi.stubGlobal('location', new URL('http://127.0.0.1/?shell=octo-desktop'))
    vi.resetModules()
    try {
      const { isDesktopShell, nativeShell } = await import('./stores')
      expect(isDesktopShell).toBe(true)
      expect(get(nativeShell)).toBe(true)
    } finally {
      vi.unstubAllGlobals()
      vi.resetModules()
    }
  })

  it('keeps an ordinary browser on the browser download path', async () => {
    vi.stubGlobal('location', new URL('http://127.0.0.1/'))
    vi.resetModules()
    try {
      const { isDesktopShell, nativeShell } = await import('./stores')
      expect(isDesktopShell).toBe(false)
      expect(get(nativeShell)).toBe(false)
    } finally {
      vi.unstubAllGlobals()
      vi.resetModules()
    }
  })
})
