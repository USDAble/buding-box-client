// product.ts — the product gate's client half.
//
// The desktop shell mints an in-memory window token at startup and injects it
// into the webview URL (cmd/octo-desktop/bridge.go shellURL). This module
// adopts it into sessionStorage on first load (and strips it from the address
// bar), then every fetch from inside the window carries it back as a header so
// the server's product gate can tell "this window" apart from other loopback
// peers. A plain browser on `octo serve` never has a token, so the gate is
// inert and the phase stays "ready".

import { writable } from "svelte/store";

// The product gate distinguishes three frontend states: still deciding,
// not logged in (show the login gate — the UI lands in P4), and logged in.
export type ProductPhase = "unknown" | "blocked" | "ready";

export const productPhase = writable<ProductPhase>("unknown");
export const productState = writable<ProductStateDTO | null>(null);

// Header the server's product gate reads (internal/productgate/gate.go).
export const WINDOW_TOKEN_HEADER = "X-Octo-Window-Token";
// Query parameter the WebSocket upgrade uses (browser WS can't set headers).
export const WINDOW_TOKEN_QUERY = "window_token";

// sessionStorage — per-tab, matching the token's in-memory lifetime: a new
// window (or a shell restart) gets a fresh token, so it must not survive the
// tab, and localStorage would leak one tab's token into another.
const STORAGE_KEY = "octo_window_token";

// The de-identified product state served by GET /api/product/state. Mirrors
// the Go productstate.PublicState shape.
export interface ProductStateDTO {
  schemaVersion: number;
  loggedIn: boolean;
  activated: boolean;
  account?: {
    phoneMasked: string;
    nickname: string;
    lastLoginAt: string;
  } | null;
  credits: { balance: number; monthUsed: number; monthKey: string };
  plan: { name: string };
  prefs: {
    locale: string;
    inputSensitiveCheck: boolean;
    defaultChatMode: string;
  };
}

// windowToken returns the adopted window token, or null outside the desktop
// shell (a plain browser on `octo serve`).
export function windowToken(): string | null {
  return sessionStorage.getItem(STORAGE_KEY);
}

// adoptWindowToken moves the window_token query parameter into sessionStorage
// and clears it from the address bar, so the token never lingers in the
// webview history. Must run before any fetch, so call it at module top level.
export function adoptWindowToken(): void {
  const params = new URLSearchParams(location.search);
  const token = params.get(WINDOW_TOKEN_QUERY);
  if (!token) return;
  sessionStorage.setItem(STORAGE_KEY, token);
  params.delete(WINDOW_TOKEN_QUERY);
  const qs = params.toString();
  history.replaceState(null, "", location.pathname + (qs ? `?${qs}` : "") + location.hash);
}

// The WebSocket handshake can't carry a header, so the token rides the query
// string there — the same trick the access key uses upstream.
export function windowTokenQuery(): string {
  const token = windowToken();
  return token ? `?${WINDOW_TOKEN_QUERY}=${encodeURIComponent(token)}` : "";
}

// refreshProductState loads the (de-identified) state and derives the phase.
// Outside the desktop shell there is no gate, so the phase is ready outright.
export async function refreshProductState(): Promise<void> {
  if (!windowToken()) {
    productPhase.set("ready");
    return;
  }
  try {
    const res = await fetch("/api/product/state", { cache: "no-store" });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    const d = (await res.json()) as ProductStateDTO;
    productState.set(d);
    productPhase.set(d.loggedIn ? "ready" : "blocked");
  } catch {
    // A state read failure must not brick the shell: fall back to "blocked"
    // so the user can retry the login flow rather than staring at a spinner.
    productPhase.set("blocked");
  }
}

// logout clears the account server-side and flips the phase back to blocked.
export async function logout(): Promise<void> {
  const token = windowToken();
  const headers: Record<string, string> = {};
  if (token) headers[WINDOW_TOKEN_HEADER] = token;
  const res = await fetch("/api/product/logout", { method: "POST", headers });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  productState.set(null);
  productPhase.set("blocked");
}
