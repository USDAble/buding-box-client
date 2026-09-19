// Navigation policy for the portable product form. Upstream views stay fully
// implemented and routable; this fork only decides which entries appear in the
// navigation. An explicit list means an upstream-added view stays hidden until
// the product deliberately gives it a user-facing entry.
//
// Every navigation decision reads this ONE module. Deleting views outright
// would turn every upstream change to them into a delete-vs-modify conflict;
// adding an approved entry here leaves the implementation and direct routes
// untouched.
//
// See the approved navigation and credits boundary §3.1-3.2.

/** Views with an approved navigation entry, in their callers' existing order. */
export const NAVIGATION_VIEWS = ['chat', 'tasks', 'agents', 'skills', 'workflows', 'browser'] as const

/** Reports whether a view belongs in navigation; it says nothing about routing. */
export function navigationVisible(v: string): boolean {
  return (NAVIGATION_VIEWS as readonly string[]).includes(v)
}

/**
 * Filters a nav array down to approved entries, preserving the caller's order.
 * Unknown views are intentionally hidden from navigation but remain routable.
 */
export function visibleNav<T extends { v: string }>(items: T[]): T[] {
  return items.filter((it) => navigationVisible(it.v))
}
