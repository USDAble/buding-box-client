// product.ts — the product gate's client half.
//
// The desktop shell mints an in-memory window token at startup and injects it
// into the webview URL (cmd/octo-desktop/bridge.go shellURL). This module
// adopts it into sessionStorage on first load (and strips it from the address
// bar), then every fetch from inside the window carries it back as a header so
// the server's product gate can tell "this window" apart from other loopback
// peers. A plain browser on `octo serve` never has a token, so the gate is
// inert and the phase stays "ready".

import { get, writable } from "svelte/store";

// The product gate distinguishes three frontend states: still deciding,
// not logged in (show the login gate — the UI lands in P4), and logged in.
export type ProductPhase = "unknown" | "blocked" | "ready";

export const productPhase = writable<ProductPhase>("unknown");
export const productState = writable<ProductStateDTO | null>(null);

// Header the server's product gate reads (internal/server/server.go, windowAllowed).
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
  /** Activation timestamps (P5): present once activated, so the account
   *  panel can render "active · N days left". The activation code itself is
   *  server-side only and never appears here. */
  activation?: { activatedAt: string; expiresAt: string; boxCode?: string } | null;
  account?: {
    phoneMasked: string;
    nickname: string;
    lastLoginAt: string;
  } | null;
  /** The balance, and nothing else: the platform's ledger sends one number, and
   *  the client is forbidden from summing anything by month (E9 rule 5). The
   *  monthUsed/monthKey pair that used to sit here had no source and would have
   *  read 0 forever (V-28). It is written by exactly one call:
   *  refreshCredits() below. */
  credits: { balance: number };
  plan: { name: string };
  prefs: {
    locale: string;
    inputSensitiveCheck: boolean;
  };
  /** P9: desktop builds suppress the first-run "set up an API key" wizard (the
   *  window token is already wired to the product backend, so onboarding is a
   *  dead end). The server sets this from config; the Web UI routes away from
   *  key_setup when true. OCTO-FORK: P9. */
  suppressOnboarding: boolean;
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

/**
 * noteSessionLost returns the UI to the login page when the server answers 401
 * `unauthorized` — the platform refused our refresh token, so the session is
 * gone and only re-logging in can restore it (需求基线 E12, P4-拦截页 §4).
 *
 * It exists as one function because it is one rule, and it is checked in one
 * place (productFetch below, plus api.ts's request for the non-product calls):
 * a 401 the user is left sitting on is a dead interface whose every following
 * action fails the same way.
 *
 * Deliberately NOT "clear productState": the server keeps the activation record
 * and the bound number (E7), and the blocked page reads exactly those two fields
 * to show the short phone+code form instead of the activation form. Only
 * `loggedIn` is corrected, so the store stops claiming a session that is gone.
 *
 * No re-fetch of /api/product/state either: that call goes through this same
 * funnel, so fetching here would recurse.
 */
export function noteSessionLost(status: number): boolean {
  if (status !== 401) return false;
  productState.update((s) => (s ? { ...s, loggedIn: false } : s));
  productPhase.set("blocked");
  return true;
}

/**
 * productFetch is the single funnel for this module's product calls, mirroring
 * the server's failPlatform. Routing every call through it is what keeps the
 * session-lost rule from being missed by the next call someone adds.
 */
async function productFetch(path: string, init?: RequestInit): Promise<Response> {
  const res = await fetch(path, init);
  noteSessionLost(res.status);
  return res;
}

// ─── blocked-page selection (L-B2) ──────────────────────────────────────────

/**
 * Which wall the blocked phase shows. `login` is the ordinary login/activation
 * form; the other two are build defects the user cannot fix (P4-拦截页 §2).
 */
export type BlockedPage = "login" | "unconfigured" | "no_keys";

export const blockedPage = writable<BlockedPage>("login");
// OCTO-FORK: the settings/model UI consumes the build-profile capability from
// the server. null means the non-product/upstream endpoint is unavailable and
// preserves ordinary octo serve behavior.
export const allowEnvironmentModelSource = writable<boolean | null>(null);

/**
 * refreshControlPlane decides which blocked page the window shows, from the two
 * compile-time facts the profile reports (本地API契约 §2.13, P4-拦截页 §2.2).
 *
 * The priority is fixed and not interchangeable: `configured` is checked first
 * because the "no keys" page asserts "this build has an address but trusts no
 * signing key", and on a build that satisfies neither that sentence is simply
 * false. Reporting the later of the two failures would name a precondition whose
 * own precondition was never met, and send the user to fix the second step of a
 * build stuck on the first.
 *
 * Failing closed to the LOGIN FORM, not to a misconfiguration page: not knowing
 * is not evidence that the package is broken. Accusing it would tell the user to
 * replace a build that may be fine, and they have no way to find out (开发规范
 * §3.9 - every degradation names its target, and this one's target is "let them
 * retry the login").
 */
export async function refreshControlPlane(): Promise<void> {
  // Computed into a local and written once. Writing "login" at the start and
  // overwriting on success would leave the PREVIOUS answer in place whenever
  // this read fails - a stale "no keys" page is a lie about the build in front
  // of the user, and it would outlive the refresh that produced it.
  let page: BlockedPage = "login";
  let environmentModelSource: boolean | null = null;
  try {
    const res = await productFetch("/api/product/control-plane", {
      cache: "no-store",
      headers: windowTokenHeaders(),
    });
    // A non-200 (including 404 from a build without this endpoint) keeps the
    // login form, for the same reason as the catch below.
    if (res.ok) {
      const d = (await res.json()) as { configured?: boolean; hasTrustedKeys?: boolean; allowEnvironmentModelSource?: boolean };
      if (typeof d.allowEnvironmentModelSource === "boolean") environmentModelSource = d.allowEnvironmentModelSource;
      if (d.configured === false) page = "unconfigured";
      else if (d.hasTrustedKeys === false) page = "no_keys";
    }
  } catch {
    // Unreadable is the same case as unreachable: keep the login form.
  }
  blockedPage.set(page);
  allowEnvironmentModelSource.set(environmentModelSource);
}

// ─── control-plane failure tiers (L-B3) ─────────────────────────────────────/** The four control-plane failure tiers. See 本地API契约 §3 and P4-拦截页 §3. */
export type FailureTier =
  | "network_unavailable"
  | "upstream_unavailable"
  | "unauthorized"
  | "account_restricted";

// The tier set and its retryability. One table, one owner: the four codes are
// classified once on the server (internal/productclient, C12) and once here for
// rendering, and the frontend must not re-derive the classification from the
// HTTP status - a 503 covers two different tiers that read very differently to
// the user (P4-拦截页 §3.2).
const TIER_CODES: Record<string, true> = {
  network_unavailable: true,
  upstream_unavailable: true,
  unauthorized: true,
  account_restricted: true,
};

/** failureTier classifies a machine code, or null when it is not a tier. */
export function failureTier(code?: string | null): FailureTier | null {
  return code && code in TIER_CODES ? (code as FailureTier) : null;
}

/**
 * tierRetryable answers "is trying again useful" - a different question from
 * "does this clear the credential" (P4-拦截页 §3.1).
 *
 * A refused refresh token never heals by retrying, so `unauthorized` gets no
 * retry button; a restricted account is not a lost session, so it keeps its
 * credential. Offering a retry for either would promise the user something the
 * system cannot deliver (开发规范 §3.9).
 */
export function tierRetryable(tier: FailureTier): boolean {
  return tier === "network_unavailable" || tier === "upstream_unavailable";
}

// ─── catalogue availability (L-C2 / 需求基线 B4, B9) ────────────────────────

/**
 * The four availability states of the signed catalogue, mirroring
 * internal/productruntime's `catalogState` (本地API契约 §2.14).
 *
 * Three of them mean "no usable model list" for three different reasons, and
 * 需求基线 B9 requires a distinct sentence for each: a user who is told "check
 * your connection" when the real problem is a rejected signature will keep
 * checking their connection.
 */
export type CatalogState = "ready" | "absent" | "stale" | "unverifiable";

// Default "ready", not "absent". Before the first read we do not know, and not
// knowing is not evidence that the build is broken — the same rule
// refreshControlPlane follows when it fails closed to the login form rather than
// to a misconfiguration page. Outside the desktop shell there is no product layer
// at all, so it stays ready and nothing is blocked.
export const catalogState = writable<CatalogState>("ready");
/** Whether pressing "retry" is worth offering, straight from the server. */
export const catalogRetryable = writable<boolean>(false);

/**
 * refreshCatalogState reads the catalogue's availability.
 *
 * The read is also what renews a lapsed catalogue (需求基线 B3: "TTL 到期后下一次
 * 开选择器触发刷新"), so this is called when the picker opens and once at startup
 * for the composer's sake. It is deliberately NOT called before every send: a
 * round trip per message to re-learn a fact we just read would be a cost with no
 * answer attached, and the server is the one that must refuse a turn anyway
 * (that half lands with PR-5, reading this same state).
 *
 * A failed read keeps the previous value. Inventing "absent" would block a user
 * whose catalogue is fine, and inventing "ready" would be a promise about a
 * response we never got; keeping the last known answer is the only option that
 * claims nothing new (开发规范 §3.9).
 */
export async function refreshCatalogState(): Promise<void> {
  if (!windowToken()) {
    // No shell, no product layer: nothing decides a turn here.
    catalogState.set("ready");
    return;
  }
  try {
    const res = await productFetch("/api/product/catalog", {
      cache: "no-store",
      headers: windowTokenHeaders(),
    });
    // A non-200 (including 404 from a build without this endpoint) keeps the
    // previous answer, for the reason above.
    if (!res.ok) return;
    const d = (await res.json()) as { state?: string; retryable?: boolean };
    if (isCatalogState(d.state)) catalogState.set(d.state);
    catalogRetryable.set(d.retryable === true);
  } catch {
    // Unreadable is the same case as unreachable: keep what we had.
  }
}

/** isCatalogState narrows the wire string, so an unrecognised value cannot
 *  become a state the rest of the app has to defend against. */
export function isCatalogState(value?: string | null): value is CatalogState {
  return value === "ready" || value === "absent" || value === "stale" || value === "unverifiable";
}

// The pre-turn decision lives in modelAvailability.ts. This module owns only
// the control-plane facts it consumes.

// refreshProductState loads the (de-identified) state and derives the phase.
// Outside the desktop shell there is no gate, so the phase is ready outright.
export async function refreshProductState(): Promise<void> {
  if (!windowToken()) {
    productPhase.set("ready");
    return;
  }
  // Which wall to show is decided first, because the two misconfiguration pages
  // render no form and read no state - so on those builds the state round trip
  // below would be a request whose answer is thrown away.
  await refreshControlPlane();
  if (get(blockedPage) !== "login") {
    productPhase.set("blocked");
    return;
  }
  try {
    // The window token must ride this call too: the gate refuses an
    // unauthenticated /api request, and a 403 here would send the UI to the
    // login screen with no way through.
    const res = await productFetch("/api/product/state", { cache: "no-store", headers: windowTokenHeaders() });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    const d = (await res.json()) as ProductStateDTO;
    productState.set(d);
    productPhase.set(d.loggedIn ? "ready" : "blocked");
    if (d.loggedIn) {
      // Only once the workspace is reachable. On the login screen there is no
      // session, so the read would go out, be refused, and teach us nothing — and
      // the picker refreshes it again when it opens anyway (B3's "下一次开选择器
      // 触发刷新"), which is what keeps a send from having to ask synchronously.
      await refreshCatalogState();
    }
  } catch {
    // A state read failure must not brick the shell: fall back to "blocked"
    // so the user can retry the login flow rather than staring at a spinner.
    productPhase.set("blocked");
  }
}

// logout clears the login token server-side AND revokes the session on the
// platform (V-54), then flips the phase back to blocked. The bound
// phone/nickname stay in the data root so the second-login form can prefill and
// compare against them (需求 §5.3.4). The logout button itself is P5's; this
// helper is wired there.
//
// The returned `revoked` is the platform half of the answer. Deleting the local
// credential is what the user asked for and always happens; telling the platform
// is a second act that can fail on its own (PQ29 option 1, decided by a human
// 2026-09-14), and the caller must be able to say which of the two happened -
// a local-only logout that reports plain success is exactly the silent fallback
// 开发规范 §3.9 forbids. `false` is also what an older server answers by
// omission, which is the right reading: it did not revoke anything.
export interface LogoutResult {
  revoked: boolean
}

export async function logout(): Promise<LogoutResult> {
  const res = await productFetch("/api/product/logout", { method: "POST", headers: windowTokenHeaders() });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  const body = (await res.json().catch(() => null)) as { revoked?: boolean } | null;
  // Re-read the state instead of blanking the store. Logging out is not an
  // un-activation (E7): the box is still activated, and the blocked page reads
  // `activated` to choose between the short phone+code form and the activation
  // form. Blanking the store made it show the activation form - whose code is
  // one-shot, so a single tap on logout stranded the user with nothing left to
  // enter (V-22). The server owns the fact; this asks it again.
  await refreshProductState();
  return { revoked: body?.revoked === true };
}

// ─── Balance refresh ────────────────────────────────────────────────────────

/**
 * refreshCredits re-reads the balance from the platform and stores it.
 *
 * IT IS THE ONLY THING THAT WRITES `credits` (需求基线 E9 rule 2, decided by a
 * human 2026-09-14: "the server deducts the credits; the client just honestly
 * re-reads the balance from the server"). Every caller below is therefore a
 * TRIGGER, not a carrier: none of them knows the new number, they only decide
 * when to ask. That is what keeps "which of the two numbers wins" from being a
 * question, and why the gateway's terminal-frame shape (D-002) does not block
 * this path.
 *
 * The server answers with the whole state object, so the store is replaced by
 * the persisted truth rather than patched locally — the same rule the nickname
 * and prefs calls follow.
 *
 * A failure is thrown, never swallowed into a zero: the caller keeps whatever it
 * was showing, because an unread answer is not a statement about the user's
 * credits and 0 is the one number the UI must not invent.
 */
export async function refreshCredits(): Promise<number> {
  const res = await productFetch("/api/product/credits", { headers: windowTokenHeaders() });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  const body = (await res.json()) as { state: ProductStateDTO };
  productState.set(body.state);
  return body.state.credits.balance;
}

export interface FeedbackReceipt {
  feedbackId: string;
  acceptedAt: string;
}

// submitFeedback sends only the fields visible in the help form. The form owns
// its draft and retry key in memory; neither is persisted or supplemented with
// chat and diagnostic data here.
export async function submitFeedback(category: "bug" | "suggestion" | "other", content: string, idempotencyKey: string): Promise<FeedbackReceipt> {
  const res = await productFetch("/api/product/feedback", {
    method: "POST",
    headers: jsonHeaders(),
    body: JSON.stringify({ category, content, idempotencyKey }),
  });
  const body = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    throw new ProductError(res.status, (body.fieldErrors as Record<string, string>) ?? {}, (body.code as string) ?? null,
      (body.retryAfterSec as number) ?? null);
  }
  return body as unknown as FeedbackReceipt;
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
  return { "Content-Type": "application/json", ...windowTokenHeaders() };
}

// windowTokenHeaders returns the product gate's header for this window, or an
// empty object outside the shell. Once a token exists, every request the window
// makes to /api must carry it: the gate cannot tell this window from any other
// loopback caller, so a call that forgets the header is refused (403
// product_gate) exactly as an outside process would be.
function windowTokenHeaders(): Record<string, string> {
  const token = windowToken();
  return token ? { [WINDOW_TOKEN_HEADER]: token } : {};
}

/**
 * Login form input. First activation submits five fields: phone, code,
 * nickname, activationCode and boxCode (需求基线 E1). boxCode answers "which
 * box does this licence belong to" while activationCode answers "was this
 * licence paid for" — the server validates them independently, one box code
 * may pair with several activation codes, and an activation code is one-shot.
 * Both are omitted on the second login.
 */
export interface LoginInput {
  phone: string;
  code: string;
  nickname: string;
  activationCode?: string;
  boxCode?: string;
}

/**
 * Requests a verification code. Resolves with the cooldown seconds; throws
 * ProductError with fieldErrors.phone === "invalid_phone" or retryAfterSec on
 * a too-soon resend (429).
 *
 * Every other refusal carries a business code and no field (V-94): a platform
 * fault is not something the user can fix in the phone box.
 */
export async function sendCode(phone: string): Promise<number> {
  const res = await productFetch("/api/product/send-code", {
    method: "POST",
    headers: jsonHeaders(),
    body: JSON.stringify({ phone }),
  });
  const body = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    if (res.status === 429) {
      throw new ProductError(res.status, {}, null, body.retryAfterSec as number | null);
    }
    const code = (body.code as string) ?? "invalid_phone";
    // A control-plane tier is not a phone problem. Filing a 503 under the phone
    // field told the user their number was wrong, and the field-error switch has
    // no case for it, so the message rendered as an empty paragraph (L-B3). It
    // goes through the business channel instead, where the page can offer the
    // retry that is actually the right next step.
    //
    // ONLY A CODE THAT NAMES THE PHONE BELONGS UNDER THE FIELD (V-94). The tier
    // test above closed the 503 case that was reported, but every other code the
    // platform can answer with still fell through to the phone box — and
    // `internal_error` / `maintenance` / `control_plane_unconfigured` have no
    // field-error case either, so the same empty paragraph came back the moment
    // the platform answered 5xx. Membership is by name, not by "not a tier":
    // the field-error switch is the same closed list, and anything outside it
    // has to reach a surface that renders a sentence.
    if (failureTier(code) || !(code in PHONE_FIELD_CODES)) {
      throw new ProductError(res.status, {}, code);
    }
    throw new ProductError(res.status, { phone: code });
  }
  return body.cooldownSec as number;
}

/**
 * The codes that belong under the phone input on this call.
 *
 * `invalid_phone` is the only one: it is the one input this endpoint has, and the
 * local handler answers it as a business-level code precisely so the frontend can
 * file it (本地API契约 §2.2). The fallback in sendCode above covers the field-error
 * envelope, whose code is not in the body.
 */
const PHONE_FIELD_CODES: Record<string, true> = { invalid_phone: true };

/**
 * Submits the login (or activation+login) form. On success it updates the
 * product stores and flips the phase to ready; on failure it throws a
 * ProductError carrying either fieldErrors (round-one format) or a business
 * code + phoneMasked (round two).
 */
export interface DictionaryNotice {
  state: "degraded" | "recovered";
  fallbackVersion: string;
  retryAt?: "next_login";
  version?: string;
}

export interface LoginResult {
  state: ProductStateDTO;
  dictionaryNotice?: DictionaryNotice;
}

export async function login(input: LoginInput): Promise<LoginResult> {
  const res = await productFetch("/api/product/login", {
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
  return { state, dictionaryNotice: body.dictionaryNotice as DictionaryNotice | undefined };
}

/** Persists the UI language before login (PUT /api/product/locale). */
export async function setProductLocale(locale: "zh" | "en"): Promise<void> {
  const res = await productFetch("/api/product/locale", {
    method: "PUT",
    headers: jsonHeaders(),
    body: JSON.stringify({ locale }),
  });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
}

// ─── P5 account panel ───────────────────────────────────────────────────────
//
// Nickname and preference edits from the account panel. Both PUTs return the
// refreshed state (the server re-reads its own store, so the reply IS the
// persisted truth) and both update the shared productState store, which makes
// the bottom-left corner change on the same round-trip the panel's form makes.

export interface AccountPrefs {
  locale?: "zh" | "en";
  /** Input sensitive-word check toggle (P8); omitted means "leave unchanged". */
  inputSensitiveCheck?: boolean;
}

/**
 * Saves a nickname edit (PUT /api/product/nickname). Throws ProductError with
 * `code` = "nickname_format" | "nickname_sensitive" on a refusal; the panel
 * maps those straight to field errors, same machine codes as the login form.
 */
export async function updateNickname(nickname: string): Promise<ProductStateDTO> {
  const res = await productFetch("/api/product/nickname", {
    method: "PUT",
    headers: jsonHeaders(),
    body: JSON.stringify({ nickname }),
  });
  const body = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    throw new ProductError(res.status, {}, (body.code as string) ?? "nickname_format");
  }
  const state = body.state as ProductStateDTO;
  productState.set(state);
  return state;
}

/**
 * Saves preference edits (PUT /api/product/prefs). Unchanged fields may be
 * omitted. Throws ProductError with
 * `code` = "invalid_value" on an unknown value, with the offending field name
 * travelling inside the ProductError's fieldErrors map (product.ts builds it from
 * body.field) — the same refusal shape PUT /api/product/locale answers with.
 */
export async function updatePrefs(prefs: AccountPrefs): Promise<ProductStateDTO> {
  const res = await productFetch("/api/product/prefs", {
    method: "PUT",
    headers: jsonHeaders(),
    body: JSON.stringify(prefs),
  });
  const body = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    const field = (body.field as string) ?? "prefs";
    throw new ProductError(res.status, { [field]: (body.code as string) ?? "invalid_value" });
  }
  const state = body.state as ProductStateDTO;
  productState.set(state);
  return state;
}
