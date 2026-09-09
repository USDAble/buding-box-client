package server

import (
	"net/http"
)

// Product sensitive-word input gate (P8). The engine itself is P7; this file
// wires it into the product surface: the check endpoint the frontend calls
// before sending, and the server-side re-check that runs on the chat path
// (frontends can be bypassed, so the server enforces the gate before the
// credit is charged — 需求 §5.5.1 / §5.4.4).
//
// OCTO-FORK: P8 敏感词接入 — see
// dev-docs-usdable/需求/2260906/技术方案/P8-敏感词接入.md.

// handleProductSensitiveCheck runs the engine over the given text and returns
// whether it hit and, when it did, the masked form. The frontend calls this on
// every send attempt so it can substitute the input box before the message
// leaves. Product-gated like the rest of the logged-in surface.
func (s *Server) handleProductSensitiveCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}
	if s.sensitiveEngine == nil {
		writeJSON(w, http.StatusOK, map[string]any{"hit": false})
		return
	}
	res := s.sensitiveEngine.Filter(req.Text)
	writeJSON(w, http.StatusOK, map[string]any{
		"hit":    res.Matched(),
		"masked": res.Text,
	})
}

// checkInputSensitive is the server-side input gate. It returns the masked
// text and true when the input-sensitivity switch is on AND the text hits a
// sensitive word; otherwise ("", false). The caller must refuse the message
// WITHOUT charging a credit when this returns true — the requirement's
// pipeline (P8 §3.6) runs this check before the P6 deduction.
func (s *Server) checkInputSensitive(text string) (string, bool) {
	if s.sensitiveEngine == nil || s.productState == nil {
		return "", false
	}
	if !s.productState.Snapshot().Prefs.InputSensitiveCheck {
		return "", false
	}
	res := s.sensitiveEngine.Filter(text)
	if !res.Matched() {
		return "", false
	}
	return res.Text, true
}

// filterThinking masks the user-visible thinking/reasoning text. The stored
// thinking block (which round-trips to the provider) stays verbatim; this is
// only applied to the copy surfaced to the frontend — the assistant_message
// event and history replay — so "若展示思考过程，则一并过滤" holds without
// breaking the provider's signature check.
func (s *Server) filterThinking(text string) string {
	if s.sensitiveEngine == nil || text == "" {
		return text
	}
	return s.sensitiveEngine.Filter(text).Text
}
