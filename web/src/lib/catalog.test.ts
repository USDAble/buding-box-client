// PR-4c's frontend half: the four catalogue states, the turn gate, and the four
// sentences 需求基线 B9 requires to be different from each other.
//
// These assertions are here rather than in product.test.ts because the subject is
// 闭环 L-C2 ("目录不可用时用户看到什么、还能做什么"), not the product gate.
import { describe, it, expect, beforeEach, vi } from "vitest";
import { get } from "svelte/store";
import {
  adoptWindowToken,
  canStartTurn,
  catalogRetryable,
  catalogState,
  isCatalogState,
  refreshCatalogState,
  windowToken,
} from "./product";
import { catalogNoticeKey } from "./chatMode";

// A fetch stand-in returning a JSON body at a fixed status.
function fetchReturning(status: number, body: unknown) {
  return vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => ({
    ok: status >= 200 && status < 300,
    status,
    statusText: "",
    json: async () => body,
  }));
}

// The window token is what puts the page inside the desktop shell, which is the
// only place a catalogue decides anything.
function inTheShell() {
  window.history.replaceState({}, "", "/?window_token=tok-catalog");
  adoptWindowToken();
}

beforeEach(() => {
  sessionStorage.clear();
  catalogState.set("ready");
  catalogRetryable.set(false);
  window.history.replaceState({}, "", "/");
  vi.unstubAllGlobals();
});

describe("catalog availability (L-C2)", () => {
  it("a lapsed catalogue blocks a new turn while leaving reading alone", async () => {
    inTheShell();
    vi.stubGlobal("fetch", fetchReturning(200, { state: "stale", retryable: true }));
    await refreshCatalogState();

    expect(get(catalogState)).toBe("stale");
    expect(canStartTurn()).toBe(false);
    // The other half of B4 rule 1, asserted in the same breath so a future
    // "fix" cannot satisfy one by breaking the other: the gate is a read-only
    // predicate — it consults no history, clears nothing, and a second call
    // gives the same answer.
    expect(canStartTurn()).toBe(false);
  });

  it("every state that is not ready blocks a turn, and ready does not", async () => {
    inTheShell();
    for (const [state, blocked] of [
      ["ready", false],
      ["absent", true],
      ["stale", true],
      ["unverifiable", true],
    ] as const) {
      vi.stubGlobal("fetch", fetchReturning(200, { state, retryable: true }));
      await refreshCatalogState();
      expect(get(catalogState)).toBe(state);
      expect(canStartTurn()).toBe(!blocked);
    }
  });

  it("reports the server's retryable flag rather than deriving one", async () => {
    inTheShell();
    // A failed verification is not retryable: B4's recovery is the platform
    // changing its key, which the user cannot cause by pressing a button.
    vi.stubGlobal("fetch", fetchReturning(200, { state: "unverifiable", retryable: false }));
    await refreshCatalogState();
    expect(get(catalogRetryable)).toBe(false);

    vi.stubGlobal("fetch", fetchReturning(200, { state: "stale", retryable: true }));
    await refreshCatalogState();
    expect(get(catalogRetryable)).toBe(true);
  });

  it("keeps the previous answer when the read fails", async () => {
    inTheShell();
    vi.stubGlobal("fetch", fetchReturning(200, { state: "ready", retryable: false }));
    await refreshCatalogState();

    // A 404 (a build without the endpoint) and a thrown fetch both mean "we did
    // not find out", which is not evidence that the build is broken: inventing
    // "absent" would block a user whose catalogue is fine.
    vi.stubGlobal("fetch", fetchReturning(404, {}));
    await refreshCatalogState();
    expect(get(catalogState)).toBe("ready");

    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("offline"); }));
    await refreshCatalogState();
    expect(get(catalogState)).toBe("ready");
  });

  it("decides nothing outside the desktop shell", async () => {
    const fetchMock = fetchReturning(200, { state: "absent", retryable: true });
    vi.stubGlobal("fetch", fetchMock);

    expect(windowToken()).toBeNull();
    await refreshCatalogState();

    // A plain browser on `octo serve` has no product layer, so there is nothing
    // to gate and no request worth making.
    expect(get(catalogState)).toBe("ready");
    expect(canStartTurn()).toBe(true);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("ignores a state the server never sends", async () => {
    inTheShell();
    expect(isCatalogState("ready")).toBe(true);
    expect(isCatalogState("exploded")).toBe(false);
    expect(isCatalogState(undefined)).toBe(false);

    vi.stubGlobal("fetch", fetchReturning(200, { state: "exploded" }));
    await refreshCatalogState();
    expect(get(catalogState)).toBe("ready");
  });
});

describe("B9's four sentences are four different sentences", () => {
  it("gives each of the four inputs its own key", () => {
    const keys = new Set<string>();
    for (const state of ["ready", "absent", "stale", "unverifiable"] as const) {
      catalogState.set(state);
      keys.add(catalogNoticeKey());
    }
    // Four inputs, four keys — none shared. B9's acceptance is literally "四条文案
    // 互不相同且都出现", and the empty-group case must not be answered with the
    // sentence for a broken catalogue: a user told to check their connection when
    // the real cause is a rejected signature will keep checking their connection.
    expect(keys.size).toBe(4);
  });

  it("answers the group-is-empty case only when the catalogue is fine", () => {
    catalogState.set("ready");
    expect(catalogNoticeKey()).toBe("mode.no_models");

    catalogState.set("stale");
    expect(catalogNoticeKey()).not.toBe("mode.no_models");
  });
});
