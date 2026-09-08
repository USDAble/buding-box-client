import { describe, it, expect } from "vitest";
import { avatarInitial, avatarColor, avatarColorIndex } from "./avatar";

describe("avatarInitial", () => {
  it("keeps a leading Han glyph as-is", () => {
    expect(avatarInitial("用户1234")).toBe("用");
  });

  it("uppercases a leading Latin letter", () => {
    expect(avatarInitial("user1234")).toBe("U");
  });

  it("leaves a leading digit alone", () => {
    expect(avatarInitial("138_abc")).toBe("1");
  });

  it("renders a leading emoji as itself instead of crashing", () => {
    expect(avatarInitial("😀abc")).toBe("😀");
  });

  it("falls back to ? on an empty / whitespace nickname", () => {
    expect(avatarInitial("")).toBe("?");
    expect(avatarInitial("   ")).toBe("?");
  });
});

describe("avatarColor", () => {
  it("is stable for the same nickname", () => {
    expect(avatarColor("用户1234")).toBe(avatarColor("用户1234"));
    expect(avatarColorIndex("用户1234")).toBe(avatarColorIndex("用户1234"));
  });

  it("stays within the palette", () => {
    for (const name of ["用户1234", "user1234", "布丁", "abc_123", "x"]) {
      const idx = avatarColorIndex(name);
      expect(idx).toBeGreaterThanOrEqual(0);
      expect(idx).toBeLessThan(6);
    }
  });
});
