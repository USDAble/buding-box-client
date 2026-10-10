package productruntime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
)

func authGateway(t *testing.T, handler http.HandlerFunc) (GatewayEndpoint, *productclient.CredentialHolder) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	held := signedIn("old")
	platform := productclient.New(srv.URL, productclient.ClientMeta{}, held)
	renew := SessionRenewer{Platform: platform, Tokens: held}
	return GatewayEndpoint{Host: srv.URL, Tokens: held, Ensure: renew.Ensure, Renew: renew.Renew}, held
}

func answerRenewal(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"data":{"accessToken":"new","refreshToken":"rotated","accessTokenExpiresInSec":3600}}`)
}

func TestGatewayAuthReplaysOnceForBufferedAndStreamingRequests(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var renewals, hits atomic.Int32
			var originalBody, originalTurn, originalSession string
			g, held := authGateway(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/refresh" {
					renewals.Add(1)
					answerRenewal(w)
					return
				}
				body, _ := io.ReadAll(r.Body)
				if hits.Add(1) == 1 {
					originalBody, originalTurn, originalSession = string(body), r.Header.Get("X-Buding-Turn"), r.Header.Get("X-Buding-Local-Session")
					w.WriteHeader(401)
					fmt.Fprint(w, `{"code":"token_expired"}`)
					return
				}
				if string(body) != originalBody || r.Header.Get("X-Buding-Turn") != originalTurn || r.Header.Get("X-Buding-Local-Session") != originalSession || r.Header.Get("Authorization") != "Bearer new" {
					t.Error("replay changed the request identity/body or retained the rejected token")
				}
				if stream {
					fmt.Fprint(w, stubStream)
				} else {
					fmt.Fprint(w, stubCompletion)
				}
			})
			var err error
			if stream {
				err = runStream(t, g)
			} else {
				err = run(t, g)
			}
			if err != nil {
				t.Fatal(err)
			}
			if renewals.Load() != 1 || hits.Load() != 2 || held.Get().RefreshToken != "rotated" {
				t.Fatal("expected one persisted-in-memory rotation and one replay")
			}
		})
	}
}

func TestGatewayAuthCollapsesConcurrentBackgroundRequests(t *testing.T) {
	const workers = 6
	var oldCalls, refreshes atomic.Int32
	allRejected := make(chan struct{})
	g, _ := authGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			refreshes.Add(1)
			answerRenewal(w)
			return
		}
		if r.Header.Get("Authorization") == "Bearer old" {
			if oldCalls.Add(1) == workers {
				close(allRejected)
			}
			<-allRejected
			w.WriteHeader(401)
			fmt.Fprint(w, `{"code":"unauthorized"}`)
			return
		}
		fmt.Fprint(w, stubCompletion)
	})
	sender, err := g.Sender(reasoningOff)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := sender.SendMessages(context.Background(), "model", "", []agent.Message{agent.NewUserMessage("work")}, 10); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes=%d, want 1", refreshes.Load())
	}
}

func TestGatewayAuthDoesNotLoopOrRetryBusinessFailures(t *testing.T) {
	for _, status := range []int{401, 402, 403, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests, renewals atomic.Int32
			g, _ := authGateway(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/refresh" {
					renewals.Add(1)
					answerRenewal(w)
					return
				}
				requests.Add(1)
				w.WriteHeader(status)
				fmt.Fprint(w, `{"code":"refused"}`)
			})
			if runStream(t, g) == nil {
				t.Fatal("expected refusal")
			}
			wantRequests, wantRenewals := int32(1), int32(0)
			if status == 401 {
				wantRequests, wantRenewals = 2, 1
			}
			if requests.Load() != wantRequests || renewals.Load() != wantRenewals {
				t.Fatal("unbounded or inappropriate replay")
			}
		})
	}
}

func TestGatewayAuthRefreshFailureRetainsOrEndsCredentials(t *testing.T) {
	for _, status := range []int{401, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests, renewals atomic.Int32
			g, held := authGateway(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/refresh" {
					renewals.Add(1)
					w.WriteHeader(status)
					code := "unauthorized"
					if status == 503 {
						code = "upstream_unavailable"
					}
					fmt.Fprintf(w, `{"code":%q}`, code)
					return
				}
				requests.Add(1)
				w.WriteHeader(401)
			})
			if runStream(t, g) == nil {
				t.Fatal("expected refresh failure")
			}
			want := int32(1)
			if status == 503 {
				want = 2
			}
			if requests.Load() != 1 || renewals.Load() != want {
				t.Fatal("incorrect renewal/replay budget")
			}
			if (held.Get().RefreshToken == "") != (status == 401) {
				t.Fatal("temporary failure cleared credentials, or rejection retained them")
			}
		})
	}
}

func TestGatewayAuthPersistsRotationAfterRejectedRequest(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())
	store, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(credentialstore.Credential{RefreshToken: "refresh-not-used-here", InstallID: "install"}); err != nil {
		t.Fatal(err)
	}
	g, held := authGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			answerRenewal(w)
			return
		}
		if r.Header.Get("Authorization") == "Bearer old" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, stubStream)
	})
	renew := SessionRenewer{Platform: productclient.New(g.Host, productclient.ClientMeta{}, held), Tokens: held, Creds: store}
	g.Ensure, g.Renew = renew.Ensure, renew.Renew
	if err := runStream(t, g); err != nil {
		t.Fatal(err)
	}
	cred, present, err := store.Load()
	if err != nil || !present || cred.RefreshToken != "rotated" || cred.InstallID != "install" {
		t.Fatal("rotated refresh token was not persisted")
	}
}

func TestGatewayAuthPreemptivelyRenewsBeforeLaterRound(t *testing.T) {
	var refreshes atomic.Int32
	g, held := authGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			refreshes.Add(1)
			answerRenewal(w)
			return
		}
		if r.Header.Get("Authorization") != "Bearer new" {
			t.Error("expired token reached gateway")
		}
		fmt.Fprint(w, stubCompletion)
	})
	now := time.Now()
	held.Set(productclient.Credentials{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: now.Add(time.Minute)})
	renew := SessionRenewer{Platform: productclient.New(g.Host, productclient.ClientMeta{}, held, productclient.WithClock(func() time.Time { return now })), Tokens: held}
	g.Ensure, g.Renew = renew.Ensure, renew.Renew
	sender, err := g.Sender(reasoningOff)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := sender.SendMessages(context.Background(), "model", "", []agent.Message{agent.NewUserMessage("work")}, 10); err != nil {
		t.Fatal(err)
	}
	if refreshes.Load() != 1 {
		t.Fatal("later request did not preemptively renew")
	}
}

func TestGatewayAuthUsesLatestTokenBeforeEachRound(t *testing.T) {
	var refreshes atomic.Int32
	var held *productclient.CredentialHolder
	g, tokens := authGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			refreshes.Add(1)
			answerRenewal(w)
			return
		}
		if r.Header.Get("Authorization") != "Bearer new" {
			t.Error("sent expired token")
		}
		fmt.Fprint(w, stubCompletion)
	})
	held = tokens
	sender, err := g.Sender(reasoningOff)
	if err != nil {
		t.Fatal(err)
	}
	platform := productclient.New(g.Host, productclient.ClientMeta{}, held)
	if err := platform.RenewToken(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.SendMessages(context.Background(), "model", "", []agent.Message{agent.NewUserMessage("work")}, 10); err != nil {
		t.Fatal(err)
	}
	if refreshes.Load() != 1 {
		t.Fatal("unexpected extra rotation")
	}
}

func TestGatewayAuthDoesNotReplayAnOpenedStream(t *testing.T) {
	var requests, renewals atomic.Int32
	g, _ := authGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			renewals.Add(1)
			answerRenewal(w)
			return
		}
		requests.Add(1)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"+"data: {\"error\":{\"code\":\"token_expired\",\"message\":\"expired\"}}\n\n")
	})
	if _, err := streamText(t, g); err == nil {
		t.Fatal("expected stream failure")
	}
	if requests.Load() != 1 || renewals.Load() != 0 {
		t.Fatal("opened stream was replayed")
	}
}

func TestGatewayAuthStopsAfterCancellationLogoutOrAccountChange(t *testing.T) {
	for _, action := range []string{"cancel", "logout", "switch"} {
		t.Run(action, func(t *testing.T) {
			g, held := authGateway(t, func(w http.ResponseWriter, r *http.Request) { t.Error("stopped task sent a request") })
			sender, err := g.Sender(reasoningOff)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch action {
			case "cancel":
				cancel()
			case "logout":
				held.Clear()
			case "switch":
				held.Set(productclient.Credentials{AccessToken: "other", RefreshToken: "other-refresh"})
			}
			if _, err := sender.SendMessages(ctx, "model", "", []agent.Message{agent.NewUserMessage("work")}, 10); err == nil {
				t.Fatal("expected stopped request")
			}
		})
	}
}

type authCountingTool struct{ calls int }

func (t *authCountingTool) Execute(context.Context, string, map[string]any) (agent.ToolResult, error) {
	t.calls++
	return agent.ToolResult{Text: "saved"}, nil
}

func TestGatewayAuthContinuesBackgroundAgentWithoutRepeatingTools(t *testing.T) {
	var calls atomic.Int32
	g, _ := authGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			answerRenewal(w)
			return
		}
		switch calls.Add(1) {
		case 1:
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"save-1","type":"function","function":{"name":"save","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
		case 2:
			w.WriteHeader(401)
			fmt.Fprint(w, `{"code":"token_expired"}`)
		default:
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), "saved") {
				t.Error("completed tool result was lost")
			}
			fmt.Fprint(w, stubStream)
		}
	})
	sender, err := g.Sender(reasoningOff)
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(sender, "model")
	tool := &authCountingTool{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.RunStream(ctx, "background work", []agent.ToolDefinition{{Name: "save", Parameters: map[string]any{"type": "object"}}}, tool, nil); err != nil {
		t.Fatal(err)
	}
	if tool.calls != 1 || calls.Load() != 3 {
		t.Fatalf("tool calls=%d, model requests=%d", tool.calls, calls.Load())
	}
}
