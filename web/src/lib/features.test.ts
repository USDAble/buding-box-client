import { describe, it, expect } from 'vitest'
import { NAVIGATION_VIEWS, navigationVisible, visibleNav } from './features'

describe('navigationVisible', () => {
  it('shows the approved product entries', () => {
    for (const v of ['chat', 'tasks', 'agents', 'skills', 'workflows', 'browser']) {
      expect(navigationVisible(v)).toBe(true)
    }
  })

  it('keeps unsupported and future views out of navigation', () => {
    for (const v of ['channels', 'mcp', 'lightapps', 'future_view']) {
      expect(navigationVisible(v)).toBe(false)
    }
  })
})

describe('visibleNav', () => {
  it('filters unapproved views and preserves order', () => {
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

  it('defaults unknown views to hidden from navigation', () => {
    const items = [
      { v: 'future_view', label: 'Future' },
      { v: 'mcp', label: 'MCP' },
    ]
    expect(visibleNav(items)).toEqual([])
  })

  it('mirrors the NAVIGATION_VIEWS contract', () => {
    expect([...NAVIGATION_VIEWS]).toEqual(['chat', 'tasks', 'agents', 'skills', 'workflows', 'browser'])
  })
})
