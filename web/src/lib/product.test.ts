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
  noteSessionLost,
  blockedPage,
  failureTier,
  tierRetryable,
  WINDOW_TOKEN_HEADER,
} from "./product";
import type { ProductStateDTO } from "./product";

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
  blockedPage.set("login");
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
    // Named by URL, not by index: the control-plane probe is also a product call
    // and an index would silently start asserting on the wrong one.
    const call = fetchMock.mock.calls.find((c) => c[0] === "/api/product/state");
    expect(new Headers(call?.[1]?.headers).get(WINDOW_TOKEN_HEADER)).toBe("tok");
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
    const fetchMock = vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => ({
      ok: true,
      status: 200,
      statusText: "",
      json: async () =>
        String(input).includes("/logout")
          ? {}
          : { schemaVersion: 1, loggedIn: false, activated: true, credits: { balance: 0, monthUsed: 0, monthKey: "" }, plan: { name: "" }, prefs: { locale: "", inputSensitiveCheck: false, defaultChatMode: "" } },
    }));
    vi.stubGlobal("fetch", fetchMock);

    await logout();

    expect(get(productPhase)).toBe("blocked");
    expect(fetchMock).toHaveBeenCalledWith("/api/product/logout", {
      method: "POST",
      headers: { [WINDOW_TOKEN_HEADER]: "tok" },
    });
  });

  it("keeps the activation facts so the login page shows the short form", async () => {
    // The bug this pins (V-22): clearing the store left `activated` false, so
    // the blocked page rendered the ACTIVATION form after a logout. An
    // activation code can only be used once, so a single tap on logout left the
    // user with a form they could never satisfy.
    sessionStorage.setItem("octo_window_token", "tok");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => ({
        ok: true,
        status: 200,
        statusText: "",
        json: async () =>
          String(input).includes("/logout")
            ? {}
            : {
                schemaVersion: 1,
                loggedIn: false,
                activated: true,
                account: { phoneMasked: "138****1234", nickname: "tester", lastLoginAt: "" },
                credits: { balance: 0, monthUsed: 0, monthKey: "" },
                plan: { name: "" },
                prefs: { locale: "", inputSensitiveCheck: false, defaultChatMode: "" },
              },
      })),
    );

    await logout();

    const st = get(productState);
    expect(st?.activated).toBe(true);
    expect(st?.loggedIn).toBe(false);
    // The bound number has to survive too: it is what prefills the short form.
    expect(st?.account?.phoneMasked).toBe("138****1234");
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

describe("noteSessionLost", () => {
  it("returns to the blocked page on 401 and keeps the activation facts", () => {
    productPhase.set("ready");
    productState.set({
      schemaVersion: 1,
      loggedIn: true,
      activated: true,
      account: { phoneMasked: "138****1234", nickname: "tester", lastLoginAt: "" },
      credits: { balance: 0, monthUsed: 0, monthKey: "" },
      plan: { name: "" },
      prefs: { locale: "", inputSensitiveCheck: false, defaultChatMode: "" },
      suppressOnboarding: true,
    } as ProductStateDTO);

    expect(noteSessionLost(401)).toBe(true);

    expect(get(productPhase)).toBe("blocked");
    const st = get(productState);
    expect(st?.loggedIn).toBe(false);
    // Same reason as the logout case: the second login is the short form.
    expect(st?.activated).toBe(true);
  });

  it("ignores every status that is not 401", () => {
    // Clearing on an outage would turn a flaky network into a forced re-login.
    for (const status of [200, 403, 429, 500, 503]) {
      productPhase.set("ready");
      expect(noteSessionLost(status)).toBe(false);
      expect(get(productPhase)).toBe("ready");
    }
  });

  it("flips the phase when a product call is answered 401", async () => {
    // The funnel, not the 401 handler: this is what makes the rule reach a call
    // that nobody remembered to wire.
    sessionStorage.setItem("octo_window_token", "tok");
    productPhase.set("ready");
    vi.stubGlobal("fetch", fetchReturning(401, { code: "unauthorized" }));

    await refreshProductState();

    expect(get(productPhase)).toBe("blocked");
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

describe("blocked-page selection (L-B2)", () => {
  // The two misconfiguration pages exist so the user is told "this package is
  // wrong" BEFORE typing, instead of after a failed round trip. The four
  // outcomes and their priority are P4-拦截页 §2.2; the order is not decorative:
  // hasTrustedKeys answers before configured because "has an address but trusts
  // no key" has already passed the configuration check, so it is the later
  // failure of the two.
  function stubSequence(responses: Array<{ status: number; body: unknown }>) {
    let i = 0;
    const mock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => {
      const r = responses[Math.min(i++, responses.length - 1)];
      return {
        ok: r.status >= 200 && r.status < 300,
        status: r.status,
        statusText: "",
        json: async () => r.body,
      };
    });
    vi.stubGlobal("fetch", mock);
    return mock;
  }

  const stateBody = {
    schemaVersion: 1, loggedIn: false, activated: false,
    credits: { balance: 0, monthUsed: 0, monthKey: "" }, plan: { name: "" },
    prefs: { locale: "", inputSensitiveCheck: false, defaultChatMode: "" },
  };

  it("selects the unconfigured page when the build names no control plane", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    stubSequence([
      { status: 200, body: { configured: false, hasTrustedKeys: false } },
      { status: 200, body: stateBody },
    ]);

    await refreshProductState();

    expect(get(blockedPage)).toBe("unconfigured");
  });

  it("selects the no-keys page when a host exists but no key is trusted", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    stubSequence([
      { status: 200, body: { configured: true, hasTrustedKeys: false } },
      { status: 200, body: stateBody },
    ]);

    await refreshProductState();

    expect(get(blockedPage)).toBe("no_keys");
  });

  it("reports the unconfigured page when neither fact holds", async () => {
    // The correction TDD surfaced: the first draft checked hasTrustedKeys first
    // and so showed 「无公钥」 here. That page's claim is "this build has an
    // address but trusts no key" - false on a build that has no address. With
    // both missing, the address is the unmet precondition, so it is the one to
    // name: the key is the second step of a build stuck on the first.
    sessionStorage.setItem("octo_window_token", "tok");
    stubSequence([
      { status: 200, body: { configured: false, hasTrustedKeys: false } },
      { status: 200, body: stateBody },
    ]);

    await refreshProductState();

    expect(get(blockedPage)).toBe("unconfigured");
  });

  it("falls back to the login form when the control-plane read fails", async () => {
    // Bounded degradation (开发规范 §3.9): not knowing must NOT be reported as
    // "unconfigured" - that would accuse the package of a defect we have no
    // evidence for, and the user has nothing to fix.
    sessionStorage.setItem("octo_window_token", "tok");
    stubSequence([
      { status: 500, body: {} },
      { status: 200, body: stateBody },
    ]);

    await refreshProductState();

    expect(get(blockedPage)).toBe("login");
    expect(get(productPhase)).toBe("blocked");
  });

  it("falls back to the login form when the reply omits the facts", async () => {
    // A reply we cannot read is the same case as a reply we cannot get.
    sessionStorage.setItem("octo_window_token", "tok");
    stubSequence([
      { status: 200, body: { somethingElse: true } },
      { status: 200, body: stateBody },
    ]);

    await refreshProductState();

    expect(get(blockedPage)).toBe("login");
  });

  it("keeps the login form for a healthy build", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    stubSequence([
      { status: 200, body: { configured: true, hasTrustedKeys: true } },
      { status: 200, body: stateBody },
    ]);

    await refreshProductState();

    expect(get(blockedPage)).toBe("login");
  });

  it("skips the state read on a misconfigured build", async () => {
    // Nothing on those two pages reads productState, so the read would be a
    // round trip whose result is discarded.
    sessionStorage.setItem("octo_window_token", "tok");
    const mock = stubSequence([
      { status: 200, body: { configured: false, hasTrustedKeys: false } },
      { status: 200, body: stateBody },
    ]);

    await refreshProductState();

    expect(mock).toHaveBeenCalledTimes(1);
    expect(get(productPhase)).toBe("blocked");
  });

  it("does not probe the control plane outside the desktop shell", async () => {
    // A plain browser on `octo serve` has no window identity, so the product
    // gate does not exist for it and neither does this question.
    const mock = stubSequence([{ status: 200, body: {} }]);

    await refreshProductState();

    expect(mock).not.toHaveBeenCalled();
    expect(get(productPhase)).toBe("ready");
  });

  it("stamps the control-plane read with the window token", async () => {
    sessionStorage.setItem("octo_window_token", "tok");
    const mock = stubSequence([
      { status: 200, body: { configured: true, hasTrustedKeys: true } },
      { status: 200, body: stateBody },
    ]);

    await refreshProductState();

    const call = mock.mock.calls.find((c) => c[0] === "/api/product/control-plane");
    expect(call).toBeTruthy();
    expect(new Headers(call?.[1]?.headers).get(WINDOW_TOKEN_HEADER)).toBe("tok");
  });
});

describe("control-plane failure tiers (L-B3)", () => {
  it("recognises the four tiers and nothing else", () => {
    expect(failureTier("network_unavailable")).toBe("network_unavailable");
    expect(failureTier("upstream_unavailable")).toBe("upstream_unavailable");
    expect(failureTier("unauthorized")).toBe("unauthorized");
    expect(failureTier("account_restricted")).toBe("account_restricted");
    // Business and field codes are not tiers: the page renders them under an
    // input or in the banner, and calling them tiers would offer a pointless
    // retry for something the user can actually fix.
    expect(failureTier("invalid_code")).toBeNull();
    expect(failureTier("activation_invalid")).toBeNull();
    expect(failureTier(null)).toBeNull();
    expect(failureTier(undefined)).toBeNull();
  });

  it("marks only the outage tiers as retryable", () => {
    // The column that matters (P4-拦截页 §3.1): "can retry" and "clear the
    // credential" are different questions. A refused refresh token does not heal
    // by retrying, and a restricted account is not a lost session.
    expect(tierRetryable("network_unavailable")).toBe(true);
    expect(tierRetryable("upstream_unavailable")).toBe(true);
    expect(tierRetryable("unauthorized")).toBe(false);
    expect(tierRetryable("account_restricted")).toBe(false);
  });

  it("reports a refused send-code as a tier, not as a bad phone number", async () => {
    // The bug this pins: a 503 from the platform used to be filed under
    // fieldErrors.phone, so the user was told their number was wrong (and the
    // field-error switch had no case for it, rendering an EMPTY message).
    vi.stubGlobal("fetch", fetchReturning(503, { code: "network_unavailable" }));

    const err = await sendCode("13800001234").catch((e) => e);
    expect(err).toBeInstanceOf(ProductError);
    expect(err.code).toBe("network_unavailable");
    expect(err.fieldErrors.phone).toBeUndefined();
  });

  it("still files a genuinely bad phone number under the field", async () => {
    vi.stubGlobal("fetch", fetchReturning(400, { field: "phone", code: "invalid_phone" }));

    const err = await sendCode("123").catch((e) => e);
    expect(err.fieldErrors.phone).toBe("invalid_phone");
    expect(err.code).toBeNull();
  });
});
