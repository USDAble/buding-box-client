package productruntime

import (
	"net/http"

	"github.com/open-octo/octo-agent/internal/pii"
)

// handlePrivacyRules projects the versioned built-in registry for presentation.
// It deliberately returns identifiers only: localized labels live in the UI,
// while detection patterns and source examples must never leave internal/pii.
func (rt *Runtime) handlePrivacyRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ruleVersion": pii.RuleVersion,
		"rules":       pii.RuleIDs(),
	})
}

// handlePrivacyTransform previews the same irreversible transform enforced by
// the server send path. Product routes inherit authentication, the window gate,
// and Cache-Control: no-store from the registrar supplied to Runtime.Mount.
func (rt *Runtime) handlePrivacyTransform(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if rt.deps.PersonalInfo == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"code":  "privacy_transform_failed",
			"error": "personal information protection is unavailable",
		})
		return
	}
	result, err := rt.deps.PersonalInfo.Transform(req.Text)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"code":  "privacy_transform_failed",
			"error": "personal information protection failed; please retry",
		})
		return
	}
	matches := result.Matches
	if matches == nil {
		matches = []pii.MatchSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hit":         len(matches) > 0,
		"masked":      result.Masked,
		"matches":     matches,
		"ruleVersion": pii.RuleVersion,
	})
}
