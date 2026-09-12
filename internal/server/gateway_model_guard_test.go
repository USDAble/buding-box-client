package server

import (
	"context"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/config"
)

// The incident these nails guard (需求基线 V-35). A session bound to a model the
// picker read out of the signed catalog carries "buding-gateway::<model>" as its
// model NAME, because the composite id the picker sends does not exist in
// data/config.yml — handleUpdateSessionModel's last branch stores it as a raw
// model string. senderForSession's fallback then handed that string to the
// default sender, which is whatever config.yml points at, and the third-party
// endpoint answered:
//
//	HTTP 400 (invalid_request_error): The supported API model names are
//	deepseek-flash, deepseek-v4-pro, but you passed buding-gateway::buding-privacy-1
//
// Two things are wrong with that, and only the first is cosmetic: the user gets
// an incomprehensible error, and the conversation was transmitted to a provider
// that has no business seeing it. 需求基线 B4 forbids the second in every profile.
const testGatewayPrefix = "buding-gateway::"

// countingSender records how many turns actually reached the default sender.
// It is the difference between "we returned a nicer error" and "nothing left the
// device", and only the second is the guarantee B4 asks for — so the nails below
// assert the count, not just the error.
type countingSender struct{ calls int }

func (c *countingSender) SendMessages(_ context.Context, _, _ string, _ []agent.Message, _ int) (agent.Reply, error) {
	c.calls++
	return agent.Reply{Content: "stub reply"}, nil
}

// serverWithCountingSender builds a server whose default sender is the counting
// stub, so "the turn did not start" is observable.
func serverWithCountingSender(t *testing.T, cfg Config, seed config.Config) (*Server, *countingSender) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("OCTO_DATA_ROOT", tmp)
	t.Setenv("USERPROFILE", tmp)

	if err := seed.Save(); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	srv := mustServer(t, cfg)
	counted := &countingSender{}
	srv.sender = counted
	return srv, counted
}

// seedOrdinaryEndpoint is a config whose only endpoint is unrelated to the
// gateway — the state every non-product build is in.
func seedOrdinaryEndpoint() config.Config {
	return config.Config{
		Endpoints: []config.Endpoint{
			{ID: "ep-kimi", Provider: "kimi", APIKey: "sk-kimi", Models: []config.EndpointModel{{Model: "kimi-k2.6"}}},
		},
		Default: "ep-kimi::kimi-k2.6",
	}
}

// boundSession builds the session shape under test. agent.NewSession's second
// parameter is the SYSTEM PROMPT, not the model binding — a "bound" session
// built by passing the ref there is silently unbound, and every assertion about
// a resolved entry then passes for the wrong reason (it rides the default
// sender, which is what a wide assertion checks first). The binding is set
// explicitly so the test says what it means.
func boundSession(model, modelConfig string) *agent.Session {
	sess := agent.NewSession(model, "")
	sess.ModelConfig = modelConfig
	return sess
}

// The main nail. A gateway-bound model name must not reach the default sender,
// and the refusal must be a refusal — not a prettier error after the request
// already went out.
func TestAGatewayModelNeverRidesTheDefaultSender(t *testing.T) {
	srv, counted := serverWithCountingSender(t,
		Config{Addr: "127.0.0.1:0", GatewayModelPrefix: testGatewayPrefix},
		seedOrdinaryEndpoint())

	sess := boundSession("buding-gateway::buding-privacy-1", "")
	sender, model := srv.senderForSession(sess)

	if sender == srv.sender {
		t.Fatal("a gateway-bound model rode the default sender — the request would reach config.yml's third party (B4)")
	}
	if model != "buding-gateway::buding-privacy-1" {
		t.Errorf("model = %q, want the composite id verbatim so the refusal can name it", model)
	}
	if _, err := sender.SendMessages(context.Background(), model, "", nil, 0); err == nil {
		t.Error("SendMessages returned nil for a model no configured endpoint can serve")
	}
	if counted.calls != 0 {
		t.Errorf("the default sender was called %d time(s); nothing may leave the device for a model this build cannot serve", counted.calls)
	}
}

// The seam with PR-5, stated as a test: the moment a sender that speaks the
// gateway protocol exists — here an endpoint whose id IS the gateway, which
// EntryByModel resolves before the fallback is ever reached — the guard steps
// aside on its own. Nothing has to be remembered or unregistered.
func TestAnEndpointThatServesTheGatewayIsNotRefused(t *testing.T) {
	seed := config.Config{
		Endpoints: []config.Endpoint{
			{ID: "buding-gateway", Provider: "kimi", APIKey: "sk-kimi", Models: []config.EndpointModel{{Model: "buding-privacy-1"}}},
		},
		Default: "buding-gateway::buding-privacy-1",
	}
	srv, _ := serverWithCountingSender(t,
		Config{Addr: "127.0.0.1:0", GatewayModelPrefix: testGatewayPrefix}, seed)

	sess := boundSession("buding-privacy-1", "buding-gateway::buding-privacy-1")
	sender, model := srv.senderForSession(sess)

	if _, refused := sender.(refusedGatewaySender); refused {
		t.Fatal("the guard refused a model whose gateway endpoint is configured and resolvable (PR-5 would hit this)")
	}
	if sender == srv.sender {
		t.Error("the session must run on the resolved entry's sender, not the default")
	}
	if model != "buding-privacy-1" {
		t.Errorf("model = %q, want the bare model name from the resolved entry", model)
	}
}

// The counter-nail against over-reach: the guard keys on the gateway prefix,
// so every ordinary path must behave exactly as before, including the one that
// actually runs.
func TestOrdinaryModelsAreUnaffected(t *testing.T) {
	srv, counted := serverWithCountingSender(t,
		Config{Addr: "127.0.0.1:0", GatewayModelPrefix: testGatewayPrefix},
		seedOrdinaryEndpoint())

	// A session bound to a real config entry keeps resolving to that entry.
	bound := boundSession("kimi-k2.6", "ep-kimi::kimi-k2.6")
	if sender, _ := srv.senderForSession(bound); sender == srv.sender {
		t.Error("a normally bound session must keep riding its own entry's sender")
	}

	// A plain model name (the settings/CLI path) still uses the default sender
	// and still runs.
	plain := boundSession("stub-model", "")
	sender, model := srv.senderForSession(plain)
	if sender != srv.sender || model != "stub-model" {
		t.Fatalf("plain session = (%v, %q), want the default sender + stub-model", sender, model)
	}
	reply, err := sender.SendMessages(context.Background(), model, "", nil, 0)
	if err != nil {
		t.Fatalf("an ordinary model must still run: %v", err)
	}
	if reply.Content != "stub reply" || counted.calls != 1 {
		t.Errorf("reply = %q, calls = %d; the guard must not intercept ordinary turns", reply.Content, counted.calls)
	}
}

// octo serve, the CLI and every upstream entry point leave the field empty
// (server-diff-guard: this package's upstream behavior is the default). Their
// resolution must therefore stay exactly upstream's, degradation included.
func TestTheGuardIsInertWithoutAnInjectedPrefix(t *testing.T) {
	srv, counted := serverWithCountingSender(t,
		Config{Addr: "127.0.0.1:0"}, // no GatewayModelPrefix — not a product build
		seedOrdinaryEndpoint())

	sess := boundSession("buding-gateway::buding-privacy-1", "")
	sender, model := srv.senderForSession(sess)

	if sender != srv.sender {
		t.Fatal("with no injected prefix the fallback must stay upstream's (degrade to the default sender)")
	}
	if _, err := sender.SendMessages(context.Background(), model, "", nil, 0); err != nil {
		t.Fatalf("upstream behavior must be unchanged: %v", err)
	}
	if counted.calls != 1 {
		t.Errorf("calls = %d, want 1 — the unguarded path must still run", counted.calls)
	}
}

// The guard sits on the fallback as a whole, not on one branch of it. This is
// the second branch: ModelConfig names the gateway, but the entry is gone
// (deleted since binding), so resolution fails a different way and must refuse
// just the same.
func TestAStaleGatewayBindingIsAlsoRefused(t *testing.T) {
	srv, counted := serverWithCountingSender(t,
		Config{Addr: "127.0.0.1:0", GatewayModelPrefix: testGatewayPrefix},
		seedOrdinaryEndpoint())

	sess := boundSession("buding-privacy-1", "buding-gateway::buding-privacy-1")
	sender, model := srv.senderForSession(sess)

	if sender == srv.sender {
		t.Fatal("a stale gateway binding fell back to the default sender")
	}
	if _, err := sender.SendMessages(context.Background(), model, "", nil, 0); err == nil {
		t.Error("SendMessages returned nil for a gateway-bound model with no resolvable endpoint")
	}
	if counted.calls != 0 {
		t.Errorf("the default sender was called %d time(s), want 0", counted.calls)
	}
}

// The counter-nail to the nail above: a stale binding to an ORDINARY endpoint
// keeps upstream's degrade-to-default behavior. Without this, a guard that
// refused every failed resolution would pass every other test in this file.
func TestAStaleOrdinaryBindingStillDegrades(t *testing.T) {
	srv, counted := serverWithCountingSender(t,
		Config{Addr: "127.0.0.1:0", GatewayModelPrefix: testGatewayPrefix},
		seedOrdinaryEndpoint())

	sess := boundSession("kimi-k2.6", "ep-deleted::kimi-k2.6")
	sender, model := srv.senderForSession(sess)

	if sender != srv.sender {
		t.Fatal("an ordinary stale binding must keep degrading to the default sender (upstream behavior)")
	}
	if _, err := sender.SendMessages(context.Background(), model, "", nil, 0); err != nil {
		t.Fatalf("an ordinary stale binding must still run: %v", err)
	}
	if counted.calls != 1 {
		t.Errorf("calls = %d, want 1", counted.calls)
	}
}

// A refusal the user cannot act on is only marginally better than the 400 it
// replaced. The message must name the model, say who serves it, and say that
// nothing was sent — that last part is what tells the user their prompt was not
// transmitted anywhere.
func TestTheRefusalExplainsItself(t *testing.T) {
	srv, _ := serverWithCountingSender(t,
		Config{Addr: "127.0.0.1:0", GatewayModelPrefix: testGatewayPrefix},
		seedOrdinaryEndpoint())

	sess := boundSession("buding-gateway::buding-privacy-1", "")
	sender, model := srv.senderForSession(sess)

	_, err := sender.SendMessages(context.Background(), model, "", nil, 0)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"buding-gateway::buding-privacy-1", "gateway", "nothing was sent"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q must mention %q", err.Error(), want)
		}
	}
}
