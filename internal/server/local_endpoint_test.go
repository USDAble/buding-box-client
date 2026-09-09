package server

import (
	"testing"

	"github.com/open-octo/octo-agent/internal/config"
)

// TestEnsureLocalEndpointSeedsOnEmptyConfig pins §3.2: a fresh data root (no
// config.yml) gets a buding endpoint with provider: local and the four fixed
// model ids.
func TestEnsureLocalEndpointSeedsOnEmptyConfig(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())

	ensureLocalEndpoint()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ep := findEndpoint(cfg, localEndpointID)
	if ep == nil {
		t.Fatalf("buding endpoint not seeded; endpoints=%+v", cfg.Endpoints)
	}
	if ep.Provider != "local" {
		t.Errorf("Provider = %q, want local", ep.Provider)
	}
	if ep.APIKey != "" {
		t.Errorf("APIKey = %q, want empty (keyless by design)", ep.APIKey)
	}
	got := endpointModelIDs(ep)
	want := []string{"buding-local-general", "buding-local-fast", "buding-cloud-plus", "buding-cloud-pro"}
	if len(got) != len(want) {
		t.Fatalf("seeded models = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("model[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestEnsureLocalEndpointResolvesToLocalSender verifies the end-to-end default
// path: once buding is seeded, resolveProviderAndModel defaults to the local
// channel (not onboarding) — so a fresh install can chat without a key.
func TestEnsureLocalEndpointResolvesToLocalSender(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())

	ensureLocalEndpoint()

	sender, model, provName, err := resolveProviderAndModel("", "")
	if err != nil {
		t.Fatalf("resolveProviderAndModel: %v", err)
	}
	if provName != "local" {
		t.Errorf("provName = %q, want local", provName)
	}
	if model == "" {
		t.Error("model = empty, want a buding model")
	}
	if sender == nil {
		t.Error("sender = nil, want a local sender (not onboarding)")
	}
}

// TestEnsureLocalEndpointIdempotentAndAdditive verifies re-seeding never
// duplicates buding, and that seeding into a config with an existing endpoint
// preserves that endpoint.
func TestEnsureLocalEndpointIdempotentAndAdditive(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())

	ensureLocalEndpoint() // seed once
	cfg, _ := config.Load()
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Simulate a user-configured real endpoint existing alongside buding.
	cfg.Endpoints = append(cfg.Endpoints, config.Endpoint{
		ID:       "real",
		Provider: "openai",
		APIKey:   "sk-test",
		Models:   []config.EndpointModel{{Model: "gpt-test"}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save with real endpoint: %v", err)
	}

	ensureLocalEndpoint() // re-seed should be a no-op for buding

	cfg2, _ := config.Load()
	if n := countEndpoint(cfg2, localEndpointID); n != 1 {
		t.Errorf("buding endpoint count = %d, want 1", n)
	}
	if n := countEndpoint(cfg2, "real"); n != 1 {
		t.Errorf("real endpoint count = %d, want 1 (must be preserved)", n)
	}
}

func findEndpoint(cfg config.Config, id string) *config.Endpoint {
	for i := range cfg.Endpoints {
		if cfg.Endpoints[i].ID == id {
			return &cfg.Endpoints[i]
		}
	}
	return nil
}

func countEndpoint(cfg config.Config, id string) int {
	n := 0
	for i := range cfg.Endpoints {
		if cfg.Endpoints[i].ID == id {
			n++
		}
	}
	return n
}

func endpointModelIDs(ep *config.Endpoint) []string {
	ids := make([]string, 0, len(ep.Models))
	for _, m := range ep.Models {
		ids = append(ids, m.Model)
	}
	return ids
}
