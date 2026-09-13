package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
)

// These nails cover PR-5a's routing half: the decision PR-4c0 put in
// senderForSession now has somewhere to send the turn, and the three ways that
// can still end in a refusal must each stay a refusal rather than becoming a
// nil dereference.
//
// They reuse countingSender and boundSession from gateway_model_guard_test.go on
// purpose: the point is that the SAME resolution point behaves coherently across
// the two PRs, and a second stub would let the halves drift.

// emptyProfileServer is a server in the state a product build starts in: a
// gateway factory may be wired, and data/config.yml names no provider at all
// (which is B4's requirement, not an accident).
//
// It pins the provider and clears that provider's key env var, because the
// alternative is a fixture whose result depends on the developer's shell: a
// machine with ANTHROPIC_API_KEY exported would find a key that CI does not,
// and the two nails below would pass locally and fail in CI (or the reverse).
// Pinning the provider is what makes resolveAPIKey reach a defined answer
// rather than inheriting whatever the default entry happens to name.
func emptyProfileServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("OCTO_DATA_ROOT", tmp)
	t.Setenv("USERPROFILE", tmp)
	if cfg.Provider == "" {
		cfg.Provider = "anthropic"
	}
	t.Setenv(app.VendorAPIKeyEnvVar(cfg.Provider), "")
	t.Setenv(strings.ToUpper(cfg.Provider)+"_MODEL", "")
	return mustServer(t, cfg)
}

// withoutADefaultSender is emptyProfileServer plus the one state mustServer
// cannot express: it seeds a stubSender, so "this build has no provider at all"
// has to be stated explicitly. That state is the product's normal one (B4), and
// it is the state V-36's two nails are about.
func withoutADefaultSender(t *testing.T, cfg Config) *Server {
	t.Helper()
	srv := emptyProfileServer(t, cfg)
	srv.sender = nil
	return srv
}

// TestAGatewayBoundTurnRunsOnTheInjectedSender is the main nail. The turn must
// reach the sender the product injected, and it must carry the bare catalog id:
// the gateway has never heard of "buding-gateway::buding-privacy-1", which names
// a local endpoint (C1).
func TestAGatewayBoundTurnRunsOnTheInjectedSender(t *testing.T) {
	injected := &countingSender{}
	srv := emptyProfileServer(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
		GatewaySender:      func(app.ReasoningTuning) (agent.Sender, error) { return injected, nil },
	})

	sess := boundSession("buding-gateway::buding-privacy-1", "")
	sender, model := srv.senderForSession(sess)

	if sender != agent.Sender(injected) {
		t.Fatal("a gateway-bound turn did not run on the injected sender")
	}
	if model != "buding-privacy-1" {
		t.Errorf("model = %q, want the bare catalog id", model)
	}
	if _, err := sender.SendMessages(context.Background(), model, "", nil, 0); err != nil {
		t.Fatalf("the turn must run on the gateway: %v", err)
	}
	if injected.calls != 1 {
		t.Errorf("injected sender got %d turn(s), want 1", injected.calls)
	}
}

// C2 规则 2 is why the seam is a factory: the sender is built from the token that
// is current NOW, so the token a refresh produced between turns has to be picked
// up with no invalidation step. Asking once per resolution is what makes that
// work, so it is asserted rather than assumed.
func TestTheFactoryIsAskedAgainForEachTurn(t *testing.T) {
	asked := 0
	srv := emptyProfileServer(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
		GatewaySender: func(app.ReasoningTuning) (agent.Sender, error) {
			asked++
			return &countingSender{}, nil
		},
	})

	sess := boundSession("buding-gateway::buding-privacy-1", "")
	srv.senderForSession(sess)
	srv.senderForSession(sess)

	if asked != 2 {
		t.Errorf("the factory was asked %d time(s) across two turns, want 2 — a cached sender would present an expired token", asked)
	}
}

// A factory can refuse (signed out, no host in this build). That refusal has to
// reach the user as the turn's error, and nothing may be sent: C9 forbids
// falling back, and the default sender is the specific thing it forbids falling
// back TO.
func TestAFactoryRefusalBecomesTheTurnError(t *testing.T) {
	refused := errors.New("not signed in: the built-in gateway needs a session token")
	defaultSender := &countingSender{}
	srv := emptyProfileServer(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
		GatewaySender:      func(app.ReasoningTuning) (agent.Sender, error) { return nil, refused },
	})
	srv.sender = defaultSender

	sess := boundSession("buding-gateway::buding-privacy-1", "")
	sender, model := srv.senderForSession(sess)

	if sender == agent.Sender(defaultSender) {
		t.Fatal("a refused gateway turn fell back to the default sender (C9)")
	}
	_, err := sender.SendMessages(context.Background(), model, "", nil, 0)
	if err == nil {
		t.Fatal("want the factory's refusal as the turn error")
	}
	if !strings.Contains(err.Error(), "not signed in") {
		t.Errorf("error = %q, want the factory's reason preserved", err.Error())
	}
	if defaultSender.calls != 0 {
		t.Errorf("the default sender was called %d time(s); a refused gateway turn must not be sent anywhere", defaultSender.calls)
	}
}

// V-36's fail-closed half. With a factory wired, ensureSender stops requiring a
// default sender, so an ordinary (non-gateway) turn can now reach here with
// nothing to send it with. Upstream's answer was a startup error; the turn-path
// answer has to be an error too, and specifically NOT a nil dereference — an
// interface with a nil value panics at the call, which the user would see as a
// crashed turn rather than an explanation.
func TestAnUnservableTurnIsRefusedNotDereferenced(t *testing.T) {
	srv := withoutADefaultSender(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
		GatewaySender:      func(app.ReasoningTuning) (agent.Sender, error) { return &countingSender{}, nil },
	})
	if err := srv.ensureSender(); err != nil {
		t.Fatalf("ensureSender must not refuse a build that has a gateway: %v", err)
	}
	if srv.getSender() != nil {
		t.Fatal("this fixture is meant to have no default sender")
	}

	sess := boundSession("stub-model", "")
	sender, model := srv.senderForSession(sess)

	if sender == nil {
		t.Fatal("senderForSession returned nil; the agent loop would panic at the send")
	}
	_, err := sender.SendMessages(context.Background(), model, "", nil, 0)
	if err == nil {
		t.Fatal("want an error for a turn no provider can serve")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %q, want upstream's onboarding wording kept", err.Error())
	}
	// The model is still named, so the user can tell which model was refused.
	if model != "stub-model" {
		t.Errorf("model = %q, want the model the turn was for", model)
	}
}

// The counter-nail that protects `octo serve`, the CLI and every existing test:
// with no factory injected, ensureSender's behavior is upstream's verbatim.
// Without this, a change that made every build tolerate a missing provider would
// pass every other test in this file.
func TestWithoutAFactoryTheStartupErrorIsUnchanged(t *testing.T) {
	srv := withoutADefaultSender(t, Config{Addr: "127.0.0.1:0"}) // no GatewaySender

	err := srv.ensureSender()
	if err == nil {
		t.Fatal("a build with no gateway and no provider must still refuse at startup (upstream behavior)")
	}
	if !strings.Contains(err.Error(), "complete setup via the Web UI") {
		t.Errorf("error = %q, want upstream's message verbatim", err.Error())
	}
}
