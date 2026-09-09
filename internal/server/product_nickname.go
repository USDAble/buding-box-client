package server

import (
	"net/http"

	"github.com/open-octo/octo-agent/internal/productstate"
)

// Product nickname editing for the account panel (P5). The rule is the same
// one the login form runs (需求 §5.3.3 / §5.4.2): shape then sensitive words,
// and a hit is refused outright — never masked-and-saved. Unlike the login
// endpoint this is a single-field edit, so failures are one at a time, keyed
// by the same machine codes the login form maps.
//
// The endpoint is registered behind the product gate (s.apiProduct): it only
// makes sense once the window is logged in.
//
// OCTO-FORK: P5 account panel — see
// dev-docs-usdable/需求/2260906/技术方案/P5-个人中心.md.

// handleProductNickname updates the bound account's nickname and returns the
// refreshed de-identified state, so the corner + panel update from one
// round-trip without a second GET.
func (s *Server) handleProductNickname(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nickname string `json:"nickname"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}

	switch err := productstate.ValidateNickname(req.Nickname); {
	case err != nil:
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "nickname_format"})
		return
	case productstate.Sensitive(req.Nickname):
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "nickname_sensitive"})
		return
	}

	if !s.productState.Snapshot().LoggedIn() {
		// The gate already refuses not-logged-in windows; a nil account here
		// would mean the state file was edited behind the server's back
		// mid-request. Fail closed rather than panic.
		writeJSON(w, http.StatusConflict, map[string]string{"code": "not_logged_in"})
		return
	}

	if err := s.productState.Mutate(func(st *productstate.State) error {
		st.Account.Nickname = req.Nickname
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "save nickname failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"state": s.productState.Snapshot().Public()})
}
