import { describe, expect, it } from 'vitest'
import { fitArtifactLayout, readArtifactLayout } from './artifact-layout'

describe('artifact layout fitting', () => {
  it('fits a clipped portrait poster in both side panel and maximized preview', () => {
    const poster = { width: 900, height: 1350, clipped: true }
    for (const [width, height] of [[416, 925], [1400, 800], [300, 300]]) {
      const fitted = fitArtifactLayout(width, height, poster)
      expect(fitted.width * fitted.scale).toBeLessThanOrEqual(width)
      expect(fitted.height * fitted.scale).toBeLessThanOrEqual(height)
      expect(fitted.width / fitted.height).toBeCloseTo(2 / 3)
    }
  })
  it('keeps long documents readable with vertical scrolling and responsive pages at full scale', () => {
    expect(fitArtifactLayout(400, 800, { width: 400, height: 10000, clipped: false })).toEqual({ width: 400, height: 800, scale: 1 })
    expect(fitArtifactLayout(400, 800, { width: 1000, height: 10000, clipped: false })).toEqual({ width: 1000, height: 2000, scale: 0.4 })
    expect(fitArtifactLayout(400, 800, null)).toEqual({ width: 400, height: 800, scale: 1 })
  })
  it('accepts only the active frame, its origin and current viewport, with bounded dimensions', () => {
    const frame = document.createElement('iframe'); document.body.appendChild(frame)
    const origin = 'http://preview.artifacts.localhost:8088'
    const data = { type: 'octo:artifact-layout', width: 900, height: 1350, viewportWidth: 416, clipped: true }
    const event = (patch = {}, props = {}) => new MessageEvent('message', { source: frame.contentWindow, origin, data: { ...data, ...patch }, ...props })
    expect(readArtifactLayout(event(), frame, origin, 416)).toEqual({ width: 900, height: 1350, clipped: true })
    for (const patch of [{ width: NaN }, { width: -1 }, { width: 32769 }, { height: Infinity }, { viewportWidth: 900 }, { clipped: 'true' }, { type: 'other' }]) {
      expect(readArtifactLayout(event(patch), frame, origin, 416)).toBeNull()
    }
    expect(readArtifactLayout(event({}, { source: window }), frame, origin, 416)).toBeNull()
    expect(readArtifactLayout(event({}, { origin: 'http://unrelated.localhost' }), frame, origin, 416)).toBeNull()
    frame.remove()
  })
})
