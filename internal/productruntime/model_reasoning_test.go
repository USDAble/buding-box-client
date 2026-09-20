package productruntime

import (
	"reflect"
	"testing"

	"github.com/open-octo/octo-agent/internal/productstate"
)

func TestReasoningMetadataAndPerModelPreferences(t *testing.T) {
	policy := fixtureCatalogPolicy(t)
	choices := []string{"default", "off", "low", "high", "max"}
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
	if rt.reasoningPreference("model-a", choices) != "max" || rt.reasoningPreference("model-b", choices) != "off" || rt.reasoningPreference("model-c", choices) != "default" {
		t.Fatal("model preferences leaked")
	}
	if rt.reasoningPreference("model-a", []string{"default"}) != "default" {
		t.Fatal("removed option remained active")
	}
	cloned := state.State()
	cloned.Prefs.ModelReasoning["model-a"] = "off"
	if rt.reasoningPreference("model-a", choices) != "max" {
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
	if rt.reasoningPreference("model-b", choices) != "default" || rt.reasoningPreference("model-a", choices) != "max" {
		t.Fatal("default and off conflated")
	}
}
