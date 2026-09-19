package productruntime_test

import (
	"net/http"
	"testing"
)

func TestBoxReturnsOnlyTheAuthorisedBoxProjection(t *testing.T) {
	h := newHarness(t)
	h.activate()

	status, body := h.do(http.MethodGet, "/api/product/box", nil)
	if status != http.StatusOK {
		t.Fatalf("box status = %d body = %v, want 200", status, body)
	}
	if body["id"] == "" || body["displayName"] == "" {
		t.Errorf("box identity = %v, want safe id and name", body)
	}
	for _, forbidden := range []string{"boxCode", "activationCode", "ip", "serial", "credential"} {
		if _, found := body[forbidden]; found {
			t.Errorf("box response exposed %q: %v", forbidden, body)
		}
	}
}
