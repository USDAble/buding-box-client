// Package productgate is the product gate: a middleware that blocks the
// desktop window (and the requests it originates) from using the product while
// not logged in. It sits beside — never inside — the upstream access-key
// machine gate (internal/server requireAuth): the machine gate answers "is
// this a client on this machine / carrying the key", the product gate answers
// "is the logged-in window allowed to chat".
//
// Identification is a process-in-memory window token: the desktop process
// mints a random token at startup, injects it into the webview URL, and hands
// it here. A request carrying the token is a window request and must be
// logged in; a request without one is not the window (a CLI, VS Code, Obsidian,
// or another loopback process) and passes — the requirement's own boundary
// (需求 §9.8), not a hole. The token is memory-only, so there is nothing to
// revoke: a process restart mints a fresh one.
//
// See dev-docs-usdable/需求/2260906/技术方案/P3-登录态与产品门.md §3.1.
package productgate

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/open-octo/octo-agent/internal/productstate"
)

// HeaderWindowToken is the request header the frontend stamps on every API
// call it makes from inside the desktop webview.
const HeaderWindowToken = "X-Octo-Window-Token"

// QueryWindowToken is the query parameter the frontend uses on the WebSocket
// upgrade, where the browser API cannot set custom headers.
const QueryWindowToken = "window_token"

// Gate is the product gate. A nil store is never passed; a nil-able guard
// would silently fail open.
type Gate struct {
	token string
	store *productstate.Store
}

// New builds a gate. windowToken is the desktop process's in-memory token;
// empty means there is no desktop window (plain `octo serve`), in which case
// the gate passes everything through — there is nothing to block.
func New(windowToken string, st *productstate.Store) *Gate {
	return &Gate{token: windowToken, store: st}
}

// tokenFromRequest extracts the presented window token: the header on every
// route, plus the query parameter on /ws (the browser WebSocket API cannot set
// headers, mirroring the access-key query parameter upstream).
func tokenFromRequest(r *http.Request) string {
	if t := r.Header.Get(HeaderWindowToken); t != "" {
		return t
	}
	if r.URL.Path == "/ws" {
		return r.URL.Query().Get(QueryWindowToken)
	}
	return ""
}

// Allow reports whether a request may proceed. It is the single decision point
// shared by the HTTP middleware and the WebSocket upgrader (which does not run
// through the ordinary middleware chain). ok=false carries a reason string for
// the caller to embed in the 403 body.
func (g *Gate) Allow(r *http.Request) (ok bool, reason string) {
	if g == nil || g.store == nil || g.token == "" {
		return true, ""
	}
	presented := tokenFromRequest(r)
	if presented == "" {
		// Not the window (a CLI / VS Code / Obsidian / loopback peer): pass.
		return true, ""
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(g.token)) != 1 {
		// A stale or forged token. Carrying one marks the request as a window
		// request, so a mismatch is refused rather than treated as "not the
		// window".
		return false, "bad_token"
	}
	if g.store.LoggedIn() {
		return true, ""
	}
	return false, "not_logged_in"
}

// WriteDenied writes the standard 403 product-gate response body. Shared by
// the middleware and the WebSocket upgrader so both surfaces carry the same
// machine-readable shape the frontend keys on ("product_gate" + reason).
func WriteDenied(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "product_gate",
		"reason": reason,
	})
}

// Middleware wraps next with the product gate. The exempt routes (login, state
// lookup, locale, static) are registered by the caller WITHOUT this wrapper;
// everything else goes through it, so a route added later is protected by
// default — the safe direction for a fork that keeps absorbing upstream routes.
func (g *Gate) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, reason := g.Allow(r); !ok {
			WriteDenied(w, reason)
			return
		}
		next.ServeHTTP(w, r)
	})
}
