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

// logout clears the login token server-side and flips the phase back to
// blocked. The bound phone/nickname stay in the data root so the second-login
// form can prefill and compare against them (需求 §5.3.4). The logout button
// itself is P5's; this helper is wired there.
export async function logout(): Promise<void> {
  const token = windowToken();
  const headers: Record<string, string> = {};
  if (token) headers[WINDOW_TOKEN_HEADER] = token;
  const res = await fetch("/api/product/logout", { method: "POST", headers });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  productState.set(null);
  productPhase.set("blocked");
}

// ─── P4 login form ──────────────────────────────────────────────────────────
//
// send-code / login / locale round-trips for the blocked (login) page. Errors
// come back as machine codes, never copy — the view renders the message in the
// current language so a zh→en switch never leaks a hardcoded string.

/** Error thrown by the product API calls, carrying the machine-code payloads. */
export class ProductError extends Error {
  constructor(
    public status: number,
    public fieldErrors: Record<string, string> = {},
    public code: string | null = null,
    public retryAfterSec: number | null = null,
    public phoneMasked: string | null = null,
  ) {
    super(`product api ${status}`);
    this.name = "ProductError";
  }
}

function jsonHeaders(): Record<string, string> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  const token = windowToken();
  if (token) headers[WINDOW_TOKEN_HEADER] = token;
  return headers;
}

/** Login form input; activationCode is required on first activation only. */
export interface LoginInput {
  phone: string;
  code: string;
  nickname: string;
  activationCode?: string;
}

/**
 * Requests a verification code. Resolves with the cooldown seconds; throws
 * ProductError with fieldErrors.phone === "invalid_phone" or retryAfterSec on
 * a too-soon resend (429).
 */
export async function sendCode(phone: string): Promise<number> {
  const res = await fetch("/api/product/send-code", {
    method: "POST",
    headers: jsonHeaders(),
    body: JSON.stringify({ phone }),
  });
  const body = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    if (res.status === 429) {
      throw new ProductError(res.status, {}, null, body.retryAfterSec as number | null);
    }
    throw new ProductError(res.status, { phone: (body.code as string) ?? "invalid_phone" });
  }
  return body.cooldownSec as number;
}

/**
 * Submits the login (or activation+login) form. On success it updates the
 * product stores and flips the phase to ready; on failure it throws a
 * ProductError carrying either fieldErrors (round-one format) or a business
 * code + phoneMasked (round two).
 */
export async function login(input: LoginInput): Promise<ProductStateDTO> {
  const res = await fetch("/api/product/login", {
    method: "POST",
    headers: jsonHeaders(),
    body: JSON.stringify(input),
  });
  const body = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    const fieldErrors = body.fieldErrors as Record<string, string> | undefined;
    if (fieldErrors) throw new ProductError(res.status, fieldErrors);
    throw new ProductError(
      res.status,
      {},
      (body.code as string) ?? null,
      null,
      (body.phoneMasked as string) ?? null,
    );
  }
  const state = body.state as ProductStateDTO;
  productState.set(state);
  productPhase.set(state.loggedIn ? "ready" : "blocked");
  return state;
}

/** Persists the UI language before login (PUT /api/product/locale). */
export async function setProductLocale(locale: "zh" | "en"): Promise<void> {
  const res = await fetch("/api/product/locale", {
    method: "PUT",
    headers: jsonHeaders(),
    body: JSON.stringify({ locale }),
  });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
}
