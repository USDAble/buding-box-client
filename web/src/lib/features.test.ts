import { describe, it, expect } from 'vitest'
import { HIDDEN_VIEWS, viewHidden, visibleNav } from './features'

describe('viewHidden', () => {
  it('hides the three upstream capability views', () => {
    expect(viewHidden('channels')).toBe(true)
    expect(viewHidden('mcp')).toBe(true)
    expect(viewHidden('lightapps')).toBe(true)
  })

  it('leaves the product views visible', () => {
    for (const v of ['chat', 'agents', 'skills', 'workflows', 'browser', 'tasks']) {
      expect(viewHidden(v)).toBe(false)
    }
  })
})

describe('visibleNav', () => {
  it('filters hidden views and preserves order', () => {
    const items = [
      { v: 'tasks', label: 'Tasks' },
      { v: 'lightapps', label: 'Light Apps' },
      { v: 'chat', label: 'Chat' },
      { v: 'mcp', label: 'MCP' },
      { v: 'agents', label: 'Agents' },
    ]
    const out = visibleNav(items)
    expect(out.map((i) => i.v)).toEqual(['tasks', 'chat', 'agents'])
  })

  it('defaults unknown views to visible (blacklist semantics)', () => {
    const items = [
      { v: 'future_view', label: 'Future' },
      { v: 'mcp', label: 'MCP' },
    ]
    expect(visibleNav(items).map((i) => i.v)).toEqual(['future_view'])
  })

  it('mirrors the HIDDEN_VIEWS contract', () => {
    // Every hidden view must be exactly the upstream capability trio.
    expect([...HIDDEN_VIEWS].sort()).toEqual(['channels', 'lightapps', 'mcp'])
  })
})
