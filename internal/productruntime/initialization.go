package productruntime

import (
	"context"
	"net/http"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
)

type initializationResult struct {
	name  string
	value any
	err   error
}

// handleInitialize keeps optional workspace dependencies outside authentication.
// Each operation owns its existing cache and failure policy; no fallback data is
// synthesized here. The bounded requests run together on login and restoration.
func (rt *Runtime) handleInitialize(w http.ResponseWriter, r *http.Request) {
	if !rt.deps.State.PublicState().LoggedIn {
		writeCode(w, http.StatusUnauthorized, productclient.CodeUnauthorized, nil)
		return
	}
	if rt.deps.Platform == nil {
		writeControlPlaneUnconfigured(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	results := make(chan initializationResult, 4)
	go func() {
		outcome, err := rt.refreshCatalog(ctx, true)
		if err != nil {
			rt.logCatalogOutcome(outcome, err)
		}
		results <- initializationResult{"catalog", rt.currentCatalogAvailability().toDTO(), err}
	}()
	go func() {
		value, err := rt.refreshCredits(ctx)
		results <- initializationResult{"credits", value, err}
	}()
	go func() {
		value, err := rt.deps.Platform.Box(ctx)
		results <- initializationResult{"box", value, err}
	}()
	go func() {
		value, err := rt.refreshServerDictionary(ctx)
		results <- initializationResult{"dictionary", value, err}
	}()
	modules := map[string]string{}
	response := map[string]any{"modules": modules}
	var expired error
	for range 4 {
		result := <-results
		modules[result.name] = "ready"
		if result.err != nil {
			modules[result.name] = "unavailable"
			if IsSessionExpired(result.err) {
				expired = result.err
			}
		}
		switch result.name {
		case "catalog":
			response["catalog"] = result.value
		case "box":
			if result.err == nil {
				response["box"] = result.value
			}
		case "dictionary":
			response["dictionaryNotice"] = result.value
		}
	}
	if expired != nil {
		rt.failPlatform(w, expired)
		return
	}
	response["state"] = rt.deps.State.PublicState()
	writeJSON(w, http.StatusOK, response)
}
