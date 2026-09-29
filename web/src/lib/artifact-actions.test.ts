import { describe, it, expect, afterEach, vi } from 'vitest'
import { imagePreviewError, downloadArtifact } from './artifact-actions'
import { nativeShell } from './stores'
import * as api from './api'
import type { Artifact } from './types'

// A failed <img> fires onerror with no reason attached; imagePreviewError
// re-requests the src to read the endpoint's error body (#1896).

afterEach(() => {
  nativeShell.set(false)
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

// OCTO-FORK: Office artifacts must preserve binary bytes through both browser and desktop save paths.
describe('downloadArtifact — Office files', () => {
  const artifact = { name: 'report.xlsx', src: '/api/artifact', code: '/tmp/report.xlsx' } as Artifact

  it('downloads the fetched bytes with the original filename in a browser', async () => {
    nativeShell.set(false)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(new Uint8Array([0, 1, 255]))))
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: () => 'blob:artifact', revokeObjectURL: () => {} }))
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      expect(this.download).toBe('report.xlsx')
      expect(this.href).toContain('blob:artifact')
    })

    await downloadArtifact(artifact, vi.fn())

    expect(fetch).toHaveBeenCalledWith('/api/artifact')
    expect(click).toHaveBeenCalledOnce()
  })

  it('passes exact bytes to the desktop save dialog', async () => {
    nativeShell.set(true)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(new Uint8Array([0, 1, 255]))))
    const save = vi.spyOn(api, 'nativeSaveBinary').mockResolvedValue({ path: '/tmp/report.xlsx', cancelled: false })

    await downloadArtifact(artifact, vi.fn())

    expect(save).toHaveBeenCalledWith('report.xlsx', 'AAH/')
  })
})

describe('imagePreviewError', () => {
  it("returns the server's error body for a failing src", async () => {
    vi.stubGlobal('fetch', vi.fn(async () =>
      new Response(JSON.stringify({ error: 'artifact exceeds the 10 MB preview cap' }), { status: 413 }),
    ))

    expect(await imagePreviewError('/api/x')).toBe('artifact exceeds the 10 MB preview cap')
  })

  it('falls back to the status line when the body is not JSON', async () => {
    vi.stubGlobal('fetch', vi.fn(async () =>
      new Response('<html>gateway error</html>', { status: 502, statusText: 'Bad Gateway' }),
    ))

    expect(await imagePreviewError('/api/x')).toBe('502 Bad Gateway')
  })

  it('returns empty when the refetch succeeds (decode failure) or throws', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(new Uint8Array([1, 2, 3]))))
    expect(await imagePreviewError('/api/x')).toBe('')

    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('network down') }))
    expect(await imagePreviewError('/api/x')).toBe('')
  })
})
