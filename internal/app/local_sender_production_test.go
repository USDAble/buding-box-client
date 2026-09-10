//go:build product_production

package app

import (
	"context"
	"testing"

	"github.com/open-octo/octo-agent/internal/provider"
)

// The local fake channel is compiled out of production builds. These tests pin
// the *negative* guarantee: in a production build the provider name still
// resolves (so diagnostics stay precise), but it can never emit output.
//
// Run with: go test -tags product_production ./internal/app/

// TestProductionLocalProviderCannotEmit is the structural counterpart of
// TestLocalSenderReplyHitsSensitiveFilter: there is no reply to filter, because
// the reply engine is not in this binary.
func TestProductionLocalProviderCannotEmit(t *testing.T) {
	p, err := buildClient(ProviderLocal, "", "", "", nil, nil)
	if err != nil {
		// Failing construction is an acceptable shape of the same guarantee.
		return
	}
	if _, err := p.Send(context.Background(), provider.Request{Model: "buding-local-general"}); err == nil {
		t.Fatal("production local provider produced a response, want a hard error")
	}
}

// TestProductionLocalProviderNameIsStable keeps the provider name resolvable so
// a production config or catalog mentioning `provider: local` reports a precise
// error instead of "unknown provider".
func TestProductionLocalProviderNameIsStable(t *testing.T) {
	p, err := buildClient(ProviderLocal, "", "", "", nil, nil)
	if err != nil {
		t.Fatalf("buildClient(local) = %v, want a named stub provider", err)
	}
	if got := p.Name(); got != ProviderLocal {
		t.Fatalf("Name() = %q, want %q", got, ProviderLocal)
	}
}

// TestProductionLocalSendStreamEmitsNothing asserts the streaming path fails
// before any token is delivered, so no caller can observe partial fake output.
func TestProductionLocalSendStreamEmitsNothing(t *testing.T) {
	p, err := buildClient(ProviderLocal, "", "", "", nil, nil)
	if err != nil {
		return
	}
	sp, ok := p.(provider.StreamingProvider)
	if !ok {
		t.Fatal("production local provider does not implement provider.StreamingProvider")
	}
	var emitted []string
	_, err = sp.SendStream(context.Background(), provider.Request{Model: "buding-local-general"}, provider.StreamCallbacks{
		OnText: func(s string) { emitted = append(emitted, s) },
	})
	if err == nil {
		t.Fatal("SendStream succeeded, want a hard error")
	}
	if len(emitted) != 0 {
		t.Fatalf("SendStream emitted %q before failing, want nothing", emitted)
	}
}
