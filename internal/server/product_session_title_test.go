package server

import (
	"context"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

func TestProductSessionTitleDoesNotCallGateway(t *testing.T) {
	s := &Server{}
	messages := []agent.Message{agent.NewUserMessage("Create an onboarding presentation")}
	// A nil agent proves title generation needs neither a model nor credentials.
	title, err := s.generateSessionTitle(context.Background(), nil, messages, true)
	if err != nil || title != agent.FirstUserSnippet(messages) {
		t.Fatalf("title = %q, error = %v", title, err)
	}
}
