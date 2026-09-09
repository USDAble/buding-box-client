package server

import (
	"net/http"

	"github.com/open-octo/octo-agent/internal/productstate"
)

// Product preference editing for the account panel (P5). locale was already
// settable pre-login via PUT /api/product/locale (P4, gate-exempt so the
// login page can switch language); this endpoint is the logged-in counterpart
// and also carries defaultChatMode — the field the panel's "default mode for
// new sessions" control writes and P9 reads when creating a session. Both
// fields survive logout (需求 §5.3.5), so a change here lands in the same
// Prefs block the login page reads.
//
// OCTO-FORK: P5 account panel — see
// dev-docs-usdable/需求/2260906/技术方案/P5-个人中心.md.

// handleProductPrefs updates the given preferences and returns the refreshed
// de-identified state. Either field may be omitted; at least the request
// carries what the caller changed. Unknown values are rejected with machine
// codes rather than silently stored, so a typo'ed client can't wedge the UI
// into a state the pickers can't represent.
func (s *Server) handleProductPrefs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Locale              *string `json:"locale"`
		DefaultChatMode     *string `json:"defaultChatMode"`
		InputSensitiveCheck *bool   `json:"inputSensitiveCheck"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}

	if req.Locale != nil && *req.Locale != "zh" && *req.Locale != "en" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"field": "locale", "code": "invalid_value"})
		return
	}
	if req.DefaultChatMode != nil && !validChatMode(*req.DefaultChatMode) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"field": "defaultChatMode", "code": "invalid_value"})
		return
	}

	if err := s.productState.Mutate(func(st *productstate.State) error {
		if req.Locale != nil {
			st.Prefs.Locale = *req.Locale
		}
		if req.DefaultChatMode != nil {
			st.Prefs.DefaultChatMode = *req.DefaultChatMode
		}
		if req.InputSensitiveCheck != nil {
			st.Prefs.InputSensitiveCheck = *req.InputSensitiveCheck
		}
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "save prefs failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"state": s.productState.Snapshot().Public()})
}

// validChatMode reports whether v names one of the three chat modes from 需求
// §5.6. These are the group ids chat-modes.json (P9) keys its modes by, so
// keeping the allow-list here means the panel can only ever write a value P9
// will understand. If P9 later adds a fourth mode, this list grows with it.
func validChatMode(v string) bool {
	switch v {
	case "privacy", "smart", "default":
		return true
	}
	return false
}
