import { afterEach, describe, expect, it, vi } from 'vitest'
import { pickBrowserExport, writeBrowserExport, type ExportSaveHandle } from './exportSave'

afterEach(() => vi.unstubAllGlobals())

describe('browser transcript save', () => {
  it('opens Save As with the suggested filename before writing the export', async () => {
    const write = vi.fn().mockResolvedValue(undefined)
    const close = vi.fn().mockResolvedValue(undefined)
    const handle: ExportSaveHandle = { name: 'chat.md', createWritable: vi.fn().mockResolvedValue({ write, close }) }
    const picker = vi.fn().mockResolvedValue(handle)
    vi.stubGlobal('showSaveFilePicker', picker)

    const selected = await pickBrowserExport('chat.md')
    expect(picker).toHaveBeenCalledWith({ suggestedName: 'chat.md' })
    expect(handle.createWritable).not.toHaveBeenCalled()
    await writeBrowserExport(selected!, new Blob(['hello']))
    expect(write).toHaveBeenCalledWith(expect.any(Blob))
    expect(close).toHaveBeenCalledOnce()
  })

  it('distinguishes a cancelled picker from an unsupported browser', async () => {
    vi.stubGlobal('showSaveFilePicker', vi.fn().mockRejectedValue({ name: 'AbortError' }))
    expect(await pickBrowserExport('chat.md')).toBeNull()
    vi.stubGlobal('showSaveFilePicker', undefined)
    expect(await pickBrowserExport('chat.md')).toBeUndefined()
  })

  it('aborts a failed write so a partial export is not committed', async () => {
    const abort = vi.fn().mockResolvedValue(undefined)
    const handle: ExportSaveHandle = {
      name: 'chat.md',
      createWritable: vi.fn().mockResolvedValue({
        write: vi.fn().mockRejectedValue(new Error('disk full')),
        close: vi.fn(),
        abort,
      }),
    }
    await expect(writeBrowserExport(handle, new Blob(['hello']))).rejects.toThrow('disk full')
    expect(abort).toHaveBeenCalledOnce()
  })
})
