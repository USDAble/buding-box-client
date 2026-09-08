package server

import (
	"net/http"

	"github.com/open-octo/octo-agent/internal/productstate"
)

// Product state + logout + locale handlers. Login, send-code and activate
// arrive in P4 (their validation rules belong to the login form); this file
// only serves the state the frontend routes on, clears the account on logout,
// and persists the interface language — the three things the gate needs
// without the form.

// handleProductState serves the de-identified product state. It is exempt from
// the product gate so the frontend can decide whether to render the login
// gate or the main UI before any other request. When the user has not chosen a
// language yet, prefs.locale is filled with the system-derived language so the
// login page opens in the right language — derived here, not persisted (需求
// §5.3.1 / §10 T10). OCTO-FORK: P4 — see
// dev-docs-usdable/需求/2260906/技术方案/P4-拦截页.md.
func (s *Server) handleProductState(w http.ResponseWriter, r *http.Request) {
	pub := s.productState.Snapshot().Public()
	if pub.Prefs.Locale == "" {
		pub.Prefs.Locale = productstate.SystemLocale()
	}
	writeJSON(w, http.StatusOK, pub)
}

// handleProductLogout clears only the account, keeping activation and credits
// (需求 §5.3.5: logging out must not destroy the device's activation or the
// accumulated points).
func (s *Server) handleProductLogout(w http.ResponseWriter, r *http.Request) {
	err := s.productState.Mutate(func(st *productstate.State) error {
		st.Account = nil
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "logout failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleProductLocale persists the interface language. It is exempt from the
// product gate — the requirement allows switching the UI language before
// login (需求 §5.3).
func (s *Server) handleProductLocale(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Locale string `json:"locale"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}
	if req.Locale != "zh" && req.Locale != "en" {
		writeError(w, http.StatusBadRequest, `locale must be "zh" or "en"`)
		return
	}
	err := s.productState.Mutate(func(st *productstate.State) error {
		st.Prefs.Locale = req.Locale
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save locale failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
