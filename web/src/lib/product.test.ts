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
  sendCode,
  login,
  setProductLocale,
  ProductError,
  WINDOW_TOKEN_HEADER,
} from "./product";

// A fetch stand-in returning a JSON body at a fixed status. `body` is the raw
// object, so callers assert on what the client does with it (loggedIn, etc.).
// The two parameters exist so `mock.calls[i][1]` stays typed as RequestInit and
// a test can read the request body it asserted on.
function fetchReturning(status: number, body: unknown) {
  return vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => ({
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

  it("stamps the state read with the window token so the gate lets it through", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    const fetchMock = fetchReturning(200, { loggedIn: false });
    vi.stubGlobal("fetch", fetchMock);

    await refreshProductState();

    // The gate refuses an unauthenticated window, so this call must carry the
    // token exactly like login/logout do. Without it the shell would 403 on its
    // own first request and sit on the login screen with no way forward.
    const init = fetchMock.mock.calls[0][1];
    expect(new Headers(init?.headers).get(WINDOW_TOKEN_HEADER)).toBe("tok");
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

describe("sendCode", () => {
  it("resolves cooldown seconds on success", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    const fetchMock = fetchReturning(200, { cooldownSec: 60 });
    vi.stubGlobal("fetch", fetchMock);

    await expect(sendCode("13800001234")).resolves.toBe(60);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/product/send-code",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("throws retryAfterSec on a too-soon resend", async () => {
    vi.stubGlobal("fetch", fetchReturning(429, { retryAfterSec: 42 }));

    const err = await sendCode("13800001234").catch((e) => e);
    expect(err).toBeInstanceOf(ProductError);
    expect(err.retryAfterSec).toBe(42);
  });

  it("throws invalid_phone in fieldErrors", async () => {
    vi.stubGlobal("fetch", fetchReturning(400, { field: "phone", code: "invalid_phone" }));

    const err = await sendCode("123").catch((e) => e);
    expect(err.fieldErrors.phone).toBe("invalid_phone");
  });
});

describe("login", () => {
  const stateDTO = {
    schemaVersion: 1, loggedIn: true, activated: true,
    credits: { balance: 1, monthUsed: 0, monthKey: "" }, plan: { name: "" },
    prefs: { locale: "", inputSensitiveCheck: false, defaultChatMode: "" },
  };

  it("stores the state and flips to ready on success", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    vi.stubGlobal("fetch", fetchReturning(200, { state: stateDTO }));

    await login({ phone: "13800001234", code: "123456", nickname: "用户1234", activationCode: "BUDING-DEMO-0001" });

    expect(get(productPhase)).toBe("ready");
    expect(get(productState)?.loggedIn).toBe(true);
  });

  it("throws fieldErrors from round-one format errors", async () => {
    vi.stubGlobal("fetch", fetchReturning(400, { fieldErrors: { phone: "invalid_phone", nickname: "nickname_format" } }));

    const err = await login({ phone: "1", code: "1", nickname: "a" }).catch((e) => e);
    expect(err.fieldErrors).toEqual({ phone: "invalid_phone", nickname: "nickname_format" });
  });

  it("throws the business code and masked phone from round two", async () => {
    vi.stubGlobal("fetch", fetchReturning(400, { code: "phone_mismatch", phoneMasked: "138****1234" }));

    const err = await login({ phone: "13900009999", code: "123456", nickname: "用户1234" }).catch((e) => e);
    expect(err.code).toBe("phone_mismatch");
    expect(err.phoneMasked).toBe("138****1234");
  });

  it("sends both activation credentials on first activation", async () => {
    const fetchMock = fetchReturning(200, { state: stateDTO });
    vi.stubGlobal("fetch", fetchMock);

    await login({
      phone: "13800001234",
      code: "123456",
      nickname: "用户1234",
      activationCode: "BUDING-DEMO-0001",
      boxCode: "BOX-DEMO-0001",
    });

    const body = JSON.parse(String(fetchMock.mock.calls[0][1]?.body));
    expect(body).toMatchObject({
      activationCode: "BUDING-DEMO-0001",
      boxCode: "BOX-DEMO-0001",
    });
  });

  it("carries the activation-family codes through unchanged", async () => {
    // The view maps each code to its own copy (需求基线 E1 rule 2), so the
    // client must not fold or rewrite them on the way out of `login`.
    for (const code of ["activation_invalid", "activation_code_used", "box_code_unknown", "box_code_mismatch"]) {
      vi.stubGlobal("fetch", fetchReturning(400, { code }));

      const err = await login({ phone: "13800001234", code: "123456", nickname: "用户1234" }).catch((e) => e);
      expect(err.code).toBe(code);
    }
  });
});

describe("setProductLocale", () => {
  it("PUTs the locale", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    const fetchMock = fetchReturning(200, { ok: true });
    vi.stubGlobal("fetch", fetchMock);

    await setProductLocale("zh");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/product/locale",
      expect.objectContaining({ method: "PUT", body: JSON.stringify({ locale: "zh" }) }),
    );
  });
});
