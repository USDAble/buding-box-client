package productruntime

import (
	"context"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
)

func TestGatewayChecksExistingTokenBeforeCreatingSender(t *testing.T) {
	held := &productclient.CredentialHolder{}
	held.Set(productclient.Credentials{AccessToken: "expired", ExpiresAt: time.Now().Add(-time.Minute)})
	checked := false
	gateway := GatewayEndpoint{Host: "http://127.0.0.1:1", Tokens: held, Ensure: func(context.Context) error {
		checked = true
		held.Set(productclient.Credentials{AccessToken: "renewed", ExpiresAt: time.Now().Add(time.Hour)})
		return nil
	}}
	if _, err := gateway.Sender(reasoningOff); err != nil {
		t.Fatal(err)
	}
	if !checked || held.AccessToken() != "renewed" {
		t.Fatal("existing expired token skipped renewal")
	}
}
