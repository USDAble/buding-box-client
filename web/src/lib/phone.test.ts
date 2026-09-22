import { describe, it, expect } from "vitest";
import { normalizePhone, normalizePhoneWithCallingCode, splitPhoneForLocalAPI } from "./phone";

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

  it("combines a selected calling code while preserving pasted international numbers", () => {
    expect(normalizePhoneWithCallingCode('415 555 0123', '+1')).toEqual({ ok: true, value: '+14155550123' })
    expect(normalizePhoneWithCallingCode('13800001234', '+86')).toEqual({ ok: true, value: '+8613800001234' })
    expect(normalizePhoneWithCallingCode('86-138-0000-1234', '+86')).toEqual({ ok: true, value: '+8613800001234' })
    expect(normalizePhoneWithCallingCode('+44 20 7946 0958', '+86')).toEqual({ ok: true, value: '+442079460958' })
    expect(normalizePhoneWithCallingCode('123', '+1').ok).toBe(false)
  })

  it("splits known calling codes for the local API and preserves unknown E.164 paste", () => {
    const codes = ['+86', '+1', '+44']
    expect(splitPhoneForLocalAPI('+8613800001234', codes)).toEqual({ phone: '13800001234', region_code: '86' })
    expect(splitPhoneForLocalAPI('+14155550123', codes)).toEqual({ phone: '4155550123', region_code: '1' })
    expect(splitPhoneForLocalAPI('+442079460958', codes)).toEqual({ phone: '2079460958', region_code: '44' })
    expect(splitPhoneForLocalAPI('+33123456789', codes)).toEqual({ phone: '+33123456789' })
  })
});
