package productclient_test

import (
	"context"
	"encoding/json"
	"github.com/open-octo/octo-agent/internal/productclient"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFinanceReadsActualTopLevelDTOAndScopesQueries(t *testing.T) {
	fixtures := map[string]string{
		"wallet":                     `{"available_points":"123.4500","reserved_points":"2.0000","total_recharged_points":"200.0000","total_consumed_points":"74.5500","status":"active"}`,
		"pricing":                    `{"items":[{"logical_model_id":"model-real","input_points_per_million":"12.3400"}],"points_per_usd":"100.0000"}`,
		"wallet/ledger":              `{"items":[{"id":42,"event_type":"recharge","available_change":"123.4500"}],"next_cursor":"next","has_more":true}`,
		"usage":                      `{"items":[{"run_id":"run-real","net_points":null,"billing_status":"pending_usage"}],"summary":{"net_points":"3.0000"},"has_more":false}`,
		"recharge/options":           `{"items":[{"id":"option-published","amount_cents":2500,"currency":"CNY","points":"2500.0000","enabled":true}],"payment_methods":[],"payment_enabled":false}`,
		"recharge/orders":            `{"items":[{"id":"order-real","status":"pending"}],"has_more":false}`,
		"recharge/orders/order-real": `{"id":"order-real","status":"paid","points":"2500.0000"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access" {
			t.Error("missing bearer")
		}
		if r.URL.Query().Get("cursor") != "cursor+value" {
			t.Error("cursor not encoded")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fixtures[r.URL.Path[len("/api/v1/client/"):]]))
	}))
	defer server.Close()
	holder := &productclient.CredentialHolder{}
	holder.Set(productclient.Credentials{AccessToken: "test-access"})
	client := productclient.New(server.URL+"/api/v1", productclient.ClientMeta{}, holder)
	for resource, want := range fixtures {
		t.Run(resource, func(t *testing.T) {
			got, err := client.FinanceRead(context.Background(), resource, url.Values{"cursor": {"cursor+value"}})
			if err != nil {
				t.Fatal(err)
			}
			var actual, expected any
			json.Unmarshal(got, &actual)
			json.Unmarshal([]byte(want), &expected)
			a, _ := json.Marshal(actual)
			b, _ := json.Marshal(expected)
			if string(a) != string(b) {
				t.Fatalf("got=%s want=%s", a, b)
			}
		})
	}
}
func TestRechargeCreationOnlySendsPublishedIDsAndStableKey(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/v1/client/recharge/orders" || r.Header.Get("Idempotency-Key") != "intent-stable-123" {
			t.Errorf("incorrect request")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if len(body) != 2 || body["option_id"] != "published-option" || body["payment_method"] != "wechat" {
			t.Fatalf("unexpected body %v", body)
		}
		w.Write([]byte(`{"id":"same-order","status":"pending","payment_url":"weixin://wxpay/bizpayurl?pr=test"}`))
	}))
	defer server.Close()
	holder := &productclient.CredentialHolder{}
	holder.Set(productclient.Credentials{AccessToken: "token"})
	client := productclient.New(server.URL+"/v1", productclient.ClientMeta{}, holder)
	for range 2 {
		raw, err := client.CreateRechargeOrder(context.Background(), productclient.RechargeOrderInput{OptionID: "published-option", PaymentMethod: "wechat"}, "intent-stable-123")
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		json.Unmarshal(raw, &out)
		if out["id"] != "same-order" || out["status"] != "pending" {
			t.Fatal("order not preserved")
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
