import { describe, it, expect } from "vitest";
import { diffCalendarDays, licenseView } from "./license";

// diffCalendarDays is LOCAL-calendar-day arithmetic, so the fixtures must be
// built from local calendar fields (new Date(y, m, d, h)) — a UTC-string
// fixture would make the test depend on the machine's timezone.
describe("diffCalendarDays", () => {
  it("is 0 for the same instant and for two instants on the same local date", () => {
    const noon = new Date(2026, 0, 15, 12, 0, 0);
    expect(diffCalendarDays(noon, new Date(2026, 0, 15, 0, 0, 1))).toBe(0);
    expect(diffCalendarDays(noon, new Date(2026, 0, 15, 23, 59, 59))).toBe(0);
  });

  it("counts one day for adjacent midnights", () => {
    expect(diffCalendarDays(new Date(2026, 0, 15, 23, 0), new Date(2026, 0, 16, 1, 0))).toBe(1);
  });

  it("is symmetric and signed", () => {
    const a = new Date(2026, 0, 15);
    const b = new Date(2026, 0, 20);
    expect(diffCalendarDays(a, b)).toBe(5);
    expect(diffCalendarDays(b, a)).toBe(-5);
  });

  it("survives a DST boundary (round-trips to whole days either way)", () => {
    // Covers a northern-hemisphere spring-forward and an autumn fall-back in
    // one span, from a date that must be in DST (Jul) to one that must not
    // (Jan) and back.
    const jan1 = new Date(2026, 0, 1, 12);
    const jul1 = new Date(2026, 6, 1, 12);
    expect(diffCalendarDays(jan1, jul1)).toBe(181); // 2026: 181 days Jan 1 → Jul 1
    expect(diffCalendarDays(jul1, jan1)).toBe(-181);
  });

  it("spans a month boundary", () => {
    expect(diffCalendarDays(new Date(2026, 0, 31), new Date(2026, 2, 1))).toBe(29);
  });
});

describe("licenseView", () => {
  const now = new Date(2026, 5, 15, 10, 0, 0); // Jun 15 2026 10:00 local

  it("returns null with no timestamp", () => {
    expect(licenseView(undefined, now)).toBeNull();
    expect(licenseView(null, now)).toBeNull();
  });

  it("returns null for an unparseable timestamp rather than crashing", () => {
    expect(licenseView("not-a-date", now)).toBeNull();
  });

  it("is active with the calendar days remaining", () => {
    const exp = new Date(2026, 5, 15 + 30, 12, 0, 0).toISOString();
    expect(licenseView(exp, now)).toEqual({ state: "active", daysLeft: 30 });
  });

  it("expiring today is active with 0 days left, never -0 or 1", () => {
    const exp = new Date(2026, 5, 15, 23, 59, 59).toISOString();
    const view = licenseView(exp, now);
    expect(view).toEqual({ state: "active", daysLeft: 0 });
    expect(Object.is(view!.daysLeft, -0)).toBe(false);
  });

  it("an instant in the past is expired with 0 days left", () => {
    const exp = new Date(2026, 5, 14, 23, 59, 59).toISOString();
    expect(licenseView(exp, now)).toEqual({ state: "expired", daysLeft: 0 });
  });

  it("an expiry long past clamps at 0 rather than going negative", () => {
    const exp = new Date(2025, 0, 1, 9).toISOString();
    expect(licenseView(exp, now)).toEqual({ state: "expired", daysLeft: 0 });
  });

  it("an expiry exactly now is expired (time-based, not day-based)", () => {
    expect(licenseView(now.toISOString(), now)).toEqual({ state: "expired", daysLeft: 0 });
  });
});
