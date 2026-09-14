import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { get } from "svelte/store";
import { checkSensitive } from "./sensitive";
import { WINDOW_TOKEN_HEADER, productState, productPhase } from "./product";

function fetchReturning(status: number, body: unknown) {
  return vi.fn(async () => ({
    ok: status >= 200 && status < 300,
    status,
    statusText: "",
    json: async () => body,
  }));
}

beforeEach(() => {
  sessionStorage.clear();
  vi.unstubAllGlobals();
});

afterEach(() => {
  productState.set(null);
  productPhase.set("unknown");
});

describe("checkSensitive", () => {
  it("reports a hit with the masked text", async () => {
    const fetchMock = fetchReturning(200, { hit: true, masked: "增值税***管理" });
    vi.stubGlobal("fetch", fetchMock);
    sessionStorage.setItem("octo_window_token", "tok");

    const res = await checkSensitive("增值税发票管理");
    expect(res.hit).toBe(true);
    expect(res.masked).toBe("增值税***管理");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/product/sensitive/check",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ text: "增值税发票管理" }),
      }),
    );
  });

  it("reports a miss", async () => {
    vi.stubGlobal("fetch", fetchReturning(200, { hit: false }));
    const res = await checkSensitive("你好");
    expect(res.hit).toBe(false);
    expect(res.masked).toBe("");
  });

  it("carries the window token header when present", async () => {
    const fetchMock = fetchReturning(200, { hit: false });
    vi.stubGlobal("fetch", fetchMock);
    sessionStorage.setItem("octo_window_token", "tok");

    await checkSensitive("hi");
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    // request() builds the header set as a Headers object, not a plain record.
    expect((init.headers as Headers).get(WINDOW_TOKEN_HEADER)).toBe("tok");
  });

  // PR-6c 判据 7：输入检测 401 不得静默失败 —— 会话被撤销时，改昵称会回登录页，
  // 输入检测也必须回登录页（noteSessionLost），而不是 Composer 吞掉错误继续发。
  // 反钉：改走 request 之前这里是静默的（raw fetch 不触发 noteSessionLost）。
  it("401 returns the window to the login page instead of failing silently", async () => {
    productState.set({ loggedIn: true } as never);
    productPhase.set("unknown");
    vi.stubGlobal("fetch", fetchReturning(401, {}));

    await expect(checkSensitive("hi")).rejects.toThrow();
    expect(get(productState)?.loggedIn).toBe(false);
    expect(get(productPhase)).toBe("blocked");
  });
});
