package server

import (
	"context"

	"github.com/open-octo/octo-agent/internal/agent"
)

// Product gateway calls share a billed turn and cannot run an unrelated title
// request concurrently. Use the existing snippet without another model call.
func (s *Server) generateSessionTitle(ctx context.Context, a *agent.Agent, messages []agent.Message, gateway bool) (string, error) {
	if s.cfg.RequireGateway || gateway {
		return agent.FirstUserSnippet(messages), nil
	}
	return a.GenerateTitleOrSnippet(ctx, messages)
}
