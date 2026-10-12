package productruntime

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/open-octo/octo-agent/internal/productprofile"
)

// Only platform effort levels are exposed; the platform owns vendor translation.
func reasoningOptions(declared []string) []string {
	out := []string{"default"}
	for _, choice := range []string{"low", "medium", "high"} {
		if slices.Contains(declared, choice) {
			out = append(out, choice)
		}
	}
	return out
}
func (rt *Runtime) reasoningPreference(model string) string {
	if rt.deps.State != nil {
		if value := rt.deps.State.State().Prefs.ModelReasoning[model]; value != "" {
			return value
		}
	}
	return "default"
}

// reasoningSelectionError uses the existing coded turn-error contract for UI copy.
type reasoningSelectionError struct{}

func (reasoningSelectionError) Error() string {
	return "selected reasoning level is no longer available; choose a reasoning level before sending"
}
func (reasoningSelectionError) ErrorCode() string { return "reasoning_selection_required" }

// ModelReasoning snapshots a model's choice for one user turn. Invalid saved
// choices stay visible in the catalog until the user explicitly reselects.
func (rt *Runtime) ModelReasoning(model string) (string, error) {
	model = strings.TrimPrefix(model, productprofile.GatewayModelPrefix())
	effort := rt.reasoningPreference(model)
	return effort, rt.ValidateModelReasoning(model, effort)
}

// ValidateModelReasoning rechecks the fixed turn choice before each request.
func (rt *Runtime) ValidateModelReasoning(model, effort string) error {
	model = strings.TrimPrefix(model, productprofile.GatewayModelPrefix())
	status, known := rt.CatalogModel(model)
	if !known || !status.Current || !status.Selectable {
		return errors.New("model_unavailable: refresh the model list and choose an available model")
	}
	if !slices.Contains(status.ReasoningOptions, effort) {
		return reasoningSelectionError{}
	}
	return nil
}
func (rt *Runtime) handleModelReasoning(w http.ResponseWriter, r *http.Request) {
	if !rt.financeReady(w) {
		return
	}
	var input struct {
		ModelID string `json:"modelId"`
		Effort  string `json:"effort"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
		writeCode(w, 400, "invalid_request", nil)
		return
	}
	input.ModelID = strings.TrimPrefix(input.ModelID, productprofile.GatewayModelPrefix())
	status, known := rt.CatalogModel(input.ModelID)
	if !known || !status.Current || !status.Selectable {
		writeCode(w, 400, "model_unavailable", nil)
		return
	}
	if !slices.Contains(status.ReasoningOptions, input.Effort) {
		writeCode(w, 400, "unsupported_reasoning_effort", nil)
		return
	}
	if err := rt.deps.State.SetModelReasoning(input.ModelID, input.Effort); err != nil {
		writeCode(w, 500, "state_write_failed", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"modelId": input.ModelID, "effort": input.Effort})
}
