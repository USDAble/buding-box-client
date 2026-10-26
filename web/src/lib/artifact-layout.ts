// OCTO-FORK: size the isolated preview without inspecting or rewriting its DOM.
export type ArtifactLayout = { width: number; height: number; clipped: boolean }

export function readArtifactLayout(event: MessageEvent, frame: HTMLIFrameElement, origin: string, viewportWidth: number): ArtifactLayout | null {
  const data = event.data
  if (event.source !== frame.contentWindow || event.origin !== origin || data?.type !== 'octo:artifact-layout') return null
  if (![data.width, data.height, data.viewportWidth].every(value => Number.isFinite(value) && value > 0 && value <= 32768)) return null
  // A report queued before a container resize describes the old viewport.
  if (Math.abs(data.viewportWidth - viewportWidth) > 2 || typeof data.clipped !== 'boolean') return null
  return { width: data.width, height: data.height, clipped: data.clipped }
}

export function fitArtifactLayout(width: number, height: number, layout: ArtifactLayout | null) {
  if (width <= 0 || height <= 0 || !layout) return { width, height, scale: 1 }
  const scale = Math.min(1, width / layout.width, layout.clipped ? height / layout.height : 1)
  return { width: layout.width, height: layout.clipped ? layout.height : height / scale, scale }
}
