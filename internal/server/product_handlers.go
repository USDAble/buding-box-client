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
// gate or the main UI before any other request.
func (s *Server) handleProductState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.productState.Snapshot().Public())
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
