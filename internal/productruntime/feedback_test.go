package productruntime_test

import (
	"net/http"
	"testing"
)

func TestFeedbackForwardsOnlyValidatedFormData(t *testing.T) {
	h := newHarness(t)
	h.activate()

	status, body := h.do(http.MethodPost, "/api/product/feedback", map[string]any{
		"category":       "suggestion",
		"content":        "  Add a keyboard shortcut.  ",
		"idempotencyKey": "0b6f0f6e-0000-4000-8000-000000000001",
	})
	if status != http.StatusOK {
		t.Fatalf("feedback status = %d body = %v, want 200", status, body)
	}
	if body["feedbackId"] == "" {
		t.Errorf("feedback receipt = %v, want non-empty receipt", body)
	}

	status, body = h.do(http.MethodPost, "/api/product/feedback", map[string]any{
		"category": "suggestion", "content": "", "idempotencyKey": "0b6f0f6e-0000-4000-8000-000000000002",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("empty feedback status = %d body = %v, want 400", status, body)
	}
	fields, _ := body["fieldErrors"].(map[string]any)
	if fields["content"] != "invalid_length" {
		t.Errorf("empty feedback errors = %v, want content invalid_length", body)
	}

}
