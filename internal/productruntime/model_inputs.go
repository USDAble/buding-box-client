package productruntime

import (
	"context"

	"github.com/open-octo/octo-agent/internal/agent"
)

// ModelImageInputStatus shares the existing catalog owner and this turn's
// immutable effort. Separate tools and vision successes are never combined.
func (rt *Runtime) ModelImageInputStatus(ctx context.Context, model, effort string, withTools bool) agent.ImageInputStatus {
	status := agent.ImageInputStatus{Known: true, Reason: "capability_not_verified"}
	if err := rt.ValidateModelReasoning(ctx, model, effort); err != nil {
		status.Reason = "model_or_catalog_unavailable"
		return status
	}
	qualification, known := rt.CatalogModel(model)
	if !known || !qualification.Current || !qualification.Selectable {
		status.Reason = "model_or_catalog_unavailable"
		return status
	}
	for _, combination := range qualification.CapabilityCombinations {
		if combination.ReasoningEffort == effort && combination.Tools == withTools && combination.Vision {
			return agent.ImageInputStatus{Known: true, Allowed: true}
		}
	}
	return status
}
