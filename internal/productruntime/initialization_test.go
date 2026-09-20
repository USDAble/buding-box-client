package productruntime_test

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

func TestLoginReturnsBeforeWorkspaceReadsAndInitializationRunsInParallel(t *testing.T) {
	h := newHarness(t)
	original := h.platformSrv.Config.Handler
	var reads atomic.Int32
	var timedOut atomic.Bool
	var once sync.Once
	started := make(chan struct{})
	h.platformSrv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/catalog/models", "/v1/credits/ledger", "/v1/box", "/v1/dictionaries/sensitive":
			if reads.Add(1) == 4 {
				once.Do(func() { close(started) })
			}
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				timedOut.Store(true)
				http.Error(w, "workspace requests were serialized", http.StatusGatewayTimeout)
				return
			}
		}
		original.ServeHTTP(w, r)
	})
	h.sendCode("13800001234")
	status, body := h.login(map[string]any{
		"phone": "13800001234", "code": clienttest.FixtureSMSCode, "nickname": "tester",
		"activationCode": clienttest.FixtureActivationCode, "boxCode": clienttest.FixtureBoxCode,
	})
	if status != http.StatusOK || reads.Load() != 0 {
		t.Fatalf("login must finish independently: status=%d reads=%d", status, reads.Load())
	}
	state := body["state"].(map[string]any)
	if state["credits"].(map[string]any)["known"] != false {
		t.Fatal("unread balance must remain unknown")
	}
	status, body = h.do(http.MethodPost, "/api/product/initialize", nil)
	if status != http.StatusOK || timedOut.Load() || reads.Load() != 4 {
		t.Fatalf("parallel initialization: status=%d reads=%d timeout=%v", status, reads.Load(), timedOut.Load())
	}
	state = body["state"].(map[string]any)
	if state["credits"].(map[string]any)["known"] != true {
		t.Fatal("verified ledger was not marked known")
	}
}

func TestWorkspaceInitializationRequiresLogin(t *testing.T) {
	h := newHarness(t)
	status, _ := h.do(http.MethodPost, "/api/product/initialize", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("status=%d", status)
	}
}
