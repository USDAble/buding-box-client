package productruntime_test

import (
	"net/http"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
)

func TestFeedbackForwardsOnlyValidatedFormData(t *testing.T) {
	h := newHarness(t)
	h.activate()

	status, body := h.do(http.MethodPost, "/api/product/feedback", map[string]any{
		"category":       "suggestion",
		"title":          "Keyboard shortcuts",
		"content":        "  Add a keyboard shortcut.  ",
		"impact":         "normal",
		"idempotencyKey": "0b6f0f6e-0000-4000-8000-000000000001",
	})
	if status != http.StatusOK {
		t.Fatalf("feedback status = %d body = %v, want 200", status, body)
	}
	if body["feedbackId"] == "" {
		t.Errorf("feedback receipt = %v, want non-empty receipt", body)
	}

	status, body = h.do(http.MethodPost, "/api/product/feedback", map[string]any{
		"category": "suggestion", "title": "No content", "content": "", "impact": "normal", "idempotencyKey": "0b6f0f6e-0000-4000-8000-000000000002",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("empty feedback status = %d body = %v, want 400", status, body)
	}
	fields, _ := body["fieldErrors"].(map[string]any)
	if fields["content"] != "invalid_length" {
		t.Errorf("empty feedback errors = %v, want content invalid_length", body)
	}

}

// OCTO-FORK: the UI needs the server-provided cooldown, never a client-side
// feedback threshold, so preserve both the common code and retry duration.
func TestFeedbackRateLimitPreservesTheServerCooldown(t *testing.T) {
	h := newHarness(t)
	h.activate()
	h.platform.FailFeedback(http.StatusTooManyRequests, productclient.CodeRateLimited, 45)

	status, body := h.do(http.MethodPost, "/api/product/feedback", map[string]any{
		"category":       "suggestion",
		"title":          "Keyboard shortcuts",
		"content":        "Add a keyboard shortcut.",
		"impact":         "normal",
		"idempotencyKey": "0b6f0f6e-0000-4000-8000-000000000003",
	})
	if status != http.StatusTooManyRequests {
		t.Fatalf("feedback status = %d body = %v, want 429", status, body)
	}
	if got := body["code"]; got != productclient.CodeRateLimited {
		t.Errorf("code = %v, want %q", got, productclient.CodeRateLimited)
	}
	if got := body["retryAfterSec"]; got != float64(45) {
		t.Errorf("retryAfterSec = %v, want 45", got)
	}
}
