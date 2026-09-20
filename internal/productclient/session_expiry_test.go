package productclient_test

import (
	"context"
	"fmt"
	"github.com/open-octo/octo-agent/internal/productclient"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEnsureTokenRenewsNearExpiryAndPreservesCredentialsOnNetworkFailure(t *testing.T) {
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/auth/refresh" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":{"accessToken":"new-access","refreshToken":"new-refresh","accessTokenExpiresInSec":900}}`)
	}))
	holder := &productclient.CredentialHolder{}
	holder.Set(productclient.Credentials{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: now.Add(time.Minute)})
	client := productclient.New(ts.URL, productclient.ClientMeta{}, holder, productclient.WithClock(func() time.Time { return now }))
	if err := client.EnsureToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("refreshed a valid access token")
	}
	now = now.Add(40 * time.Second)
	if err := client.EnsureToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || holder.Get().RefreshToken != "new-refresh" {
		t.Fatal("near-expiry token was not rotated")
	}
	ts.Close()
	now = now.Add(time.Hour)
	before := holder.Get()
	if err := client.EnsureToken(context.Background()); err == nil {
		t.Fatal("expected transport failure")
	}
	if holder.Get() != before {
		t.Fatal("transport failure discarded the portable session")
	}
}
