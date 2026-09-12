// Package productruntime serves the local product API the web UI calls
// (/api/product/*), defined by dev-docs-usdable/需求/20260911/本地API契约.md.
//
// It sits between two contracts: browser-facing on this side, platform-facing on
// the other (中台交付包.md). It holds no platform field names of its own - the
// transport shapes live in internal/productclient and local state lives in
// internal/productstate. This package is only translation.
//
// Deliberately NOT in internal/server: that package is upstream code, and product
// behaviour grows here instead (需求基线 G1).
package productruntime

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productstate"
)

// defaultCooldownSec mirrors the platform's usual value, used only when the
// platform does not report one.
const defaultCooldownSec = 60

// Deps are the collaborators the runtime needs. All are required except Platform,
// which is nil when no control plane is configured.
//
// The concrete stores are used rather than interfaces: there is exactly one
// implementation of each and both are already the single owner of their file
// (开发规范 §3.8), so a port here would add indirection without buying a seam.
type Deps struct {
	State    *productstate.Store
	Creds    *credentialstore.Store
	Platform *productclient.Client
	// SendCodeCooldownSec is reported to the UI so it can count down. It is the
	// local default; the platform's own value wins when it sends one.
	SendCodeCooldownSec int
}

// Runtime serves the local product endpoints.
type Runtime struct {
	deps Deps
}

// New builds a Runtime.
func New(deps Deps) *Runtime {
	if deps.SendCodeCooldownSec <= 0 {
		deps.SendCodeCooldownSec = defaultCooldownSec
	}
	return &Runtime{deps: deps}
}

// Handler returns the local product routes as a standalone http.Handler, for
// tests and for any host that wants to serve them without internal/server.
//
// The gate is NOT applied here yet: it needs the window token, which is its own
// change (L-B1). What this returns is the unauthenticated surface.
func (rt *Runtime) Handler() http.Handler {
	mux := http.NewServeMux()
	rt.Mount(func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, h) })
	return mux
}

// Mount registers the local product routes through a caller-supplied registrar.
//
// This is how the routes reach the real server: cmd/octo-desktop passes the
// server's own authenticated registrar (server.Config.MountAPI), so every route
// below inherits requireAuth and the no-store policy without this package
// knowing that server exists. Handler() below is the standalone adapter, used by
// tests and by any host that wants a bare http.Handler.
//
// This method is the single list of product routes — both adapters call it, so
// a route cannot be added to one path and forgotten in the other.
func (rt *Runtime) Mount(api func(pattern string, h http.HandlerFunc)) {
	api("GET /api/product/state", rt.handleState)
	api("POST /api/product/send-code", rt.handleSendCode)
	api("POST /api/product/login", rt.handleLogin)
	api("POST /api/product/logout", rt.handleLogout)
	api("PUT /api/product/locale", rt.handleLocale)
}

// handleState is the first call the UI makes: it decides whether the window
// shows the login screen or the workspace (本地API契约 §2.1).
//
// The state object is returned unwrapped, unlike login's {"state": ...}. That
// asymmetry is in the contract, not an oversight - the frontend reads it as the
// object itself.
func (rt *Runtime) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, rt.deps.State.PublicState())
}

// handleSendCode asks the platform to text a login code.
func (rt *Runtime) handleSendCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if !validPhone(req.Phone) {
		// Business-level, NOT field-level. The frontend reads body.code and files
		// it under the phone input itself; moving this into fieldErrors would
		// silently lose the message (本地API契约 §2.2).
		writeCode(w, http.StatusBadRequest, productclient.CodeInvalidPhone, nil)
		return
	}
	if rt.deps.Platform == nil {
		writeControlPlaneUnconfigured(w)
		return
	}

	data, err := rt.deps.Platform.SendSMS(r.Context(), productclient.SendSMSRequest{
		Phone:   req.Phone,
		Purpose: productclient.PurposeLogin,
	})
	if err != nil {
		rt.failPlatform(w, err)
		return
	}
	cooldown := data.CooldownSec
	if cooldown <= 0 {
		cooldown = rt.deps.SendCodeCooldownSec
	}
	writeJSON(w, http.StatusOK, map[string]any{"cooldownSec": cooldown})
}

// handleLogin covers first activation and every later login. The box code and the
// activation code are required on the first and omitted afterwards (本地API契约 §2.3).
func (rt *Runtime) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone          string `json:"phone"`
		Code           string `json:"code"`
		Nickname       string `json:"nickname"`
		ActivationCode string `json:"activationCode"`
		BoxCode        string `json:"boxCode"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if !rt.validateLogin(w, req.Phone, req.Code, req.Nickname, req.ActivationCode, req.BoxCode) {
		return
	}
	if rt.deps.Platform == nil {
		writeControlPlaneUnconfigured(w)
		return
	}

	data, err := rt.deps.Platform.Login(r.Context(), productclient.LoginRequest{
		Phone:    req.Phone,
		Code:     req.Code,
		Nickname: req.Nickname,
		// Both empty on a later login; the platform treats their absence as
		// "already activated" and answers with the record it holds.
		ActivationCode: strings.TrimSpace(req.ActivationCode),
		BoxCode:        strings.TrimSpace(req.BoxCode),
		InstallID:      rt.deps.State.InstallID(),
	})
	if err != nil {
		rt.failPlatform(w, err)
		return
	}

	outcome := productstate.LoginOutcome{
		PhoneMasked: data.Account.PhoneMasked,
		Nickname:    data.Account.Nickname,
	}
	if data.Activation != nil {
		// The box code is taken from the platform's answer, never from the form.
		// A later login sends no form field at all, and it still has to end up in
		// the license page (需求基线 E6.1).
		outcome.ActivatedAt = data.Activation.ActivatedAt
		outcome.ExpiresAt = data.Activation.ExpiresAt
		outcome.BoxCode = data.Activation.BoxCode
	}
	if err := rt.deps.State.ApplyLogin(outcome, rt.deps.State.State().Credits); err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}

	// The refresh token is the one long-lived credential; the access token stays
	// in the client's memory and is never written (需求基线 E6 规则 2).
	if err := rt.deps.Creds.Save(credentialstore.Credential{
		RefreshToken:       data.RefreshToken,
		AccountPhoneMasked: data.Account.PhoneMasked,
		InstallID:          rt.deps.State.InstallID(),
	}); err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"state": rt.deps.State.PublicState()})
}

// handleLogout ends the session on this installation.
//
// It deletes the credential and clears the login flag. The activation record and
// the bound number stay: logging out is not un-activating, and the second-login
// form compares against the number (需求基线 E7).
func (rt *Runtime) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := rt.deps.Creds.Delete(); err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}
	if err := rt.deps.State.Logout(); err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleLocale stores the interface language. It works while logged out, because
// the login screen itself has to be readable (需求基线 E8 规则 5).
func (rt *Runtime) handleLocale(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Locale string `json:"locale"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if !validLocale(req.Locale) {
		writeFieldErrors(w, http.StatusBadRequest, map[string]string{"locale": "invalid_value"})
		return
	}
	if err := rt.deps.State.SetLocale(req.Locale); err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// validateLogin applies the local rules and writes the failure envelope itself.
// It reports whether the caller should continue.
//
// Local validation exists to answer "is this even well formed" cheaply and
// per-input. Everything that requires knowing whether a credential is real is the
// platform's call, and its failures stay distinguishable from these: the split
// between invalid_activation (shape) and activation_invalid (rejected) is
// deliberate in the contract (§3).
func (rt *Runtime) validateLogin(w http.ResponseWriter, phone, code, nickname, activationCode, boxCode string) bool {
	fields := map[string]string{}
	if !validPhone(phone) {
		fields["phone"] = productclient.CodeInvalidPhone
	}
	if !validCode(code) {
		fields["code"] = productclient.CodeInvalidCode
	}
	if !validNickname(nickname) {
		fields["nickname"] = "nickname_format"
	}

	// A first activation is any attempt that carries either credential, plus any
	// attempt on an installation that has never activated. Both credentials are
	// then required, so a half-filled form is reported against the empty one
	// rather than sent to the platform.
	activationAttempt := activationCode != "" || boxCode != "" || !rt.deps.State.State().Activated
	if activationAttempt {
		if strings.TrimSpace(activationCode) == "" {
			fields["activationCode"] = "invalid_activation"
		}
		if strings.TrimSpace(boxCode) == "" {
			fields["boxCode"] = "invalid_box_code"
		}
	}

	if len(fields) > 0 {
		writeFieldErrors(w, http.StatusBadRequest, fields)
		return false
	}
	return true
}

// decodeBody reads a JSON body, answering with a field-level error when it cannot
// be parsed so the UI has a shape it already handles.
func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		writeCode(w, http.StatusBadRequest, productclient.CodeInvalidRequest, nil)
		return false
	}
	return true
}

// validPhone is the mainland mobile shape: 11 digits starting with 1. The
// platform re-validates; this only avoids a round trip for an obvious typo.
func validPhone(phone string) bool {
	if len(phone) != 11 || phone[0] != '1' {
		return false
	}
	for _, c := range phone {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// validCode is the SMS code shape (6 digits). A wrong-but-well-formed code is the
// platform's business failure, not this one.
func validCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// validNickname bounds the nickname. Whether it contains a sensitive word is the
// platform's answer, because the dictionary is server-side authoritative (P8).
func validNickname(nickname string) bool {
	n := strings.TrimSpace(nickname)
	return n != "" && len([]rune(n)) <= 20
}

// validLocale accepts only the two shipped languages. Anything else is a
// field-level error; the frontend renders its own wording (本地API契约 §2.5).
func validLocale(locale string) bool {
	return locale == "zh" || locale == "en"
}

// errSessionExpired is re-exported for callers that need to tell a lost session
// apart from an outage when they decide whether to clear local state.
var errSessionExpired = productclient.ErrSessionExpired

// IsSessionExpired reports whether err means the refresh token is gone or was
// refused - the case that returns the user to the blocked screen (需求基线 E12).
func IsSessionExpired(err error) bool {
	return errors.Is(err, errSessionExpired)
}

// failPlatform is the single funnel every platform failure passes through.
//
// It exists so L-A6 has exactly one enforcement point: a caller cannot reach the
// platform and forget the session-expiry rule, because writing the response any
// other way would mean not using the mapping at all. The first authorised call
// (the catalog fetch) is what makes the expiry branch reachable end to end; the
// rule is wired here ahead of it so that landing that call cannot silently skip
// it.
func (rt *Runtime) failPlatform(w http.ResponseWriter, err error) {
	if IsSessionExpired(err) {
		rt.forgetSession(err)
	}
	writePlatformError(w, err)
}

// forgetSession drops the local half of a session the platform has revoked
// (需求基线 E12, L-A6).
//
// Order is load-bearing: the credential leaves the disk *before* the caller
// answers "unauthorized". The frontend reacts to that answer by rendering the
// login page, and a credential still on disk would make the next startup read
// "logged in" again - an interface that quietly disagrees with the file, and one
// the user cannot escape because every request would re-fail the same way.
//
// The activation record and the bound number stay (E7): an expired session is
// not an un-activation. Keeping them is also what makes the second login ask for
// only phone and code - and matters more here than anywhere else, because an
// activation code can be used exactly once and could not be re-entered.
//
// A failure to clear is reported and does not stop the logout; see
// P4-拦截页.md §4.4 for why the in-memory phase must still reach the login page.
func (rt *Runtime) forgetSession(cause error) {
	if err := rt.deps.Creds.Delete(); err != nil {
		// Not fatal: State.Logout below still returns the interface to the login
		// page, which is the part the user depends on. Reported rather than
		// swallowed so a read-only or unplugged data root is visible.
		slog.Error("product: session expired but the credential could not be removed",
			"err", err, "cause", cause)
	}
	if err := rt.deps.State.Logout(); err != nil {
		slog.Error("product: session expired but the login flag could not be cleared", "err", err)
	}
}
