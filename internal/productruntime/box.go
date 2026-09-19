package productruntime

import (
	"net/http"
)

// handleBox exposes the account-authorised box projection. It intentionally
// does not cache or invent a local status: a failed read is not evidence that a
// box is offline, and productstub is the only source of development fixtures.
func (rt *Runtime) handleBox(w http.ResponseWriter, r *http.Request) {
	if rt.deps.Platform == nil {
		writeControlPlaneUnconfigured(w)
		return
	}
	box, err := rt.deps.Platform.Box(r.Context())
	if err != nil {
		rt.failPlatform(w, err)
		return
	}
	writeJSON(w, http.StatusOK, box)
}
