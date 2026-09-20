package productruntime

import (
	"encoding/json"
	"errors"
	"github.com/open-octo/octo-agent/internal/atomicfile"
	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/productclient"
	"net/http"
	"net/url"
	"os"
	"sync"
)

// mountFinance preserves the upstream financial DTOs: money and points remain decimal strings.
func (rt *Runtime) mountFinance(api func(string, http.HandlerFunc)) {
	for _, resource := range []string{"wallet", "pricing", "wallet/ledger", "usage", "recharge/options", "recharge/orders"} {
		api("GET /api/product/"+resource, func(w http.ResponseWriter, r *http.Request) { rt.financeRead(w, r, resource) })
	}
	api("GET /api/product/recharge/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		rt.financeRead(w, r, "recharge/orders/"+r.PathValue("id"))
	})
	api("POST /api/product/recharge/orders", rt.createRecharge)
	api("GET /api/product/recharge/intent", rt.rechargeIntent)
	api("PUT /api/product/recharge/intent", rt.rechargeIntent)
	api("DELETE /api/product/recharge/intent", rt.rechargeIntent)
}
func (rt *Runtime) financeReady(w http.ResponseWriter) bool {
	if !rt.deps.State.State().LoggedIn {
		writeCode(w, 401, productclient.CodeUnauthorized, nil)
		return false
	}
	if rt.deps.Platform == nil {
		writeControlPlaneUnconfigured(w)
		return false
	}
	return true
}
func (rt *Runtime) financeRead(w http.ResponseWriter, r *http.Request, resource string) {
	if !rt.financeReady(w) {
		return
	}
	query := url.Values{}
	for _, key := range []string{"cursor", "limit", "status", "start_date", "end_date", "date_from", "date_to", "session_id", "client_turn_id", "run_id", "request_id", "include_summary"} {
		if value := r.URL.Query().Get(key); value != "" {
			query.Set(key, value)
		}
	}
	result, err := rt.deps.Platform.FinanceRead(r.Context(), resource, query)
	if err != nil {
		rt.failPlatform(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (rt *Runtime) createRecharge(w http.ResponseWriter, r *http.Request) {
	if !rt.financeReady(w) {
		return
	}
	var req struct {
		productclient.RechargeOrderInput
		IdempotencyKey string `json:"idempotencyKey"`
		Scope          string `json:"scope"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 128 || req.OptionID == "" || req.PaymentMethod == "" {
		writeCode(w, 400, "invalid_value", nil)
		return
	}
	scope, userID, scopeErr := rt.deps.Platform.FinanceScope(r.Context())
	if scopeErr != nil {
		rt.failPlatform(w, scopeErr)
		return
	}
	if req.Scope != scope {
		writeCode(w, 409, "account_changed", nil)
		return
	}
	req.ExpectedUserID = userID
	result, err := rt.deps.Platform.CreateRechargeOrder(r.Context(), req.RechargeOrderInput, req.IdempotencyKey)
	if err != nil {
		rt.failPlatform(w, err)
		return
	}
	writeJSON(w, 200, result)
}

type rechargeIntentData struct {
	OptionID      string `json:"option_id"`
	PaymentMethod string `json:"payment_method"`
	Key           string `json:"idempotencyKey"`
	OrderID       string `json:"order_id,omitempty"`
}

var rechargeIntentMu sync.Mutex

// The retry intent travels with the portable data root; it contains no payment or authentication secrets.
func (rt *Runtime) rechargeIntent(w http.ResponseWriter, r *http.Request) {
	if !rt.financeReady(w) {
		return
	}
	rechargeIntentMu.Lock()
	defer rechargeIntentMu.Unlock()
	scope, _, err := rt.deps.Platform.FinanceScope(r.Context())
	if err != nil {
		rt.failPlatform(w, err)
		return
	}
	if r.Method != http.MethodGet && r.Header.Get("X-Finance-Scope") != scope {
		writeCode(w, 409, "account_changed", nil)
		return
	}
	path, err := datapath.Join("recharge-intent-" + scope + ".json")
	if err != nil {
		writeCode(w, 500, "internal_error", nil)
		return
	}
	switch r.Method {
	case http.MethodGet:
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, 200, map[string]any{"scope": scope, "intent": nil})
			return
		}
		if err != nil {
			writeCode(w, 500, "internal_error", nil)
			return
		}
		var value rechargeIntentData
		if json.Unmarshal(raw, &value) != nil {
			writeCode(w, 500, "internal_error", nil)
			return
		}
		writeJSON(w, 200, map[string]any{"scope": scope, "intent": value})
	case http.MethodPut:
		var value rechargeIntentData
		if !decodeBody(w, r, &value) {
			return
		}
		if len(value.Key) < 8 || len(value.Key) > 128 || value.OptionID == "" || value.PaymentMethod == "" {
			writeCode(w, 400, "invalid_value", nil)
			return
		}
		if _, err = datapath.Root(); err == nil {
			var raw []byte
			raw, err = json.Marshal(value)
			if err == nil {
				err = atomicfile.WriteFile(path, raw, 0600)
			}
		}
		if err != nil {
			writeCode(w, 500, "internal_error", nil)
			return
		}
		writeJSON(w, 200, value)
	case http.MethodDelete:
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			writeCode(w, 500, "internal_error", nil)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}
