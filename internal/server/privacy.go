package server

import (
	"context"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/chatmode"
)

// withSessionPrivacy resolves P9's per-session mode, including the legacy
// empty-mode fallback, and scopes P10 masking to one provider call chain.
// OCTO-FORK: P10 隐私模式与 PII 处理.
func (s *Server) withSessionPrivacy(ctx context.Context, sess *agent.Session) context.Context {
	mode := ""
	if sess != nil {
		mode = sess.ChatMode
	}
	if mode == "" {
		mode = s.defaultChatMode()
	}
	return app.WithPrivacy(ctx, mode == chatmode.ModePrivacy)
}
