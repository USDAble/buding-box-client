package productruntime

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
	"github.com/open-octo/octo-agent/internal/productstate"
)

func TestReasoningMetadataAndPerModelPreferences(t *testing.T) {
	policy := fixtureCatalogPolicy(t)
	choices := []string{"default", "low", "medium", "high"}
	policy.Catalog.Models[0].ReasoningOptions = choices
	vendors, err := projectCatalog(policy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(vendors[0].Models[0].ReasoningOptions, choices) {
		t.Fatal("signed reasoning metadata was lost")
	}
	if !reflect.DeepEqual(vendors[0].Models[1].ReasoningOptions, []string{"default"}) {
		t.Fatal("unknown model invented reasoning choices")
	}
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())
	state, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rt := New(Deps{State: state})
	if err = state.SetModelReasoning("model-a", "max"); err != nil {
		t.Fatal(err)
	}
	if err = state.SetModelReasoning("model-b", "off"); err != nil {
		t.Fatal(err)
	}
	if rt.reasoningPreference("model-a") != "max" || rt.reasoningPreference("model-b") != "off" || rt.reasoningPreference("model-c") != "default" {
		t.Fatal("model preferences leaked")
	}
	if rt.reasoningPreference("model-a") != "max" {
		t.Fatal("invalid saved option must stay visible until reselected")
	}
	cloned := state.State()
	cloned.Prefs.ModelReasoning["model-a"] = "off"
	if rt.reasoningPreference("model-a") != "max" {
		t.Fatal("returned state map mutated store")
	}
	reopened, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.State().Prefs.ModelReasoning["model-a"] != "max" {
		t.Fatal("model preference did not persist")
	}
	if err = state.SetModelReasoning("model-b", "default"); err != nil {
		t.Fatal(err)
	}
	if rt.reasoningPreference("model-b") != "default" || rt.reasoningPreference("model-a") != "max" {
		t.Fatal("default and off conflated")
	}
}

func TestReasoningCatalogChangeRequiresExplicitReselection(t *testing.T) {
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
	model := policy.Catalog.Models[0].ID
	publish := func(options []string, revision int) {
		t.Helper()
		policy.Catalog.Models[0].ReasoningOptions = options
		raw, err := json.Marshal(policy)
		if err != nil {
			t.Fatal(err)
		}
		signature, err := productclient.SignPolicy(raw, clienttest.FixtureSigningKeyID, clienttest.FixtureSigningKey())
		if err != nil {
			t.Fatal(err)
		}
		entry.CatalogVersion = fmt.Sprintf("2026-09-11.%d", revision)
		entry.Envelope = productclient.PolicyEnvelope{Policy: raw, Signature: signature}
		if err := f.store.Put(entry); err != nil {
			t.Fatal(err)
		}
	}
	publish([]string{"high", "max", "low", "low", "off"}, 2)
	status, _ := f.rt.CatalogModel(model)
	if !reflect.DeepEqual(status.ReasoningOptions, []string{"default", "low", "high"}) {
		t.Fatal(status.ReasoningOptions)
	}
	if effort, err := f.rt.ModelReasoning(model); effort != "default" || err != nil {
		t.Fatalf("unset preference = %s, %v", effort, err)
	}
	if err := f.rt.deps.State.SetModelReasoning(model, "high"); err != nil {
		t.Fatal(err)
	}
	if effort, err := f.rt.ModelReasoning(model); effort != "high" || err != nil {
		t.Fatalf("choice = %s, %v", effort, err)
	}
	publish([]string{"low"}, 3)
	if effort, err := f.rt.ModelReasoning(model); effort != "high" || agent.ErrorCodeOf(err) != "reasoning_selection_required" {
		t.Fatalf("removed choice silently changed: %s, %v", effort, err)
	}
	if err := f.rt.ValidateModelReasoning(model, "high"); agent.ErrorCodeOf(err) != "reasoning_selection_required" {
		t.Fatal(err)
	}
	if err := f.rt.deps.State.SetModelReasoning(model, "low"); err != nil {
		t.Fatal(err)
	}
	if effort, err := f.rt.ModelReasoning(model); effort != "low" || err != nil {
		t.Fatalf("reselection = %s, %v", effort, err)
	}
	if _, err := f.rt.ModelReasoning("missing"); err == nil {
		t.Fatal("missing model accepted")
	}
}
