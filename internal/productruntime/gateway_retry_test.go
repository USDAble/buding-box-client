package productruntime

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestGatewayDoesNotMultiplyPlatformRetries(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "completion", true: "stream"}[stream], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"code":"PROVIDER_UNAVAILABLE","message":"try later"}`))
			}))
			defer server.Close()
			g := GatewayEndpoint{Host: server.URL, Tokens: signedIn("test-token")}
			var err error
			if stream {
				err = runStream(t, g)
			} else {
				err = run(t, g)
			}
			if err == nil {
				t.Fatal("503 must reach caller")
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("gateway received %d requests, want 1; the platform owns provider failover", got)
			}
		})
	}
}
