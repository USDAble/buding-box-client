import { describe, it, expect, beforeEach, vi } from "vitest";
import { get } from "svelte/store";
import {
  productPhase,
  productState,
  windowToken,
  adoptWindowToken,
  windowTokenQuery,
  refreshProductState,
  logout,
  WINDOW_TOKEN_HEADER,
} from "./product";

// A fetch stand-in returning a JSON body at a fixed status. `body` is the raw
// object, so callers assert on what the client does with it (loggedIn, etc.).
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
  productPhase.set("unknown");
  productState.set(null);
  window.history.replaceState({}, "", "/");
  vi.unstubAllGlobals();
});

describe("windowToken / adoptWindowToken / windowTokenQuery", () => {
  it("windowToken returns null before any token is adopted", () => {
    expect(windowToken()).toBeNull();
  });

  it("adoptWindowToken moves the query param into sessionStorage and strips it from the URL", () => {
    const replace = vi.spyOn(history, "replaceState");
    window.history.pushState({}, "", "/?window_token=tok123&foo=bar");
    adoptWindowToken();

    expect(windowToken()).toBe("tok123");
    // window_token gone, unrelated params preserved.
    const params = new URLSearchParams(location.search);
    expect(params.get("window_token")).toBeNull();
    expect(params.get("foo")).toBe("bar");
    expect(replace).toHaveBeenCalledWith(null, "", "/?foo=bar");
  });

  it("adoptWindowToken is a no-op without the query param", () => {
    window.history.pushState({}, "", "/");
    adoptWindowToken();
    expect(windowToken()).toBeNull();
  });

  it("windowTokenQuery is empty without a token and a query with one", () => {
    expect(windowTokenQuery()).toBe("");
    sessionStorage.setItem("octo_window_token", "abc");
    expect(windowTokenQuery()).toBe("?window_token=abc");
  });
});

describe("refreshProductState", () => {
  it("short-circuits to ready without a window token (plain browser)", async () => {
    const fetchMock = fetchReturning(200, { loggedIn: false });
    vi.stubGlobal("fetch", fetchMock);

    await refreshProductState();

    expect(get(productPhase)).toBe("ready");
    // No token → no state round-trip; the gate is inert under `octo serve`.
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("sets ready when the window is logged in", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    const state = { schemaVersion: 1, loggedIn: true, activated: true, credits: { balance: 1, monthUsed: 0, monthKey: "" }, plan: { name: "" }, prefs: { locale: "", inputSensitiveCheck: false, defaultChatMode: "" } };
    vi.stubGlobal("fetch", fetchReturning(200, state));

    await refreshProductState();

    expect(get(productPhase)).toBe("ready");
    expect(get(productState)).toEqual(state);
  });

  it("sets blocked when the window is not logged in", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    vi.stubGlobal("fetch", fetchReturning(200, { schemaVersion: 1, loggedIn: false, activated: false, credits: { balance: 0, monthUsed: 0, monthKey: "" }, plan: { name: "" }, prefs: { locale: "", inputSensitiveCheck: false, defaultChatMode: "" } }));

    await refreshProductState();

    expect(get(productPhase)).toBe("blocked");
  });

  it("falls back to blocked when the state read fails", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    vi.stubGlobal("fetch", fetchReturning(500, {}));

    await refreshProductState();

    // A failed read must not brick the shell behind a spinner.
    expect(get(productPhase)).toBe("blocked");
  });
});

describe("logout", () => {
  it("posts with the token and flips the phase to blocked", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    const fetchMock = fetchReturning(200, {});
    vi.stubGlobal("fetch", fetchMock);

    await logout();

    expect(get(productPhase)).toBe("blocked");
    expect(get(productState)).toBeNull();
    expect(fetchMock).toHaveBeenCalledWith("/api/product/logout", {
      method: "POST",
      headers: { [WINDOW_TOKEN_HEADER]: "tok" },
    });
  });

  it("posts without a token header under a plain browser", async () => {
    const fetchMock = fetchReturning(200, {});
    vi.stubGlobal("fetch", fetchMock);

    await logout();

    expect(fetchMock).toHaveBeenCalledWith("/api/product/logout", {
      method: "POST",
      headers: {},
    });
  });
});
