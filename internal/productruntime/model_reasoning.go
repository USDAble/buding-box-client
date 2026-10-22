package productruntime

import (
	"context"
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

// The existing UI distinguishes directory renewal failure from a withdrawn model.
type catalogSelectionError struct{}

func (catalogSelectionError) Error() string {
	return "model catalog is unavailable or expired; refresh before sending"
}
func (catalogSelectionError) ErrorCode() string { return "catalog_unavailable" }

// ModelReasoning snapshots a model's choice for one user turn. Invalid saved
// choices stay visible in the catalog until the user explicitly reselects.
func (rt *Runtime) ModelReasoning(model string) (string, error) {
	model = strings.TrimPrefix(model, productprofile.GatewayModelPrefix())
	effort := rt.reasoningPreference(model)
	// Sender construction has no caller context. Snapshot and local validation
	// stay immediate; the actual request owns any cancellable directory refresh.
	return effort, rt.validateModelReasoningSnapshot(model, effort)
}

// ValidateModelReasoning rechecks the fixed turn choice before each request.
func (rt *Runtime) ValidateModelReasoning(ctx context.Context, model, effort string) error {
	// OCTO-FORK: tool execution may outlast a signed catalog lease. Renew through
	// the same owner before every paid request, retaining this turn's model and
	// effort. Cancellation stops this waiter without cancelling another sender.
	if _, err := rt.refreshCatalogOnDemand(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return catalogSelectionError{}
	}
	return rt.validateModelReasoningSnapshot(model, effort)
}

func (rt *Runtime) validateModelReasoningSnapshot(model, effort string) error {
	model = strings.TrimPrefix(model, productprofile.GatewayModelPrefix())
	status, known := rt.CatalogModel(model)
	if !known || !status.Current {
		return catalogSelectionError{}
	}
	if !status.Selectable {
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
