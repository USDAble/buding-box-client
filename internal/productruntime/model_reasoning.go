package productruntime

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/open-octo/octo-agent/internal/productprofile"
)

func reasoningOptions(declared []string) []string {
	out := []string{"default"}
	for _, choice := range declared {
		switch choice {
		case "off", "minimal", "low", "medium", "high", "xhigh", "max":
			if !slices.Contains(out, choice) {
				out = append(out, choice)
			}
		}
	}
	return out
}
func (rt *Runtime) reasoningPreference(model string, options []string) string {
	if rt.deps.State == nil {
		return "default"
	}
	value := rt.deps.State.State().Prefs.ModelReasoning[model]
	if value != "" && slices.Contains(options, value) {
		return value
	}
	return "default"
}

// ModelReasoning is the sender's single source of model-specific effort. A
// missing or changed declaration returns default, never another model's choice.
func (rt *Runtime) ModelReasoning(model string) string {
	model = strings.TrimPrefix(model, productprofile.GatewayModelPrefix())
	status, known := rt.CatalogModel(model)
	if !known || !status.Current || !status.Selectable {
		return "default"
	}
	return rt.reasoningPreference(model, status.ReasoningOptions)
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
