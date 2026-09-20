package productruntime

import (
	"log/slog"
	"net/http"
)

// Agreements are readable before login; opening one never records consent.
func (rt *Runtime) handleAgreement(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	kind := r.PathValue("kind")
	if kind != "box" && kind != "privacy" {
		writeCode(w, http.StatusBadRequest, "invalid_agreement_kind", nil)
		return
	}
	if rt.deps.Platform == nil {
		writeControlPlaneUnconfigured(w)
		return
	}
	agreement, err := rt.deps.Platform.Agreement(r.Context(), kind)
	if err != nil {
		slog.Warn("product: agreement read failed", "kind", kind, "error", err)
		writePlatformError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agreement)
}
