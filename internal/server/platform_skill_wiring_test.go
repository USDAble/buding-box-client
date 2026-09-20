package server

import (
	"context"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

// OCTO-FORK: platform sessions must not advertise skills without an authenticated loader.
func TestPreparePlatformToolTurnRequiresLoader(t *testing.T) {
	s := &Server{}
	ctx := context.WithValue(context.Background(), ctxKeySessionID{}, "session")
	_, _, _, cleanup, err := s.prepareToolTurn(ctx, nil, &agent.Session{AgentID: "platform:expert_test:7"})
	cleanup()
	if err == nil || !strings.Contains(err.Error(), "platform skill loader unavailable") {
		t.Fatalf("expected missing platform loader error, got %v", err)
	}
}
