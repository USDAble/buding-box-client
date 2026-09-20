package productclient

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type directResponse struct{ Target any }
type RechargeOrderInput struct {
	ExpectedUserID string `json:"expected_user_id,omitempty"`
	OptionID       string `json:"option_id"`
	PaymentMethod  string `json:"payment_method"`
}

func (c *Client) FinanceRead(ctx context.Context, resource string, query url.Values) (json.RawMessage, error) {
	switch resource {
	case "wallet", "pricing", "wallet/ledger", "usage", "recharge/options", "recharge/orders":
	default:
		if !strings.HasPrefix(resource, "recharge/orders/") || strings.ContainsAny(strings.TrimPrefix(resource, "recharge/orders/"), "/\\?#") {
			return nil, fmt.Errorf("invalid financial resource")
		}
	}
	path := "/client/" + resource
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var out json.RawMessage
	err := c.doAuthorized(ctx, http.MethodGet, path, nil, &directResponse{&out})
	return out, err
}
func (c *Client) CreateRechargeOrder(ctx context.Context, input RechargeOrderInput, key string) (json.RawMessage, error) {
	if input.OptionID == "" || input.PaymentMethod == "" || len(key) < 8 || len(key) > 128 {
		return nil, fmt.Errorf("invalid recharge request")
	}
	var out json.RawMessage
	err := c.doAuthorizedWithKey(ctx, http.MethodPost, "/client/recharge/orders", input, &directResponse{&out}, key)
	return out, err
}

// FinanceScope comes from a server-authenticated identity, never a locally decoded JWT.
func (c *Client) FinanceScope(ctx context.Context) (scope, userID string, err error) {
	var identity struct {
		ID string `json:"id"`
	}
	if err = c.doAuthorized(ctx, http.MethodGet, "/client/account", nil, &identity); err != nil {
		return
	}
	if identity.ID == "" {
		err = fmt.Errorf("missing financial account identity")
		return
	}
	userID = identity.ID
	scope = fmt.Sprintf("%x", sha256.Sum256([]byte(c.baseURL+"\x00"+identity.ID)))
	return
}
