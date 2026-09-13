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
	"time"

	"github.com/open-octo/octo-agent/internal/catalogstore"
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
	// ControlPlane carries the two compile-time profile facts the blocked page
	// needs before the user types anything: whether this build names a real
	// control plane, and whether it trusts any signing key.
	//
	// They are facts about the BUILD, not about the user's data, which is why
	// they arrive here instead of being read out of product-state.json (E6.1) -
	// and why they are a field rather than a productprofile call: the owner is
	// internal/productprofile, and this package only forwards its answer. A
	// direct import would also make the four blocked-page cases untestable,
	// since the embedded profile cannot be varied at run time.
	ControlPlane ControlPlaneStatus
	// SendCodeCooldownSec is reported to the UI so it can count down. It is the
	// local default; the platform's own value wins when it sends one.
	SendCodeCooldownSec int
	// Catalog owns data/catalog.json. nil means this build has nowhere to keep a
	// catalog, in which case fetchCatalog does not start a fetch at all: a
	// network call whose answer nothing can read is not a degradation, it is a
	// waste (开发计划 PR-4b 第 2 步补充 ⑦).
	Catalog *catalogstore.Store
	// CatalogTrust is what verifying a fetched policy requires: the keys this
	// build trusts and the audience it answers to. Both come from
	// internal/productprofile and branding/brand.json, injected here for the
	// same reason ControlPlane is - this package forwards the answer, it does
	// not own the question.
	CatalogTrust CatalogTrust
}

// CatalogTrust is the trust anchor a fetched policy is checked against.
//
// It is a value rather than a call into productprofile so that a test can pin an
// explicit key and audience: a build-tag-selected profile would otherwise make
// these tests fail for a reason unrelated to what they assert.
type CatalogTrust struct {
	// TrustedKeys maps keyId to a base64 ed25519 public key.
	TrustedKeys map[string]string
	// Audience is the build's brandId: a policy addressed to another product
	// must not be accepted just because it is correctly signed.
	Audience string
	// Skew tolerates clock drift between this machine and the platform when the
	// policy window is checked.
	Skew time.Duration
}

// ControlPlaneStatus is the answer to "can this build reach a control plane at
// all", as computed by internal/productprofile. See 本地API契约 §2.13.
type ControlPlaneStatus struct {
	Configured     bool
	HasTrustedKeys bool
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
	api("GET /api/product/control-plane", rt.handleControlPlane)
	api("GET /api/product/chat-modes", rt.handleChatModes)
	api("POST /api/product/send-code", rt.handleSendCode)
	api("POST /api/product/login", rt.handleLogin)
	api("POST /api/product/logout", rt.handleLogout)
	api("PUT /api/product/locale", rt.handleLocale)
}

// chatModesDTO is the wire shape of 本地API契约 §2.8.
//
// There is deliberately no `fallback` field. The old shape carried one to
// announce that the built-in list had been substituted for an unreadable
// data/chat-modes.json, and 需求基线 B1 规则 4 retired both the file and the
// behaviour; re-adding the field would be the built-in list coming back with a
// flag on it. A type that cannot express the field is the cheapest way to keep
// it out (the route test asserts its absence over the wire anyway, because the
// JSON is a Go/JS boundary and this struct is not what the browser sees).
//
// The versions are always present and are empty strings when no catalog was
// available - not omitted. The frontend compares them to notice that the catalog
// changed, and "absent" and "empty" would be two spellings of one state.
type chatModesDTO struct {
	Modes          []chatModeGroupDTO `json:"modes"`
	CatalogVersion string             `json:"catalogVersion"`
	PolicyVersion  string             `json:"policyVersion"`
}

type chatModeGroupDTO struct {
	ID string `json:"id"`
	// Models is always a JSON array, never null: the picker iterates it, and a
	// null would make an empty group a different shape from a populated one.
	Models       []chatModeModelDTO `json:"models"`
	DefaultModel string             `json:"defaultModel"`
}

type chatModeModelDTO struct {
	ID          string                    `json:"id"`
	DisplayName productclient.DisplayName `json:"displayName"`
	CompositeID string                    `json:"compositeId"`
}

// handleChatModes is the picker's data source: the signed catalog, projected.
//
// It reads the cache and never the network. Fetching belongs to login (PR-4b's
// single fetch point) and, from PR-4c, to an explicit refresh - so a picker that
// is opened with no catalog answers "nothing" instead of starting a call behind
// the user's back, which would also make the response's arrival time depend on
// the network.
//
// No catalog, a damaged cache and a cache this build cannot read all produce the
// same answer - an empty list - because the picker has exactly one thing to do
// about all three, and 需求基线 B1 规则 1 forbids the alternative (a local
// list). Telling them apart is PR-4c's job, and it does it on its own endpoint
// reporting the cache's state, not by reading it out of this response.
func (rt *Runtime) handleChatModes(w http.ResponseWriter, r *http.Request) {
	empty := chatModesDTO{Modes: []chatModeGroupDTO{}}

	if rt.deps.Catalog == nil {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	entry, err := rt.deps.Catalog.Load()
	if err != nil {
		// Absence is the ordinary case on a fresh installation, so it is not
		// worth a warning; a damaged or unreadable cache is, because the user
		// sees an empty picker and nothing else would explain it.
		if !errors.Is(err, catalogstore.ErrNoCache) {
			slog.Warn("product: chat-modes served empty", "err", err)
		}
		writeJSON(w, http.StatusOK, empty)
		return
	}

	// The cached envelope was verified before it was written (catalogstore only
	// ever holds what fetchCatalog accepted), so this parses rather than
	// re-verifies: a second signature check here would be a second opinion about
	// a fact that already has one owner (开发规范 §3.8).
	policy, err := entry.Envelope.DecodePolicy()
	if err != nil {
		slog.Warn("product: cached catalog could not be read back", "err", err, "catalogVersion", entry.CatalogVersion)
		writeJSON(w, http.StatusOK, empty)
		return
	}

	groups, ignored := projectCatalog(policy)
	if len(ignored) > 0 {
		// Reported, not acted on: the mode set is a product constant, so an
		// unknown id is a platform contract drift an operator needs to see, while
		// the picker carries on with the modes the product has.
		slog.Warn("product: catalog names mode ids this build does not have", "modeIds", ignored)
	}

	out := chatModesDTO{
		Modes:          make([]chatModeGroupDTO, 0, len(groups)),
		CatalogVersion: entry.CatalogVersion,
		PolicyVersion:  policy.PolicyVersion,
	}
	for _, g := range groups {
		dto := chatModeGroupDTO{
			ID:           g.ID,
			Models:       make([]chatModeModelDTO, 0, len(g.Models)),
			DefaultModel: g.DefaultModel,
		}
		for _, m := range g.Models {
			dto.Models = append(dto.Models, chatModeModelDTO{
				ID:          m.ID,
				DisplayName: m.DisplayName,
				CompositeID: m.CompositeID,
			})
		}
		out.Modes = append(out.Modes, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleControlPlane reports whether this build can reach a control plane at
// all, so the blocked page can say "this package is misconfigured" before the
// user spends a round-trip finding out (本地API契约 §2.13, L-B2).
//
// It reads Deps rather than internal/productprofile because it must work in the
// one case the profile cannot describe: a build whose config is fine on disk
// but whose assembly failed. Forwarding a captured value also keeps the four
// blocked-page cases testable, and keeps this package out of the decoding of
// what "configured" means - that judgement has one owner.
func (rt *Runtime) handleControlPlane(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, controlPlaneDTO{
		Configured:     rt.deps.ControlPlane.Configured,
		HasTrustedKeys: rt.deps.ControlPlane.HasTrustedKeys,
	})
}

// controlPlaneDTO is the wire shape of 本地API契约 §2.13. Both fields are always
// present: the frontend distinguishes the four blocked-page outcomes by their
// values, not by their absence, so omitting a false would collapse two of them.
type controlPlaneDTO struct {
	Configured     bool `json:"configured"`
	HasTrustedKeys bool `json:"hasTrustedKeys"`
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
		rt.followActivationRefusal(req.ActivationCode, req.BoxCode, err)
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

	// The catalog is fetched after the session exists and before the response
	// goes out, because the picker needs it immediately on the first render
	// (需求基线 B1 规则 1: a catalog failure never blocks the login).
	//
	// One error is the exception, and it is not really a catalog failure: a
	// refused session means the platform has already told us the token it just
	// issued is not usable. That has to leave the user on the blocked page with
	// no credential behind, exactly as any other refused call would (L-A6), so it
	// goes through the same funnel instead of being logged as a catalog problem.
	if outcome, err := rt.fetchCatalog(r.Context()); err != nil {
		if IsSessionExpired(err) {
			rt.failPlatform(w, err)
			return
		}
		rt.logCatalogOutcome(outcome, err)
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

// followActivationRefusal makes the local activation flag follow the platform
// when it denies a sign-in that offered no activation credential (V-44).
//
// BOTH HALVES OF THE CONDITION ARE LOAD-BEARING, and neither is a detail:
//
//   - The platform must have SAID the authorization is unusable. Only the four
//     codes 本地API契约 §3 classifies as activation failures count. A transport
//     failure, a 5xx, a wrong SMS code or a restricted account says nothing
//     about whether this installation is activated, and un-activating a paying
//     user because the network blinked would be the worst reading of §3.9.
//   - The attempt must NOT have offered a credential. An attempt that carries an
//     activation code is the user saying "activate this", and the platform
//     refusing that code says nothing about an activation this installation
//     already holds. Without this half, one mistyped character in the five-field
//     form would un-activate an installation that is fine.
//
// `phone_mismatch` is deliberately outside the family even though it is an
// activation failure: it can only answer an attempt that carried a credential,
// so the second half above already excludes it, and what it means is "that code
// belongs to another number" - not "this installation is unactivated".
//
// The answer itself is untouched: the code, the envelope and the status the user
// sees are the platform's. All this changes is which form they read it on.
func (rt *Runtime) followActivationRefusal(activationCode, boxCode string, err error) {
	if strings.TrimSpace(activationCode) != "" || strings.TrimSpace(boxCode) != "" {
		return
	}
	var pe *productclient.Error
	if !errors.As(err, &pe) || !refusesActivation(pe.Code) {
		return
	}
	if rt.deps.State == nil {
		return
	}
	if err := rt.deps.State.WithdrawActivationClaim(); err != nil {
		// Not fatal: the user still gets the platform's answer. But the wall then
		// keeps the two-field shape, so the one line that explains why the
		// interface still asks for a code it cannot accept is worth having.
		slog.Error("product: the platform denied this installation's activation but the local claim could not be lowered",
			"code", pe.Code, "err", err)
	}
}

// refusesActivation reports whether a platform code is one of the four that mean
// "this authorization is not usable" (需求基线 E1 规则 2, L-A4). The four come
// from internal/productclient, which owns the vocabulary.
func refusesActivation(code string) bool {
	switch code {
	case productclient.CodeActivationInvalid,
		productclient.CodeActivationCodeUsed,
		productclient.CodeBoxCodeUnknown,
		productclient.CodeBoxCodeMismatch:
		return true
	}
	return false
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
//
// It is a function of the two stores rather than a method because there are two
// callers now: this package's HTTP funnel, and the renewal a gateway turn asks
// for (PR-4c1). A turn refusal never passes through the other one, so the
// alternative was a second copy of "the session is over" in session.go - and a
// copy is how the login flag and the credential file drift apart (开发规范 §3.8).
func (rt *Runtime) forgetSession(cause error) {
	forgetSession(rt.deps.Creds, rt.deps.State, cause)
}

func forgetSession(creds *credentialstore.Store, state *productstate.Store, cause error) {
	if creds != nil {
		if err := creds.Delete(); err != nil {
			// Not fatal: State.Logout below still returns the interface to the
			// login page, which is the part the user depends on. Reported rather
			// than swallowed so a read-only or unplugged data root is visible.
			slog.Error("product: session expired but the credential could not be removed",
				"err", err, "cause", cause)
		}
	}
	if state != nil {
		if err := state.Logout(); err != nil {
			slog.Error("product: session expired but the login flag could not be cleared", "err", err)
		}
	}
}
