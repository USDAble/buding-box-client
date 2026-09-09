package app

import (
	"context"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

// TestNewSenderLocalBuildsKeylessProvider pins the P11 wiring: provider "local"
// builds without a key/base URL, and the resulting sender is lifted to the
// FULL agent sender stack (StreamingSender + ToolStreamingSender), so the fake
// channel drives the identical streaming path a real provider would. The
// design (§3.1) explicitly requires this — a Provider-only fake would bypass
// P8's StreamFilter and P10's decorators in demos.
func TestNewSenderLocalBuildsKeylessProvider(t *testing.T) {
	s, err := NewSender(SenderOptions{Provider: ProviderLocal})
	if err != nil {
		t.Fatalf("NewSender(local): %v", err)
	}
	if _, ok := s.(agent.StreamingSender); !ok {
		t.Errorf("sender does not implement agent.StreamingSender")
	}
	if _, ok := s.(agent.ToolStreamingSender); !ok {
		t.Errorf("sender does not implement agent.ToolStreamingSender")
	}
}

// TestLocalSenderReplyHitsSensitiveFilter verifies the local sender's reply is
// always hit by the output filter — the "验收能看到打码" behaviour, frozen as
// a test (§6).
func TestLocalSenderReplyHitsSensitiveFilter(t *testing.T) {
	s, err := NewSender(SenderOptions{Provider: ProviderLocal})
	if err != nil {
		t.Fatalf("NewSender(local): %v", err)
	}
	engine := sensitive.New("")

	reply, err := s.SendMessages(context.Background(), "buding-local-general", "", []agent.Message{
		{Role: agent.RoleUser, Content: "随便聊聊"},
	}, 128)
	if err != nil {
		t.Fatalf("SendMessages: %v", err)
	}
	if !engine.Filter(reply.Content).Matched() {
		t.Errorf("local reply %q was not hit by the sensitive filter", reply.Content)
	}
}

// TestBuildClientLocalKeyless confirms buildClient accepts "local" with an
// empty key (unlike every named cloud vendor, which would error).
func TestBuildClientLocalKeyless(t *testing.T) {
	p, err := buildClient(ProviderLocal, "", "", "", nil)
	if err != nil {
		t.Fatalf("buildClient(local): %v", err)
	}
	if p.Name() != "local" {
		t.Errorf("Name() = %q, want local", p.Name())
	}
}
