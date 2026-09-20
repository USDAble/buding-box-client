package productruntime_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func financeCall(t *testing.T, h *harness, method, path string, body any, scope string) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	r, _ := http.NewRequest(method, h.local.URL+path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Finance-Scope", scope)
	response, err := h.local.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var out map[string]any
	if err = json.NewDecoder(response.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, out
}
func TestRechargeIntentPersistsUnderPortableRootAndRequiresLogin(t *testing.T) {
	h := newHarness(t)
	status, _ := h.do("GET", "/api/product/recharge/intent", nil)
	if status != 401 {
		t.Fatal(status)
	}
	h.activate()
	_, initial := h.do("GET", "/api/product/recharge/intent", nil)
	scope := initial["scope"].(string)
	intent := map[string]any{"option_id": "option-real", "payment_method": "wechat", "idempotencyKey": "persistent-intent-123"}
	if status, _ := financeCall(t, h, "PUT", "/api/product/recharge/intent", intent, scope); status != 200 {
		t.Fatal(status)
	}
	raw, err := os.ReadFile(filepath.Join(h.root, "recharge-intent-"+scope+".json"))
	if err != nil || !strings.Contains(string(raw), "persistent-intent-123") {
		t.Fatalf("intent not portable: %s %v", raw, err)
	}
	status, got := h.do("GET", "/api/product/recharge/intent", nil)
	if status != 200 || got["intent"].(map[string]any)["idempotencyKey"] != "persistent-intent-123" {
		t.Fatal(status, got)
	}
	if status, _ := financeCall(t, h, "DELETE", "/api/product/recharge/intent", nil, scope); status != 200 {
		t.Fatal(status)
	}
	if _, err := os.Stat(filepath.Join(h.root, "recharge-intent-"+scope+".json")); !os.IsNotExist(err) {
		t.Fatal("intent not cleared")
	}
}
func TestRechargeScopeRejectsAnotherAccountAndPreservesOriginalIntent(t *testing.T) {
	h := newHarness(t)
	h.activate()
	_, initial := h.do("GET", "/api/product/recharge/intent", nil)
	scope := initial["scope"].(string)
	intent := map[string]any{"option_id": "option-real", "payment_method": "wechat", "idempotencyKey": "persistent-intent-123", "order_id": "original-account-order"}
	financeCall(t, h, "PUT", "/api/product/recharge/intent", intent, scope)
	original := h.platformSrv.Config.Handler
	h.platformSrv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/client/account" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"id":"another-account"}}`))
			return
		}
		original.ServeHTTP(w, r)
	})
	status, got := h.do("GET", "/api/product/recharge/intent", nil)
	if status != 200 || got["intent"] != nil || got["scope"] == scope {
		t.Fatal("other account read prior intent", status, got)
	}
	if status, _ := financeCall(t, h, "PUT", "/api/product/recharge/intent", intent, scope); status != 409 {
		t.Fatal(status)
	}
	if status, _ := financeCall(t, h, "DELETE", "/api/product/recharge/intent", nil, scope); status != 409 {
		t.Fatal(status)
	}
	if status, _ := h.do("POST", "/api/product/recharge/orders", map[string]any{"option_id": "option", "payment_method": "wechat", "idempotencyKey": "stable-intent-123", "scope": scope}); status != 409 {
		t.Fatal(status)
	}
}
func TestFinanceBridgeSendsOnlyServerIDsAndUsesIdempotency(t *testing.T) {
	h := newHarness(t)
	h.activate()
	_, initial := h.do("GET", "/api/product/recharge/intent", nil)
	scope := initial["scope"].(string)
	original := h.platformSrv.Config.Handler
	h.platformSrv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/client/recharge/orders" {
			original.ServeHTTP(w, r)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if len(body) != 3 || body["expected_user_id"] == "" || r.Header.Get("Idempotency-Key") != "stable-intent-123" || r.Header.Get("Authorization") == "" {
			t.Errorf("bad bridge %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"order-1","status":"pending","points":"100.0000"}`))
	})
	status, body := h.do("POST", "/api/product/recharge/orders", map[string]any{"option_id": "option", "payment_method": "wechat", "idempotencyKey": "stable-intent-123", "scope": scope})
	if status != 200 || body["status"] != "pending" || body["points"] != "100.0000" {
		t.Fatal(status, body)
	}
}
