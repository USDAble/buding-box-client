// Feature switches for the portable product form. Upstream ships IM channels,
// MCP servers and Light Apps as first-class views; this fork keeps the code
// (需求 §7: do not delete upstream capability) but removes the UI reach into
// them, because they present config surfaces that cannot actually be used in
// this phase (需求 §5.4.3).
//
// Every hide decision reads this ONE module. Deleting the views outright would
// turn every upstream change to them into a delete-vs-modify conflict (the
// hardest git class to auto-resolve); a switch array is a one-line revert when
// the feature is later opened up.
//
// See dev-docs-usdable/需求/2260906/技术方案/P6-入口隐藏与积分.md §3.1-3.2.

/** Views the portable form hides from navigation (blacklist semantics). */
export const HIDDEN_VIEWS = ['channels', 'mcp', 'lightapps'] as const

/** Reports whether a view name is hidden from the UI. */
export function viewHidden(v: string): boolean {
  return (HIDDEN_VIEWS as readonly string[]).includes(v)
}

/**
 * Filters a nav array down to visible entries, preserving order. Unknown
 * entries pass through (blacklist, not allowlist): an upstream-added view
 * should be visible unless we explicitly list it here.
 */
export function visibleNav<T extends { v: string }>(items: T[]): T[] {
  return items.filter((it) => !viewHidden(it.v))
}
