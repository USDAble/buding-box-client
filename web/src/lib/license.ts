// License expiry arithmetic for the account panel (P5). The requirement (需求
// §5.4.2) is explicit that "N days left" is a LOCAL calendar-day difference —
// not an hour difference and not UTC — so two instants on the same local date
// are zero days apart regardless of where they fall in the day, and a DST
// spring-forward cannot shave a day off a week-long expiry.

export interface LicenseView {
  state: "active" | "expired";
  /** Whole calendar days left, floored at 0. Active-today also reports 0. */
  daysLeft: number;
}

// startOfLocalDay returns the epoch ms of the local midnight that starts d's
// date. Constructing with the local calendar fields (not UTC) is what keeps
// the arithmetic timezone-safe: the caller's machine is the user's machine.
function startOfLocalDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

/**
 * Whole calendar days from `a` to `b` using each machine's local midnight
 * boundaries. A day boundary pair across DST is 23/25 hours apart, so the raw
 * millisecond difference is rounded rather than divided exactly.
 */
export function diffCalendarDays(a: Date, b: Date): number {
  return Math.round((startOfLocalDay(b) - startOfLocalDay(a)) / 86_400_000);
}

/**
 * The two-state license view the account panel renders. null when there is no
 * activation timestamp (should not happen on a logged-in panel — the main UI
 * only renders once activated — but a corrupt state file must not crash the
 * panel). `now` is injectable for tests.
 */
export function licenseView(expiresAt: string | null | undefined, now: Date = new Date()): LicenseView | null {
  if (!expiresAt) return null;
  const exp = new Date(expiresAt);
  if (Number.isNaN(exp.getTime())) return null;
  const state = exp.getTime() > now.getTime() ? "active" : "expired";
  const daysLeft = Math.max(0, diffCalendarDays(now, exp));
  return { state, daysLeft };
}
