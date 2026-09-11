import { describe, it, expect } from "vitest";
import { normalizePhone } from "./phone";

describe("normalizePhone", () => {
  it("converges the accepted spellings on one canonical form", () => {
    for (const raw of ["138 0000 1234", "+8613800001234", "86-138-0000-1234", "86 138 0000 1234", "138-0000-1234"]) {
      expect(normalizePhone(raw)).toEqual({ ok: true, value: "13800001234" });
    }
  });

  it("rejects numbers that do not normalize to a 1-leading 11-digit string", () => {
    const bad = ["8613800001", "23800001234", "1380000123", "138000012345", "13a00001234", "", "+8613800001"];
    for (const raw of bad) {
      expect(normalizePhone(raw).ok).toBe(false);
    }
  });
});
