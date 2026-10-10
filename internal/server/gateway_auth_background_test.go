package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productruntime"
	"github.com/open-octo/octo-agent/internal/scheduler"
)

// No browser or websocket client exists: renewal belongs to the task runner.
func TestScheduledGatewayTaskRenewsAndContinuesWithoutFrontend(t *testing.T) {
	var requests, renewals atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			renewals.Add(1)
			fmt.Fprint(w, `{"data":{"accessToken":"new","refreshToken":"rotated","accessTokenExpiresInSec":3600}}`)
			return
		}
		requests.Add(1)
		if r.Header.Get("Authorization") == "Bearer old" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"code":"token_expired"}`)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"background completed\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer endpoint.Close()
	held := &productclient.CredentialHolder{}
	held.Set(productclient.Credentials{AccessToken: "old", RefreshToken: "refresh"})
	renew := productruntime.SessionRenewer{Platform: productclient.New(endpoint.URL, productclient.ClientMeta{}, held), Tokens: held}
	gateway := productruntime.GatewayEndpoint{Host: endpoint.URL, Tokens: held, Ensure: renew.Ensure, Renew: renew.Renew}
	srv := emptyProfileServer(t, Config{Addr: "127.0.0.1:0", Tools: false, GatewayModelPrefix: testGatewayPrefix, GatewaySender: gateway.Sender})
	srv.initWS()
	srv.turnRunning = make(map[string]bool)
	srv.steerQueues = make(map[string][]queuedTurn)
	srv.sessionAgents = make(map[string]*agent.Agent)
	id, err := srv.RunTask(context.Background(), scheduler.Task{Name: "background", Model: testGatewayPrefix + "model", Prompt: "do work"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.LoadSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || renewals.Load() != 1 || len(session.Messages) < 2 {
		t.Fatal("scheduled task did not continue after renewal")
	}
}
