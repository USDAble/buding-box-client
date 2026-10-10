import { describe, it, expect } from "vitest";
import { normalizePhone, normalizePhoneParts } from "./phone";

describe("normalizePhone", () => {
  it("converges the accepted spellings on one canonical form", () => {
    for (const raw of ["138 0000 1234", "+8613800001234", "86-138-0000-1234", "86 138 0000 1234", "138-0000-1234"]) {
      expect(normalizePhone(raw)).toEqual({ ok: true, value: "+8613800001234" });
    }
    expect(normalizePhone('+1 (415) 555-0123')).toEqual({ ok: true, value: '+14155550123' })
    expect(normalizePhone('＋４４ ２０ ７９４６ ０９５８')).toEqual({ ok: true, value: '+442079460958' })
  });

  it("rejects malformed international numbers", () => {
    const bad = ["13a00001234", "", "+123", "+012345678"];
    for (const raw of bad) {
      expect(normalizePhone(raw).ok).toBe(false);
    }
  });
});

describe("normalizePhoneParts", () => {
  it("uses the selected country code for both domestic and international login", () => {
    expect(normalizePhoneParts('+86', '138 0000 1234')).toEqual({ ok: true, value: '+8613800001234' })
    expect(normalizePhoneParts('+852', '9123 4567')).toEqual({ ok: true, value: '+85291234567' })
    expect(normalizePhoneParts('+44', '2079460958')).toEqual({ ok: true, value: '+442079460958' })
  })
  it('accepts full-width input and complete numbers matching the selected calling code', () => {
    for (const raw of ['１３８ ００００ １２３４', '+86 138-0000-1234', '＋８６ １３８００００１２３４', '86 13800001234']) {
      expect(normalizePhoneParts('+86', raw)).toEqual({ ok: true, value: '+8613800001234' })
    }
    expect(normalizePhoneParts('+1', '+1 (415) 555-0123')).toEqual({ ok: true, value: '+14155550123' })
    expect(normalizePhoneParts('+852', '＋８５２ ９１２３ ４５６７')).toEqual({ ok: true, value: '+85291234567' })
  })
  it("rejects a mainland number under an international prefix or a malformed country code", () => {
    expect(normalizePhoneParts('+86', '12345678').ok).toBe(false)
    expect(normalizePhoneParts('+852', '+861380001234').ok).toBe(false)
    expect(normalizePhoneParts('+86;1', '13800001234').ok).toBe(false)
  })
})
