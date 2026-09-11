package clienttest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
)

// TestSignedPolicySurvivesTheWire is the test that makes the "sign the bytes on
// the wire" scheme ( D-007 ) real rather than assumed.
//
// The scheme fails silently if anything between signer and verifier reformats
// the JSON: Go's encoder compacts a json.RawMessage when it writes it, and an
// HTML-escaping pass would rewrite it too. Only a round trip through a real
// HTTP server exercises those passes, so this test signs a policy in the
// stand-in, reads it back as a client would, and verifies the signature over
// the bytes that actually arrived.
func TestSignedPolicySurvivesTheWire(t *testing.T) {
	standin := New()
	server := httptest.NewServer(standin.Handler())
	defer server.Close()

	access := loginForBootstrap(t, server.URL)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/client/bootstrap", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data productclient.PolicyEnvelope `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if len(body.Data.Policy) == 0 || body.Data.Signature.Sig == "" {
		t.Fatal("bootstrap carried no signed policy; the stand-in is not signing what the client verifies")
	}

	got, err := body.Data.Verify(productclient.VerifyOptions{
		TrustedKeys: map[string]string{FixtureSigningKeyID: FixtureSigningPublicKey()},
		Audience:    FixturePolicyAudience,
		Now:         time.Now(),
		Skew:        time.Minute,
	})
	if err != nil {
		t.Fatalf("the signature did not survive the wire: %v", err)
	}
	if got.PolicyVersion == "" {
		t.Error("verified policy has no version")
	}
	if len(got.Catalog.Models) == 0 {
		t.Error("verified policy has an empty catalog; the fixture offers no model to select")
	}
}

func loginForBootstrap(t *testing.T, base string) string {
	t.Helper()

	post := func(path string, payload any) map[string]any {
		t.Helper()
		buf, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", path, err)
		}
		resp, err := http.Post(base+path, "application/json", bytes.NewReader(buf))
		if err != nil {
			t.Fatalf("post %s: %v", path, err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("post %s status = %d, body = %v", path, resp.StatusCode, out)
		}
		return out
	}

	const phone = "13900000000"
	post("/v1/auth/sms/send", productclient.SendSMSRequest{
		Phone: phone, Purpose: productclient.PurposeLogin, ClientRequestID: "req-1",
	})
	login := post("/v1/auth/login", productclient.LoginRequest{
		Phone:          phone,
		Code:           FixtureSMSCode,
		ActivationCode: FixtureActivationCode,
		BoxCode:        FixtureBoxCode,
		InstallID:      "11111111-1111-4111-8111-111111111111",
	})

	data, ok := login["data"].(map[string]any)
	if !ok {
		t.Fatalf("login returned no data: %v", login)
	}
	access, _ := data["accessToken"].(string)
	if access == "" {
		t.Fatalf("login returned no access token: %v", login)
	}
	return access
}
