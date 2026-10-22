package productruntime

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

func imageCatalogFixture(t *testing.T) (*catalogFixture, string) {
	t.Helper()
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()
	entry, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := entry.Envelope.DecodePolicy()
	if err != nil {
		t.Fatal(err)
	}
	model := &policy.Catalog.Models[1]
	policy.Catalog.Version = "2026-10-20.1"
	entry.CatalogVersion = policy.Catalog.Version
	model.ReasoningOptions = []string{"default", "high"}
	model.CapabilityCombinations = []productclient.CapabilityCombination{
		{ReasoningEffort: "default"},
		{ReasoningEffort: "default", Tools: true},
		{ReasoningEffort: "default", Vision: true},
		{ReasoningEffort: "high", Tools: true, Vision: true},
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := base64.StdEncoding.DecodeString(clienttest.FixtureSigningKeySeed)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := productclient.SignPolicy(raw, clienttest.FixtureSigningKeyID, ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	entry.Envelope.Policy, entry.Envelope.Signature = raw, signature
	if err := f.store.Put(entry); err != nil {
		t.Fatal(err)
	}
	// The fixture's forced fetch does not attest the process session. Mark the
	// newly signed test cache as belonging to this already-authenticated session.
	f.rt.catalogSession, f.rt.catalogSessionVerified = f.rt.catalogGeneration(), true
	return f, model.ID
}

func TestModelImageInputUsesExactSignedCombination(t *testing.T) {
	f, model := imageCatalogFixture(t)
	for _, test := range []struct {
		effort         string
		tools, allowed bool
	}{
		{"default", false, true}, {"default", true, false},
		{"high", true, true}, {"high", false, false},
	} {
		status := f.rt.ModelImageInputStatus(context.Background(), model, test.effort, test.tools)
		if !status.Known || status.Allowed != test.allowed {
			t.Fatalf("effort=%s tools=%v: %+v", test.effort, test.tools, status)
		}
		if !status.Allowed && status.Reason != "capability_not_verified" {
			t.Fatalf("unverified was misreported: %+v", status)
		}
	}
	status := f.rt.ModelImageInputStatus(context.Background(), "withdrawn", "default", true)
	if status.Allowed || !status.Known {
		t.Fatalf("withdrawn model accepted: %+v", status)
	}
}

func TestGatewayImageInputKeepsTurnEffortThroughMasking(t *testing.T) {
	f, model := imageCatalogFixture(t)
	selected := "default"
	gateway := GatewayEndpoint{Host: f.baseURL, Tokens: signedIn(f.login.AccessToken),
		ReasoningForModel: func(string) (string, error) { return selected, nil }, ImageInputStatus: f.rt.ModelImageInputStatus}
	sender, err := gateway.Sender(app.ReasoningTuning{ClientModelID: model})
	if err != nil {
		t.Fatal(err)
	}
	sender = app.WrapSensitive(sender, sensitive.New(""))
	selected = "high"
	status := agent.SenderImageInputStatus(context.Background(), sender, model, true)
	if !status.Known || status.Allowed {
		t.Fatalf("turn silently adopted a later effort: %+v", status)
	}
	next, err := gateway.Sender(app.ReasoningTuning{ClientModelID: model})
	if err != nil {
		t.Fatal(err)
	}
	if status := agent.SenderImageInputStatus(context.Background(), next, model, true); !status.Allowed {
		t.Fatalf("new turn failed to use explicit choice: %+v", status)
	}
}
