package productclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRenewTokenDoesNotRestoreLoggedOutOrReplacedSession(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprint(replace), func(t *testing.T) {
			started, finish := make(chan struct{}), make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-finish
				fmt.Fprint(w, `{"data":{"accessToken":"new","refreshToken":"rotated","accessTokenExpiresInSec":3600}}`)
			}))
			defer srv.Close()
			held := &CredentialHolder{}
			held.Set(Credentials{AccessToken: "old", RefreshToken: "refresh"})
			client := New(srv.URL, ClientMeta{}, held)
			done := make(chan error, 1)
			go func() { done <- client.RenewToken(context.Background(), "old") }()
			<-started
			held.Clear()
			if replace {
				held.Set(Credentials{AccessToken: "other", RefreshToken: "other-refresh"})
			}
			close(finish)
			if err := <-done; !errors.Is(err, ErrSessionChanged) {
				t.Fatalf("renewal=%v, want changed session", err)
			}
			want := ""
			if replace {
				want = "other"
			}
			if held.AccessToken() != want {
				t.Fatal("refresh resurrected or replaced a session")
			}
		})
	}
}

func TestRenewTokenWaitingForAnotherTaskIsCancellable(t *testing.T) {
	held := &CredentialHolder{}
	held.Set(Credentials{AccessToken: "old", RefreshToken: "refresh"})
	client := New("http://127.0.0.1:1", ClientMeta{}, held)
	client.refreshGate <- struct{}{}
	defer func() { <-client.refreshGate }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := client.RenewToken(ctx, "old"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("renewal=%v, want canceled waiter", err)
	}
	if held.Get().RefreshToken == "" {
		t.Fatal("cancellation cleared credentials")
	}
}
