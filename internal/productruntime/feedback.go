package productruntime

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/open-octo/octo-agent/internal/productclient"
)

// handleFeedback forwards only the feedback form's explicit fields. It never
// consults chat state, local files, or diagnostic data.
func (rt *Runtime) handleFeedback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Category       string `json:"category"`
		Title          string `json:"title"`
		Content        string `json:"content"`
		Reproduction   string `json:"reproduction"`
		Expected       string `json:"expected"`
		Impact         string `json:"impact"`
		Contact        string `json:"contact"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if !validFeedbackCategory(req.Category) {
		writeFieldErrors(w, http.StatusBadRequest, map[string]string{"category": "invalid_value"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.Reproduction = strings.TrimSpace(req.Reproduction)
	req.Expected = strings.TrimSpace(req.Expected)
	req.Contact = strings.TrimSpace(req.Contact)
	if n := utf8.RuneCountInString(req.Title); n < 1 || n > 120 {
		writeFieldErrors(w, http.StatusBadRequest, map[string]string{"title": "invalid_length"})
		return
	}
	if n := utf8.RuneCountInString(req.Content); n < 1 || n > 4000 {
		writeFieldErrors(w, http.StatusBadRequest, map[string]string{"content": "invalid_length"})
		return
	}
	if n := utf8.RuneCountInString(req.Reproduction); n > 2000 {
		writeFieldErrors(w, http.StatusBadRequest, map[string]string{"reproduction": "invalid_length"})
		return
	}
	if n := utf8.RuneCountInString(req.Expected); n > 2000 {
		writeFieldErrors(w, http.StatusBadRequest, map[string]string{"expected": "invalid_length"})
		return
	}
	if !validFeedbackImpact(req.Impact) {
		writeFieldErrors(w, http.StatusBadRequest, map[string]string{"impact": "invalid_value"})
		return
	}
	if n := utf8.RuneCountInString(req.Contact); n > 200 {
		writeFieldErrors(w, http.StatusBadRequest, map[string]string{"contact": "invalid_length"})
		return
	}
	if !validUUID(req.IdempotencyKey) {
		writeCode(w, http.StatusBadRequest, productclient.CodeInvalidRequest, nil)
		return
	}
	if rt.deps.Platform == nil {
		writeControlPlaneUnconfigured(w)
		return
	}
	receipt, err := rt.deps.Platform.Feedback(r.Context(), productclient.FeedbackRequest{
		Category:     req.Category,
		Title:        req.Title,
		Content:      req.Content,
		Reproduction: req.Reproduction,
		Expected:     req.Expected,
		Impact:       req.Impact,
		Contact:      req.Contact,
	}, req.IdempotencyKey)
	if err != nil {
		rt.failPlatform(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func validFeedbackCategory(v string) bool {
	return v == "bug" || v == "suggestion" || v == "other"
}

func validFeedbackImpact(v string) bool {
	return v == "low" || v == "normal" || v == "high"
}

func validUUID(v string) bool {
	if len(v) != 36 {
		return false
	}
	for i, c := range v {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
