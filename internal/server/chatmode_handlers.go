// Fork-owned file: the session-level chat mode route.
//
// It lives here, rather than folded into upstream handlers.go, because
// scripts/server-diff-guard.mjs already registers exactly this path — it exists
// only because of the fork's product layer and is scheduled to leave
// internal/server entirely (convergence P0-01A D + P0-04). Keeping the ~85-line
// handler in the upstream file cost 87 lines of its 83-line fork-diff ceiling;
// the field it adds to the session descriptor is the part that genuinely has to
// stay there (sessionItem is defined in handlers.go and there is no seam for
// "add a field to a response struct" that would not create a second place
// defining the same shape).
//

package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/chatmode"
)

//
// OCTO-FORK: sets this session's chat mode (需求基线 B5 规则 6 — the mode is
// one of two independent session attributes, the other being the model
// binding). Mirrors handleUpdateSessionPermissionMode: the same
// load-under-binding / validate / set / broadcast shape, because the value
// set comes from the package that owns it in both cases (internal/chatmode
// here, internal/permission there). See
// dev-docs-usdable/需求/20260911/本地API契约.md §1.5.

type updateSessionChatModeRequest struct {
	ChatMode string `json:"chat_mode"`
}

func (s *Server) handleUpdateSessionChatMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}

	var req updateSessionChatModeRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}

	mode := strings.ToLower(strings.TrimSpace(req.ChatMode))
	if !chatmode.IsProductMode(mode) {
		// Validated against the owner of the mode set (internal/chatmode), not
		// against the signed catalog: the mode ids are product constants, and
		// the catalog only says which mode each model belongs to.
		writeError(w, http.StatusBadRequest, "chat_mode must be privacy, smart, or default")
		return
	}

	if _, err := agent.LoadSession(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if ok, _, berr := s.acquireSessionBinding(id, agent.EntryWeb, false); !ok {
		writeError(w, http.StatusConflict, berr.Error())
		return
	}
	defer s.releaseSessionBinding(id, agent.EntryWeb)

	// Reload after acquiring the binding in case another process saved.
	sess, err := agent.LoadSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := sess.SetChatMode(mode); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save session: %v", err))
		return
	}

	// Push the new mode so another window (or another session's tab) can tell
	// which mode this one is in — P10 makes that visible on purpose: with a
	// dozen sessions in the sidebar, the mode is a per-session fact the user
	// must be able to see without opening each one.
	if s.wsHub != nil {
		s.wsHub.broadcast(id, map[string]any{
			"type":       "session_update",
			"session_id": id,
			"chat_mode":  mode,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"chat_mode": mode,
	})
}
