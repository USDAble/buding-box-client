import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { get } from "svelte/store";
import { applySensitiveRejection, checkSensitive } from "./sensitive";
import { WINDOW_TOKEN_HEADER, productState, productPhase } from "./product";
import { SRC, filesMentioning, stripComments } from "../test/sourceScan";

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

// V-98: the other half of the same gate. The server side has nails (a hit is
// refused, the masked text comes back as the payload, the session file never
// sees the message — PR-6b3), but the half that turns that event into "the
// masked text is back in the box and the user is told" had none: it lived in
// ChatView.svelte, a 3000-line view no test renders. So the server could emit a
// perfectly correct event while the view listened for the wrong name, or
// dropped the restore, and every nail stayed green — the shape L-D2 was burned
// by. The effects are injected into the function so that at least the decisions
// are reachable from a test; the scan below is what keeps the view using it.
describe("applySensitiveRejection", () => {
  function effects() {
    return { sessionID: "s1", restore: vi.fn(), notify: vi.fn() };
  }

  it("puts the masked text back and says why", () => {
    const t = effects();

    expect(applySensitiveRejection({ session_id: "s1", text: "增值税***管理" }, t)).toBe(true);
    expect(t.restore).toHaveBeenCalledWith("增值税***管理");
    expect(t.notify).toHaveBeenCalledTimes(1);
  });

  it("leaves a rejection addressed to another session alone", () => {
    const t = effects();

    expect(applySensitiveRejection({ session_id: "s2", text: "增值税***管理" }, t)).toBe(false);
    expect(t.restore).not.toHaveBeenCalled();
    expect(t.notify).not.toHaveBeenCalled();
  });

  it("empties the box when the event carries no text, rather than leaving the refused text in it", () => {
    const t = effects();

    applySensitiveRejection({ session_id: "s1" }, t);
    expect(t.restore).toHaveBeenCalledWith("");
  });
});

describe("the rejection's consumer half has one owner", () => {
  // A second consumer is not a style problem: two handlers for one event mean
  // two restores and two notices, and neither test above would notice.
  it("the event has exactly one consumer under web/src", () => {
    expect(filesMentioning("'input_sensitive'")).toEqual(["views/ChatView.svelte"]);
  });

  it("that consumer decides nothing itself — it only supplies the two effects", () => {
    const chatView = stripComments(
      readFileSync(join(SRC, "views", "ChatView.svelte"), "utf8"),
    );
    const start = chatView.indexOf("ws.on('input_sensitive'");
    const handler = chatView.slice(start, chatView.indexOf("}))", start));

    expect(start).toBeGreaterThan(-1);
    expect(handler).toContain("applySensitiveRejection(");
    // The addressee check and the empty-text fallback are the two decisions the
    // function owns. Seeing either spelled out again here means the view grew a
    // second copy of the behaviour, which is what makes the nails above stop
    // covering the real path.
    expect(handler).not.toContain("session_id");
    expect(handler).not.toContain("ev.text");
  });
});
