package server

import (
	"net/http"

	"github.com/open-octo/octo-agent/internal/productstate"
)

// Product account and preference mutations. OCTO-FORK: P5 account panel — see
// dev-docs-usdable/需求/2260906/技术方案/P5-个人中心.md.
func (s *Server) handleProductNickname(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nickname string `json:"nickname"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}
	if err := productstate.ValidateNickname(req.Nickname); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "nickname_format"})
		return
	}
	if productstate.Sensitive(req.Nickname) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "nickname_sensitive"})
		return
	}
	if err := s.productState.Mutate(func(st *productstate.State) error {
		if st.Account == nil || !st.LoggedIn() {
			return errProductNotLoggedIn
		}
		st.Account.Nickname = req.Nickname
		return nil
	}); err != nil {
		if err == errProductNotLoggedIn {
			writeError(w, http.StatusForbidden, "product_gate")
			return
		}
		writeError(w, http.StatusInternalServerError, "save nickname failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": s.productState.Snapshot().Public()})
}

func (s *Server) handleProductPrefs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Locale          *string `json:"locale"`
		DefaultChatMode *string `json:"defaultChatMode"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}
	if req.Locale == nil && req.DefaultChatMode == nil {
		writeError(w, http.StatusBadRequest, "at least one preference is required")
		return
	}
	if req.Locale != nil && *req.Locale != "zh" && *req.Locale != "en" {
		writeError(w, http.StatusBadRequest, "invalid locale")
		return
	}
	if req.DefaultChatMode != nil && !validChatMode(*req.DefaultChatMode) {
		writeError(w, http.StatusBadRequest, "invalid default chat mode")
		return
	}
	if err := s.productState.Mutate(func(st *productstate.State) error {
		if !st.LoggedIn() {
			return errProductNotLoggedIn
		}
		if req.Locale != nil {
			st.Prefs.Locale = *req.Locale
		}
		if req.DefaultChatMode != nil {
			st.Prefs.DefaultChatMode = *req.DefaultChatMode
		}
		return nil
	}); err != nil {
		if err == errProductNotLoggedIn {
			writeError(w, http.StatusForbidden, "product_gate")
			return
		}
		writeError(w, http.StatusInternalServerError, "save preferences failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": s.productState.Snapshot().Public()})
}

var errProductNotLoggedIn = &productStateError{"not logged in"}

type productStateError struct{ message string }

func (e *productStateError) Error() string { return e.message }

func validChatMode(mode string) bool {
	return mode == "privacy" || mode == "smart" || mode == "default"
}
