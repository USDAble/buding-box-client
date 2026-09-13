package server

// PR-5c: a build whose model traffic must come from the control plane must not
// fall back to a third party when the control plane (or the gateway) cannot
// serve the turn. 需求基线 C9 规则 2, closed loop L-C5.
//
// Why the assertion is "the third-party stand-in was hit 0 times" rather than
// "the error message says the right thing": V-35 is the incident where the
// message and the request disagreed. The user saw a third-party 400 while the
// conversation had already been transmitted elsewhere. A nail that reads the
// error text would have passed that day.
//
// The stand-in is a real HTTP endpoint rather than a counting stub, so "nothing
// left the device" is a claim about the wire. It plays both roles the incident
// needs — the source a misconfigured build would reach, and the gateway that is
// supposed to fail instead — which is why one type serves every face below.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/config"
)

const (
	thirdPartyEndpointID = "ep-third-party"
	thirdPartyModel      = "third-party-model"
)

// thirdPartyReply is a minimal valid completion, so a sender pointed at the
// stand-in completes rather than erroring. A stand-in that fails would make
// every zero-hit assertion vacuous: "the request did not arrive" would be
// indistinguishable from "the request could not have worked anyway".
const thirdPartyReply = `{"choices":[{"message":{"role":"assistant","content":"answered somewhere else"},` +
	`"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`

// countingStandIn is a real HTTP endpoint that records how many requests
// reached it. status is swappable so the same type serves as a healthy
// third-party service and as a gateway that is failing.
type countingStandIn struct {
	mu     sync.Mutex
	hits   int
	status int
	url    string
}

func newCountingStandIn(t *testing.T, status int) *countingStandIn {
	t.Helper()
	stand := &countingStandIn{status: status}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		stand.mu.Lock()
		stand.hits++
		status := stand.status
		stand.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(thirdPartyReply))
	}))
	t.Cleanup(srv.Close)
	stand.url = srv.URL
	return stand
}

func (c *countingStandIn) hitCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

// thirdPartySeed is the endpoint data/config.yml would hold if this build had
// once been pointed at a working third-party service (or if a developer left
// one behind). inlineKey empty means the key has to come from the environment,
// which is the second of the two sources C9 规则 2 names.
func thirdPartySeed(url, inlineKey string) config.Config {
	return config.Config{
		Endpoints: []config.Endpoint{{
			ID:       thirdPartyEndpointID,
			Provider: app.ProviderCustom,
			BaseURL:  url,
			Protocol: "openai",
			APIKey:   inlineKey,
			Models:   []config.EndpointModel{{Model: thirdPartyModel}},
		}},
		Default: thirdPartyEndpointID + "::" + thirdPartyModel,
	}
}

// defaultSenderFromEntry builds the sender ensureSender would have built from
// that seed and fails the test if it is not usable. The failure is the point:
// without it a zero-hit assertion would pass on a fixture whose third party was
// unreachable, which is exactly the vacuous green this file exists to avoid.
func defaultSenderFromEntry(t *testing.T, seed config.Config) agent.Sender {
	t.Helper()
	entry, ok := seed.EntryByModel(seed.Default)
	if !ok {
		t.Fatalf("fixture: %q does not resolve to an entry", seed.Default)
	}
	sender, err := senderForEntry(entry)
	if err != nil {
		t.Fatalf("fixture: the third-party entry has no usable sender (%v); a zero-hit assertion would be vacuous", err)
	}
	return sender
}

// productionBuild is the state this PR is about: the control plane is the only
// permitted model source, and it is configured. Only the two booleans differ
// between the faces below, so the two named constructors are what make each
// test's premise readable.
func productionBuild(t *testing.T, cfg Config) *Server {
	t.Helper()
	cfg.RequireGateway = true
	cfg.ControlPlaneReady = true
	return policyBuild(t, cfg)
}

// buildWithoutAService is productionBuild in a release that still carries its
// `.invalid` placeholders: the control plane is required and absent at once.
func buildWithoutAService(t *testing.T, cfg Config) *Server {
	t.Helper()
	cfg.RequireGateway = true
	cfg.ControlPlaneReady = false
	return policyBuild(t, cfg)
}

func policyBuild(t *testing.T, cfg Config) *Server {
	t.Helper()
	cfg.Addr = "127.0.0.1:0"
	if cfg.GatewayModelPrefix == "" {
		cfg.GatewayModelPrefix = testGatewayPrefix
	}
	// No key for the custom vendor may leak in from the developer's shell: face
	// 6 sets this explicitly, and every other face has to see it empty for its
	// "the third party was configured" premise to be the fixture's doing.
	t.Setenv(app.VendorAPIKeyEnvVar(app.ProviderCustom), "")
	return emptyProfileServer(t, cfg)
}

// oneTurn is a turn with a message in it. An empty slice is refused by the
// provider before it dials anything, so a test that passed nil would read
// "nothing was sent" as a pass while proving only that no request was built.
func oneTurn() []agent.Message {
	return []agent.Message{{Role: "user", Content: "hi"}}
}

func gatewayBoundSession() *agent.Session {
	return boundSession("buding-gateway::buding-privacy-1", "")
}

// ---------------------------------------------------------------------------
// The premise: the zero-hit assertions below are only meaningful because the
// third-party sender really can complete a turn when nothing stops it.
// ---------------------------------------------------------------------------

func TestTheThirdPartyStandInIsAWorkingModelSource(t *testing.T) {
	stand := newCountingStandIn(t, http.StatusOK)
	sender := defaultSenderFromEntry(t, thirdPartySeed(stand.url, "sk-third-party"))

	reply, err := sender.SendMessages(context.Background(), thirdPartyModel, "",
		[]agent.Message{{Role: "user", Content: "hi"}}, 0)
	if err != nil {
		t.Fatalf("the third-party stand-in did not answer: %v", err)
	}
	if !strings.Contains(reply.Content, "answered somewhere else") {
		t.Errorf("reply = %q, want the stand-in's content", reply.Content)
	}
	if stand.hitCount() != 1 {
		t.Errorf("hit count = %d, want 1 — the stand-in is not reachable, so every zero-hit assertion in this file would pass vacuously", stand.hitCount())
	}
}

// ---------------------------------------------------------------------------
// L-C5, the six faces. Each ends in "the third-party stand-in was hit 0 times".
// ---------------------------------------------------------------------------

// Face 1 — the catalog is unavailable (or the picker left the session with no
// model from it). The gateway may serve the turn or refuse it; what may not
// happen is the default sender picking it up.
func TestASessionWithNoCatalogModelIsNotServedElsewhere(t *testing.T) {
	stand := newCountingStandIn(t, http.StatusOK)
	srv := productionBuild(t, Config{
		GatewaySender: func(app.ReasoningTuning) (agent.Sender, error) { return &countingSender{}, nil },
	})
	srv.sender = defaultSenderFromEntry(t, thirdPartySeed(stand.url, "sk-third-party"))

	// An empty model is what a session holds when the list came back empty and
	// nothing was ever picked.
	sess := boundSession("", "")
	sender, model := srv.senderForSession(sess)

	if _, err := sender.SendMessages(context.Background(), model, "", oneTurn(), 0); err == nil {
		t.Error("a session with no catalog model was served without an error")
	}
	if got := stand.hitCount(); got != 0 {
		t.Errorf("the third party was hit %d time(s) for a session that has no catalog model; the conversation left the device (C9 规则 2)", got)
	}
}

// Faces 2 and 3 — the gateway itself fails (5xx, and 401 after a refused
// refresh). C9 forbids falling back, so the turn must fail where it stands.
func TestAGatewayThatFailsDoesNotFallBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"upstream 5xx", http.StatusServiceUnavailable},
		{"401 after a refused refresh", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stand := newCountingStandIn(t, http.StatusOK)
			gateway := newCountingStandIn(t, tc.status)

			srv := productionBuild(t, Config{
				GatewaySender: func(app.ReasoningTuning) (agent.Sender, error) {
					return app.NewSender(app.SenderOptions{
						Provider: app.ProviderCustom, Protocol: "openai",
						APIKey: "session-token", BaseURL: gateway.url,
					})
				},
			})
			srv.sender = defaultSenderFromEntry(t, thirdPartySeed(stand.url, "sk-third-party"))

			sender, model := srv.senderForSession(gatewayBoundSession())
			if _, err := sender.SendMessages(context.Background(), model, "", oneTurn(), 0); err == nil {
				t.Error("a gateway failure was swallowed; the turn reported success")
			}
			if gateway.hitCount() == 0 {
				t.Error("the gateway was never asked, so this fixture is not exercising the failure it names")
			}
			if got := stand.hitCount(); got != 0 {
				t.Errorf("the gateway failed and the turn fell back: the third party was hit %d time(s) (C9 规则 2)", got)
			}
		})
	}
}

// Face 4 — the profile names no control plane. This must be refused before
// anything is looked up, because the profile's hosts are `.invalid` placeholders
// and dialing one produces a DNS error the user cannot act on.
func TestAnUnconfiguredControlPlaneIsRefusedAndNeverDialed(t *testing.T) {
	stand := newCountingStandIn(t, http.StatusOK)
	asked := 0
	srv := buildWithoutAService(t, Config{
		GatewaySender: func(app.ReasoningTuning) (agent.Sender, error) {
			asked++
			return &countingSender{}, nil
		},
	})
	srv.sender = defaultSenderFromEntry(t, thirdPartySeed(stand.url, "sk-third-party"))

	// Even a gateway-bound model is refused: with no control plane there is no
	// gateway host to reach, and the gateway factory must not be asked either.
	sender, model := srv.senderForSession(gatewayBoundSession())
	_, err := sender.SendMessages(context.Background(), model, "", oneTurn(), 0)
	if err == nil {
		t.Fatal("an unconfigured build served a turn instead of refusing it")
	}
	if asked != 0 {
		t.Errorf("the gateway factory was asked %d time(s) in a build with no control plane; the refusal must come before any lookup", asked)
	}
	if got := stand.hitCount(); got != 0 {
		t.Errorf("the third party was hit %d time(s) in a build with no control plane", got)
	}
	// The message has to describe the build's state, not a host: a DNS failure
	// shape would tell the user to check their network, which is wrong.
	for _, unwanted := range []string{"lookup", "no such host", "dial tcp", "invalid"} {
		if strings.Contains(strings.ToLower(err.Error()), unwanted) {
			t.Errorf("refusal %q reads like a network failure (%q); the user must be told the build has no service", err.Error(), unwanted)
		}
	}
}

// Face 5 — data/config.yml holds a usable third-party endpoint. Before PR-5c a
// product build sent the conversation there (this is V-35's other half).
func TestAUsableConfigYMLIsNotAModelSourceInAProductBuild(t *testing.T) {
	stand := newCountingStandIn(t, http.StatusOK)
	srv := productionBuild(t, Config{
		GatewaySender: func(app.ReasoningTuning) (agent.Sender, error) { return nil, errors.New("no gateway in this fixture") },
	})
	srv.sender = defaultSenderFromEntry(t, thirdPartySeed(stand.url, "sk-third-party"))

	// A bare catalog id is what the picker stores when nothing is bound, and it
	// is not gateway-prefixed — the V-35 shape from the other direction.
	sender, model := srv.senderForSession(boundSession(thirdPartyModel, ""))
	_, err := sender.SendMessages(context.Background(), model, "", oneTurn(), 0)
	if err == nil {
		t.Error("config.yml's endpoint served the turn in a build that requires the control plane")
	}
	if got := stand.hitCount(); got != 0 {
		t.Errorf("the endpoint config.yml names was hit %d time(s) (C9 规则 2: the model source must come from the catalog)", got)
	}
}

// Face 6 — the same, with the key coming from the environment instead of the
// file. Two sources, one rule: neither is a model source in a product build.
func TestAnEnvironmentKeyIsNotAModelSourceInAProductBuild(t *testing.T) {
	stand := newCountingStandIn(t, http.StatusOK)
	t.Setenv(app.VendorAPIKeyEnvVar(app.ProviderCustom), "sk-from-the-environment")

	seed := thirdPartySeed(stand.url, "")
	if seed.Endpoints[0].APIKey != "" {
		t.Fatal("fixture: this face is about the environment, so the file must carry no key")
	}
	srv := productionBuild(t, Config{
		GatewaySender: func(app.ReasoningTuning) (agent.Sender, error) { return nil, errors.New("no gateway in this fixture") },
	})
	srv.sender = defaultSenderFromEntry(t, seed)

	sender, model := srv.senderForSession(boundSession(thirdPartyModel, ""))
	_, err := sender.SendMessages(context.Background(), model, "", oneTurn(), 0)
	if err == nil {
		t.Error("an environment-variable key served the turn in a build that requires the control plane")
	}
	if got := stand.hitCount(); got != 0 {
		t.Errorf("the vendor named by an environment variable was hit %d time(s)", got)
	}
}

// ---------------------------------------------------------------------------
// Counter-nails: the rule is scoped to builds that require the control plane,
// and to nothing else.
// ---------------------------------------------------------------------------

// A developer build sources models from config.yml on purpose — that is what
// allowEnvironmentModelSource means. A rule that stopped it would be a bug
// dressed as safety.
func TestADeveloperBuildStillUsesConfigYML(t *testing.T) {
	stand := newCountingStandIn(t, http.StatusOK)
	srv := emptyProfileServer(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
		RequireGateway:     false,
		ControlPlaneReady:  false,
	})
	srv.sender = defaultSenderFromEntry(t, thirdPartySeed(stand.url, "sk-third-party"))

	sender, model := srv.senderForSession(boundSession(thirdPartyModel, ""))
	reply, err := sender.SendMessages(context.Background(), model, "", oneTurn(), 0)
	if err != nil {
		t.Fatalf("a developer build must still use its configured endpoint: %v", err)
	}
	if !strings.Contains(reply.Content, "answered somewhere else") {
		t.Errorf("reply = %q, want the configured endpoint's content", reply.Content)
	}
	if got := stand.hitCount(); got != 1 {
		t.Errorf("hit count = %d, want 1 — the rule must not reach developer builds", got)
	}
}

// ControlPlaneReady is only consulted by a build that requires the control
// plane. A developer build with no control plane configured must be unaffected,
// or every developer without a Sandbox address loses local models.
func TestAnUnconfiguredControlPlaneDoesNotObstructADeveloperBuild(t *testing.T) {
	stand := newCountingStandIn(t, http.StatusOK)
	srv := emptyProfileServer(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
		RequireGateway:     false,
		ControlPlaneReady:  false,
	})
	srv.sender = defaultSenderFromEntry(t, thirdPartySeed(stand.url, "sk-third-party"))

	sender, model := srv.senderForSession(boundSession(thirdPartyModel, ""))
	if _, err := sender.SendMessages(context.Background(), model, "", oneTurn(), 0); err != nil {
		t.Fatalf("a developer build with no control plane must still run: %v", err)
	}
	if got := stand.hitCount(); got != 1 {
		t.Errorf("hit count = %d, want 1", got)
	}
}

// octo serve, the CLI and every upstream entry point leave both fields at their
// zero value, so their resolution has to stay exactly upstream's.
func TestWithoutTheNewFieldsResolutionIsUnchanged(t *testing.T) {
	stand := newCountingStandIn(t, http.StatusOK)
	srv := emptyProfileServer(t, Config{Addr: "127.0.0.1:0"}) // no gateway, no policy fields
	srv.sender = defaultSenderFromEntry(t, thirdPartySeed(stand.url, "sk-third-party"))

	// Upstream degrades to the default sender when nothing resolves.
	sender, model := srv.senderForSession(boundSession("kimi-k2.6", "ep-deleted::kimi-k2.6"))
	if sender != srv.sender {
		t.Fatal("with no policy fields the fallback must stay upstream's")
	}
	if _, err := sender.SendMessages(context.Background(), model, "", oneTurn(), 0); err != nil {
		t.Fatalf("upstream behavior must be unchanged: %v", err)
	}
	if got := stand.hitCount(); got != 1 {
		t.Errorf("hit count = %d, want 1", got)
	}
}

// And ensureSender keeps upstream's onboarding refusal when no policy is
// injected: the conditional added for V-36 is keyed on an injected gateway
// factory, not on this PR's fields.
func TestEnsureSenderKeepsItsUpstreamRefusalWithoutInjection(t *testing.T) {
	srv := withoutADefaultSender(t, Config{Addr: "127.0.0.1:0"})
	err := srv.ensureSender()
	if err == nil {
		t.Fatal("a build with no gateway and no provider must still refuse at startup")
	}
	if !strings.Contains(err.Error(), "complete setup via the Web UI") {
		t.Errorf("error = %q, want upstream's wording", err.Error())
	}
}
