package productclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestLateUnauthorizedCatalogCannotRenewOrClearNewLogin(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-release
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthorized"}`))
	}))
	defer server.Close()
	tokens := &CredentialHolder{}
	tokens.Set(Credentials{AccessToken: "old", RefreshToken: "old-refresh"})
	client := New(server.URL, ClientMeta{}, tokens)
	result := make(chan error, 1)
	go func() { _, err := client.CatalogModels(context.Background(), ""); result <- err }()
	<-started
	tokens.Set(Credentials{AccessToken: "new", RefreshToken: "new-refresh"})
	close(release)
	if err := <-result; !errors.Is(err, ErrSessionChanged) {
		t.Fatalf("late response = %v", err)
	}
	if calls.Load() != 1 || tokens.AccessToken() != "new" {
		t.Fatal("late old-session response affected the new login")
	}
}

func TestExplicitLogoutInvalidatesLocalSessionEvenWhenOffline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	tokens := &CredentialHolder{}
	tokens.Set(Credentials{AccessToken: "old", RefreshToken: "old-refresh"})
	generation := tokens.SessionGeneration()
	client := New(server.URL, ClientMeta{}, tokens)
	if err := client.Logout(context.Background()); err == nil {
		t.Fatal("revocation failure was hidden")
	}
	if tokens.Get() != (Credentials{}) || tokens.SessionGeneration() == generation {
		t.Fatal("explicit logout retained the old local session")
	}
}
