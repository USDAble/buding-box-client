// OCTO-FORK: browser transcript exports need an explicit destination instead of a silent download.
export interface ExportSaveHandle {
  name: string
  createWritable(): Promise<{
    write(content: Blob): Promise<void>
    close(): Promise<void>
    abort?(): Promise<void>
  }>
}

// ponytail: use the browser's native Save As picker; no custom folder UI can write to arbitrary paths.
export async function pickBrowserExport(name: string): Promise<ExportSaveHandle | null | undefined> {
  const picker = (window as Window & {
    showSaveFilePicker?: (options: { suggestedName: string }) => Promise<ExportSaveHandle>
  }).showSaveFilePicker
  if (!picker) return undefined
  try {
    return await picker.call(window, { suggestedName: name })
  } catch (error) {
    if ((error as DOMException).name === 'AbortError') return null
    throw error
  }
}

export async function writeBrowserExport(handle: ExportSaveHandle, content: Blob): Promise<void> {
  const stream = await handle.createWritable()
  try {
    await stream.write(content)
    await stream.close()
  } catch (error) {
    await stream.abort?.().catch(() => {})
    throw error
  }
}
