import { describe, it, expect, beforeEach, vi } from "vitest";
import { checkSensitive } from "./sensitive";
import { WINDOW_TOKEN_HEADER } from "./product";

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
    expect((init.headers as Record<string, string>)[WINDOW_TOKEN_HEADER]).toBe("tok");
  });
});
