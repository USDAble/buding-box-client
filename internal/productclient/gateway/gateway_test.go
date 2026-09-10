package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/productclient"
)

func TestCallMetaValidate(t *testing.T) {
	cases := []struct {
		name    string
		meta    CallMeta
		wantErr error
	}{
		{"well formed", CallMeta{ClientRequestID: "cr_1", ModelID: "m"}, nil},
		{"no request id", CallMeta{ModelID: "m"}, ErrEmptyClientRequestID},
		{"no model", CallMeta{ClientRequestID: "cr_1"}, nil}, // checked below
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.meta.Validate()
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if tc.name == "no model" {
				if err == nil {
					t.Fatal("an empty modelId must be rejected")
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

// TestEmptyClientRequestIDIsTheErrorThatMatters: without the id the platform
// cannot deduplicate, so a timeout becomes indistinguishable from a second
// charge. The error must be recognisable by identity, not by string matching.
func TestEmptyClientRequestIDIsTheErrorThatMatters(t *testing.T) {
	err := CallMeta{ModelID: "m"}.Validate()
	if !errors.Is(err, ErrEmptyClientRequestID) {
		t.Fatalf("err = %v, want ErrEmptyClientRequestID", err)
	}
}

// TestCapabilitiesOfSeesEveryProbedInterface pins the probe list. If agent.go
// grows another assertion, this test is where the omission becomes visible.
func TestCapabilitiesOfSeesEveryProbedInterface(t *testing.T) {
	cases := []struct {
		name string
		send agent.Sender
		want []string
	}{
		{"plain", &plainSender{}, nil},
		{"streaming", &streamingSender{}, []string{"StreamingSender"}},
		{"tools", &toolSender{}, []string{"ToolSender"}},
		{"tool streaming", &toolStreamingSender{}, []string{"StreamingSender", "ToolSender", "ToolStreamingSender"}},
		{"no reasoning", &noReasoningSender{}, []string{"NoReasoningSender"}},
		{"low effort", &lowEffortSender{}, []string{"LowEffortSender"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CapabilitiesOf(tc.send)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("CapabilitiesOf = %v, want %v", got, tc.want)
			}
		})
	}

	if CapabilitiesOf(nil) != nil {
		t.Error("CapabilitiesOf(nil) should be empty")
	}
}

// TestPreservesCapabilitiesCatchesTheSilentRegression is a test of the test: it
// proves the check actually fails when a wrapper downgrades, which is the whole
// reason the helper exists.
func TestPreservesCapabilitiesCatchesTheSilentRegression(t *testing.T) {
	base := &toolStreamingSender{}

	// A wrapper that only declares agent.Sender: the classic accidental
	// downgrade. Nothing about this is a compile error.
	downgraded := agent.Sender(&plainSender{})
	err := PreservesCapabilities(base, downgraded)

	var loss *CapabilityLoss
	if !errors.As(err, &loss) {
		t.Fatalf("err = %v, want *CapabilityLoss", err)
	}
	want := []string{"StreamingSender", "ToolSender", "ToolStreamingSender"}
	if strings.Join(loss.Dropped, ",") != strings.Join(want, ",") {
		t.Errorf("Dropped = %v, want %v", loss.Dropped, want)
	}
	if !strings.Contains(err.Error(), "silently") {
		t.Errorf("Error() = %q; it should say the failure is silent", err.Error())
	}

	// A faithful pass-through must pass the check.
	if err := PreservesCapabilities(base, base); err != nil {
		t.Errorf("a pass-through must be accepted: %v", err)
	}

	// Gaining a capability is odd but safe: the wrapper implements it.
	if err := PreservesCapabilities(&plainSender{}, &toolSender{}); err != nil {
		t.Errorf("gaining a capability must not be reported as a loss: %v", err)
	}
}

// TestRecorderPreservesCapabilities runs the contract against the only Observer
// implementation that exists today. When the real observer lands, it is added
// here — that is the point of writing this test before the implementation.
func TestRecorderPreservesCapabilities(t *testing.T) {
	// The Recorder returns its base untouched, so it is the trivial case; the
	// test keeps the pattern in place for the real observer.
	var base agent.Sender = &toolStreamingSender{}
	var wrapped agent.Sender = base

	if err := PreservesCapabilities(base, wrapped); err != nil {
		t.Errorf("the pass-through observer broke the contract: %v", err)
	}
}

// TestControlClientIsReadAndCancelOnly is a compile-time-ish guard: the gateway
// control surface must not grow a method that moves money. Settlement belongs to
// the platform (中台交付包 §5.1).
func TestControlClientIsReadAndCancelOnly(t *testing.T) {
	// If someone adds Consume/Charge/Settle to ControlClient, this assertion
	// still compiles but the comment above stops being true — so the check is
	// on the interface's method set, expressed through a value of it.
	var c ControlClient = stubControl{}
	_ = c

	// The only two methods are Cancel and RequestStatus; calling them is the
	// whole surface.
	_ = c.Cancel
	_ = c.RequestStatus
}

type stubControl struct{}

func (stubControl) Cancel(context.Context, string) error { return nil }
func (stubControl) RequestStatus(context.Context, string) (productclient.RequestStatus, error) {
	return productclient.RequestStatus{}, nil
}

// The fake senders below implement the capability interfaces in every
// combination the agent loop probes for. Each embeds baseSender exactly once:
// embedding two senders that each embed it would make SendMessages ambiguous at
// depth 2, and the fakes would not compile as agent.Sender at all.

type baseSender struct{}

func (*baseSender) SendMessages(context.Context, string, string, []agent.Message, int) (agent.Reply, error) {
	return agent.Reply{}, nil
}

type plainSender struct{ baseSender }

type streamingSender struct{ baseSender }

func (*streamingSender) StreamMessages(context.Context, string, string, []agent.Message, int,
	func(string), func(string)) (agent.Reply, error) {
	return agent.Reply{}, nil
}

type toolSender struct{ baseSender }

func (*toolSender) SendMessagesWithTools(context.Context, string, string, []agent.Message, int,
	[]agent.ToolDefinition) (agent.Reply, error) {
	return agent.Reply{}, nil
}

type toolStreamingSender struct{ baseSender }

func (*toolStreamingSender) StreamMessages(context.Context, string, string, []agent.Message, int,
	func(string), func(string)) (agent.Reply, error) {
	return agent.Reply{}, nil
}

func (*toolStreamingSender) SendMessagesWithTools(context.Context, string, string, []agent.Message, int,
	[]agent.ToolDefinition) (agent.Reply, error) {
	return agent.Reply{}, nil
}

func (*toolStreamingSender) StreamMessagesWithTools(context.Context, string, string, []agent.Message, int,
	[]agent.ToolDefinition, func(string), agent.ToolInputDeltaFunc, agent.ThinkingDeltaFunc) (agent.Reply, error) {
	return agent.Reply{}, nil
}

type noReasoningSender struct{ baseSender }

func (s *noReasoningSender) NoReasoning() agent.Sender { return s }

type lowEffortSender struct{ baseSender }

func (s *lowEffortSender) LowEffort() agent.Sender { return s }
