import { describe, it, expect } from "vitest";
import { randomNickname, validateNickname } from "./nickname";

describe("randomNickname", () => {
  it("generates 用户 + 4 digits in zh", () => {
    expect(randomNickname("zh")).toMatch(/^用户\d{4}$/);
  });

  it("generates User + 4 digits in en", () => {
    expect(randomNickname("en")).toMatch(/^User\d{4}$/);
  });
});

describe("validateNickname", () => {
  it("accepts Han, Latin, digits and underscore in 2–16 characters", () => {
    for (const ok of ["用户1234", "User", "_name_", "张三三", "aaaaaaaaaaaaaaaa", "字a1_"]) {
      expect(validateNickname(ok)).toBe("ok");
    }
  });

  it("rejects wrong length and disallowed characters", () => {
    for (const bad of ["a", "aaaaaaaaaaaaaaaaa", "用户-1", "用户 1", "🙂🙂"]) {
      expect(validateNickname(bad)).toBe("format");
    }
  });

  it("counts code points not bytes (16 CJK chars is legal)", () => {
    expect(validateNickname("布丁盒子布丁盒子布丁盒子布丁盒子")).toBe("ok");
  });
});
