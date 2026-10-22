package server

import (
	"context"
	"fmt"

	"github.com/open-octo/octo-agent/internal/permission"
)

// OCTO-FORK: resolve only the outstanding tool approval for this session.
// Other confirmations and explicit policy denies keep their existing behavior.
func (s *Server) resolvePendingPermissionOnAuto(sessionID string) {
	s.sessionAgentsMu.Lock()
	live := s.liveSessions[sessionID]
	auto := live != nil && live.PermissionModeValue() == string(permission.ModeAutoApprove)
	s.sessionAgentsMu.Unlock()
	if !auto {
		return
	}
	s.pendingPromptMu.Lock()
	pending, ok := s.pendingConfirms[sessionID]
	s.pendingPromptMu.Unlock()
	if ok && pending.Kind == "yes_no_always" && pending.ToolName != "" {
		s.handleWSConfirmation(pending.ConfID, "yes")
	}
}

// Consume exactly once. An expired or already answered id is inert.
func (s *Server) deliverConfirmationOnce(confID, result string) bool {
	s.confirmMu.Lock()
	defer s.confirmMu.Unlock()
	ch, ok := s.confirmations[confID]
	if !ok {
		return false
	}
	delete(s.confirmations, confID)
	select {
	case ch <- result:
		return true
	default:
		return false
	}
}

// Cancellation wins over a racing approval before the waiter resumes execution.
func confirmationResult(ctx context.Context, result string, dismiss func()) (string, error) {
	if ctx.Err() != nil {
		dismiss()
		return "", fmt.Errorf("confirmation cancelled")
	}
	return result, nil
}

func webCreationPermission(requested string) (string, error) {
	if requested == "" {
		return string(resolvePermissionMode()), nil
	}
	switch requested {
	case "interactive", "auto", "strict":
		return requested, nil
	}
	return "", fmt.Errorf("permission_mode must be interactive, auto, or strict")
}

func (s *Server) updateLivePermission(id, mode string) {
	// OCTO-FORK: update the live session and gate as well as the persisted copy;
	// otherwise a running turn keeps the old mode and may save it back at turn end.
	s.sessionAgentsMu.Lock()
	if live := s.liveSessions[id]; live != nil {
		live.SetPermissionModeValue(mode)
	}
	if liveAgent := s.sessionAgents[id]; liveAgent != nil && liveAgent.Gate != nil {
		if setter, ok := liveAgent.Gate.(interface{ SetMode(permission.Mode) }); ok {
			setter.SetMode(permission.Mode(mode))
		}
	}
	s.sessionAgentsMu.Unlock()
	if mode == string(permission.ModeAutoApprove) {
		s.resolvePendingPermissionOnAuto(id)
	}

}
