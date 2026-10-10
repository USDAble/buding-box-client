// OCTO-FORK: gateway effort must stay explicit and validation applies to every send path.
package app

import (
	"context"
	"errors"
	"github.com/open-octo/octo-agent/internal/agent"
	"testing"
)

func TestGatewayReasoningGuardCoversEverySendPath(t *testing.T) {
	blocked := errors.New("choose a supported reasoning level")
	fake := &streamingMockProvider{}
	s := sender{p: fake, reasoningEffort: "high", gatewayReasoningPassthrough: true, validateRequest: &requestValidation{check: func(model string) error {
		if model != "model-a" {
			t.Fatal(model)
		}
		return blocked
	}}}
	ctx := context.Background()
	checks := []func() (agent.Reply, error){
		func() (agent.Reply, error) { return s.SendMessages(ctx, "model-a", "", nil, 16) },
		func() (agent.Reply, error) { return s.StreamMessages(ctx, "model-a", "", nil, 16, nil, nil) },
		func() (agent.Reply, error) { return s.SendMessagesWithTools(ctx, "model-a", "", nil, 16, nil) },
		func() (agent.Reply, error) {
			return s.StreamMessagesWithTools(ctx, "model-a", "", nil, 16, nil, nil, nil, nil)
		},
	}
	for _, check := range checks {
		if _, err := check(); !errors.Is(err, blocked) {
			t.Fatal(err)
		}
	}
	if fake.gotReq.Model != "" || fake.streamCalled {
		t.Fatal("invalid choice reached provider")
	}
	if s.LowEffort().(sender).reasoningEffort != "high" || s.NoReasoning().(sender).reasoningEffort != "high" {
		t.Fatal("gateway helper silently changed effort")
	}
}

// The server compares agent.Sender interfaces when checking cache/fallback identity.
// Both a plain sender and a product sender must remain comparable.
func TestSenderIdentityRemainsComparableWithRequestValidation(t *testing.T) {
	for _, validate := range []func(string) error{nil, func(string) error { return nil }} {
		original, err := NewSender(SenderOptions{Provider: ProviderOpenAI, APIKey: "fixture", ValidateRequest: validate})
		if err != nil {
			t.Fatal(err)
		}
		same := original
		if original != same {
			t.Fatal("copied sender lost its identity")
		}
		value := original.(sender)
		copyOfValue := value
		if value != copyOfValue {
			t.Fatal("sender value lost its identity")
		}
	}
}
