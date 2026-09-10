//go:build !product_production

// The mock package is excluded from production builds, so the tests that use it
// carry the same constraint. `make test-production` therefore skips these; the
// untagged `deps_test.go` still runs and still enforces the dependency rules.
package productruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/gateway"
	gatewaymock "github.com/open-octo/octo-agent/internal/productclient/gateway/mock"
	clientmock "github.com/open-octo/octo-agent/internal/productclient/mock"
	"github.com/open-octo/octo-agent/internal/productpolicy"
)

const (
	testAudience      = "puddingbox"
	testClientVersion = "1.2.0"
)

var now = clientmock.FixedNow

// testStore is a minimal credentialstore.Store. A memory store is enough here:
// P0-08 owns the OS-backed implementation and its own tests.
type testStore struct{ tokens credentialstore.Tokens }

func (s *testStore) Load(context.Context) (credentialstore.Tokens, error) {
	if s.tokens.RefreshToken == "" {
		return credentialstore.Tokens{}, credentialstore.ErrNotFound
	}
	return s.tokens, nil
}
func (s *testStore) Save(_ context.Context, t credentialstore.Tokens) error { s.tokens = t; return nil }
func (s *testStore) Clear(context.Context) error                            { s.tokens = credentialstore.Tokens{}; return nil }

func fullDeps() Deps {
	return Deps{
		Auth:         &clientmock.Auth{},
		ControlPlane: &clientmock.ControlPlane{},
		Usage:        &clientmock.Usage{},
		Sessions:     &clientmock.Sessions{},
		Gateway:      gatewaymock.NewControl(),
		Observer:     &gatewaymock.Recorder{},
		Credentials:  &testStore{},
	}
}

// policyFromEnvelope converts a bootstrap/catalog envelope into the Verified
// form a Verifier would produce, so these tests exercise the mock fixtures
// rather than a hand-built parallel shape.
func policyFromEnvelope(t *testing.T, e productclient.PolicyEnvelope) *productpolicy.Policy {
	t.Helper()
	issued, err := time.Parse(time.RFC3339, e.IssuedAt)
	if err != nil {
		t.Fatalf("fixture issuedAt: %v", err)
	}
	expires, err := time.Parse(time.RFC3339, e.ExpiresAt)
	if err != nil {
		t.Fatalf("fixture expiresAt: %v", err)
	}
	p, err := productpolicy.NewPolicy(productpolicy.Signed{
		PolicyVersion:    e.PolicyVersion,
		IssuedAt:         issued,
		ExpiresAt:        expires,
		Audience:         e.Audience,
		KeyID:            e.KeyID,
		MinClientVersion: e.MinClientVersion,
		Catalog:          e.Catalog,
		Capabilities:     e.Capabilities,
	}, testAudience, testClientVersion)
	if err != nil {
		t.Fatalf("NewPolicy from fixture: %v", err)
	}
	return p
}

func newRuntime(t *testing.T) *Runtime {
	t.Helper()
	rt, err := New(fullDeps())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rt
}

// TestNewListsEveryMissingDependency: failing one field at a time turns a
// first-run wiring mistake into several round trips.
func TestNewListsEveryMissingDependency(t *testing.T) {
	_, err := New(Deps{})

	var missing *ErrMissingDeps
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want *ErrMissingDeps", err)
	}
	for _, field := range []string{"Auth", "ControlPlane", "Usage", "Sessions", "Gateway", "Observer", "Credentials"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error %q does not name the missing %s", err.Error(), field)
		}
	}
}

// TestPreflightWithoutPolicyIsBootstrapStale: starting with no policy is the
// correct cold-start state, and it must read as "we have not talked to the
// platform yet", never as a default policy.
func TestPreflightWithoutPolicyIsBootstrapStale(t *testing.T) {
	rt := newRuntime(t)

	err := rt.Preflight(clientmock.ModelID, now)
	if !productclient.IsCode(err, productclient.CodeBootstrapStale) {
		t.Fatalf("err = %v, want bootstrap_stale", err)
	}
	if v := rt.Evaluate(productpolicy.Request{Capability: "allow_cap"}, now); v.Decision != productpolicy.Deny {
		t.Errorf("Evaluate with no policy = %q, want deny", v.Decision)
	}
}

func TestPreflightAcceptsAHealthyTurn(t *testing.T) {
	rt := newRuntime(t)
	if err := rt.SetPolicy(policyFromEnvelope(t, clientmock.Bootstrap(now).Policy)); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}

	if err := rt.Preflight(clientmock.ModelID, now); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if got := rt.PolicyVersion(); got != "2026-09-10.1" {
		t.Errorf("PolicyVersion() = %q", got)
	}
}

// TestPreflightRejectsExpiredPolicy is the R4 core assertion's client half: an
// expired policy must not be able to open a new turn.
func TestPreflightRejectsExpiredPolicy(t *testing.T) {
	rt := newRuntime(t)
	if err := rt.SetPolicy(policyFromEnvelope(t, clientmock.ExpiredBootstrap(now).Policy)); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}

	if err := rt.Preflight(clientmock.ModelID, now); !productclient.IsCode(err, productclient.CodePolicyExpired) {
		t.Fatalf("err = %v, want policy_expired", err)
	}
	// Even a valid unexpired envelope must be refused once the clock has
	// moved past its window.
	rt2 := newRuntime(t)
	if err := rt2.SetPolicy(policyFromEnvelope(t, clientmock.Bootstrap(now).Policy)); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}
	if err := rt2.Preflight(clientmock.ModelID, now.Add(3*time.Hour)); !productclient.IsCode(err, productclient.CodePolicyExpired) {
		t.Errorf("err = %v, want policy_expired after the window closes", err)
	}
}

func TestPreflightRejectsUnknownModel(t *testing.T) {
	rt := newRuntime(t)
	if err := rt.SetPolicy(policyFromEnvelope(t, clientmock.Bootstrap(now).Policy)); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}

	if err := rt.Preflight("some-other-model", now); !productclient.IsCode(err, productclient.CodeModelNotFound) {
		t.Errorf("err = %v, want model_not_found", err)
	}
	if err := rt.Preflight("", now); !productclient.IsCode(err, productclient.CodeModelNotFound) {
		t.Errorf("empty model = %v, want model_not_found", err)
	}
}

// TestPreflightRejectsIneligibleOrNonGatewayModel: a catalog entry the platform
// has not marked usable must not become usable by being present.
func TestPreflightRejectsIneligibleOrNonGatewayModel(t *testing.T) {
	for name, mutate := range map[string]func(*productclient.Catalog){
		"ineligible":  func(c *productclient.Catalog) { c.Models[0].Eligible = false },
		"not gateway": func(c *productclient.Catalog) { c.Models[0].Transport = "direct" },
	} {
		t.Run(name, func(t *testing.T) {
			env := clientmock.Bootstrap(now).Policy
			mutate(&env.Catalog)

			rt := newRuntime(t)
			if err := rt.SetPolicy(policyFromEnvelope(t, env)); err != nil {
				t.Fatalf("SetPolicy: %v", err)
			}
			if err := rt.Preflight(clientmock.ModelID, now); !productclient.IsCode(err, productclient.CodeModelNotAllowed) {
				t.Errorf("err = %v, want model_not_allowed", err)
			}
		})
	}
}

// TestSetPolicyEnforcesMonotonicity: a correctly signed but older envelope must
// not be able to re-grant a withdrawn capability.
func TestSetPolicyEnforcesMonotonicity(t *testing.T) {
	rt := newRuntime(t)

	newer := clientmock.Bootstrap(now).Policy
	newer.PolicyVersion = "2026-09-10.2"
	if err := rt.SetPolicy(policyFromEnvelope(t, newer)); err != nil {
		t.Fatalf("SetPolicy(newer): %v", err)
	}

	older := clientmock.Bootstrap(now).Policy
	older.PolicyVersion = "2026-09-10.1"
	if err := rt.SetPolicy(policyFromEnvelope(t, older)); !errors.Is(err, productpolicy.ErrVersionRollback) {
		t.Fatalf("err = %v, want ErrVersionRollback", err)
	}
	if got := rt.PolicyVersion(); got != "2026-09-10.2" {
		t.Errorf("PolicyVersion() = %q; a rejected rollback must not replace the policy in force", got)
	}

	if err := rt.SetPolicy(nil); err == nil {
		t.Error("SetPolicy(nil) must be rejected")
	}
}

// TestObserveRefusesAnUnidentifiableCall is the cheapest possible place to stop
// a call the platform could not deduplicate.
func TestObserveRefusesAnUnidentifiableCall(t *testing.T) {
	rec := &gatewaymock.Recorder{}
	deps := fullDeps()
	deps.Observer = rec
	rt, err := New(deps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sink := &gatewaymock.Sink{}
	base := &stubSender{}

	_, err = rt.Observe(base, gateway.CallMeta{ModelID: clientmock.ModelID}, sink)
	if !productclient.IsCode(err, productclient.CodeInvalidRequest) {
		t.Errorf("err = %v, want invalid_request for an empty clientRequestId", err)
	}
	if len(rec.Metas()) != 0 {
		t.Error("a rejected call must not reach the observer")
	}

	_, err = rt.Observe(base, gateway.CallMeta{ClientRequestID: "cr_1"}, sink)
	if !productclient.IsCode(err, productclient.CodeInvalidRequest) {
		t.Errorf("err = %v, want invalid_request for an empty modelId", err)
	}
	if _, err := rt.Observe(base, gateway.CallMeta{ClientRequestID: "cr_1", ModelID: clientmock.ModelID}, nil); err == nil {
		t.Error("Observe with a nil sink must be rejected")
	}
	if _, err := rt.Observe(nil, gateway.CallMeta{ClientRequestID: "cr_1", ModelID: "m"}, sink); err == nil {
		t.Error("Observe(nil sender) must be rejected")
	}
	if len(rec.Metas()) != 0 {
		t.Error("no rejected call should reach the observer")
	}

	got, err := rt.Observe(base, gateway.CallMeta{ClientRequestID: "cr_1", ModelID: clientmock.ModelID}, sink)
	if err != nil {
		t.Fatalf("a well-formed call should pass: %v", err)
	}
	// The Recorder observer returns its base unchanged; the real observer must
	// preserve capabilities (see gateway.Observer), so this pins the contract's
	// loosest legal implementation rather than a regression.
	if got != agent.Sender(base) {
		t.Error("the Recorder observer returns its base unchanged")
	}
	if metas := rec.Metas(); len(metas) != 1 || metas[0].ClientRequestID != "cr_1" {
		t.Errorf("observer saw %+v, want exactly one cr_1 call", metas)
	}
	if rec.Sink() != gateway.TerminalSink(sink) {
		t.Error("the sink must be handed through to the observer unchanged")
	}
}

// TestCancelAndRequestStatusRefuseEmptyIDs: an empty id cannot be looked up, and
// sending it would create a request the platform can never correlate.
func TestCancelAndRequestStatusRefuseEmptyIDs(t *testing.T) {
	rt := newRuntime(t)

	if err := rt.Cancel(context.Background(), "  "); !productclient.IsCode(err, productclient.CodeInvalidRequest) {
		t.Errorf("Cancel with a blank id = %v, want invalid_request", err)
	}
	if _, err := rt.RequestStatus(context.Background(), ""); !productclient.IsCode(err, productclient.CodeInvalidRequest) {
		t.Errorf("RequestStatus with a blank id = %v, want invalid_request", err)
	}
}

// TestBrokenStreamRecoveryAsksThePlatform walks the one recovery path P0-04
// allows after a stream breaks.
func TestBrokenStreamRecoveryAsksThePlatform(t *testing.T) {
	ctrl := gatewaymock.NewControl()
	ctrl.SetStatus("cr_1", productclient.RequestStatus{
		ClientRequestID: "cr_1",
		RequestID:       "gw_123",
		State:           productclient.StateReconciliationPending,
	})

	deps := fullDeps()
	deps.Gateway = ctrl
	rt, err := New(deps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := rt.RequestStatus(context.Background(), "cr_1")
	if err != nil {
		t.Fatalf("RequestStatus: %v", err)
	}
	if got.State.Final() {
		t.Error("reconciliation_pending must not read as final")
	}

	if err := rt.Cancel(context.Background(), "cr_1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if c := ctrl.Cancelled(); len(c) != 1 || c[0] != "cr_1" {
		t.Errorf("Cancelled() = %v, want [cr_1]", c)
	}

	// An id the platform never saw must surface as an error, not a zero state.
	if _, err := rt.RequestStatus(context.Background(), "never_sent"); err == nil {
		t.Error("an unknown clientRequestId must not report a successful zero state")
	}
}

// TestEvaluateUsesThePolicyInForce ties the composition root to the PEP.
func TestEvaluateUsesThePolicyInForce(t *testing.T) {
	rt := newRuntime(t)
	if err := rt.SetPolicy(policyFromEnvelope(t, clientmock.Bootstrap(now).Policy)); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}

	cases := []struct {
		capability string
		want       productpolicy.Decision
		why        string
	}{
		{"terminal", productpolicy.Ask, "entitled with permissionPolicy=ask"},
		{"local_read", productpolicy.Allow, "entitled with permissionPolicy=allow"},
		{"install_extension", productpolicy.Deny, "permissionPolicy=deny"},
		{"mcp", productpolicy.Deny, "ask but not entitled: entitlement is checked first"},
		{"absent_feature", productpolicy.Deny, "not available in this build"},
		{"made_up", productpolicy.Deny, "not in the matrix at all"},
	}
	for _, tc := range cases {
		got := rt.Evaluate(productpolicy.Request{Capability: tc.capability}, now)
		if got.Decision != tc.want {
			t.Errorf("Evaluate(%s) = %q, want %q (%s)", tc.capability, got.Decision, tc.want, tc.why)
		}
	}
}

// stubSender is the narrowest possible agent.Sender, enough to pass the nil
// check in Observe.
type stubSender struct{}

func (s *stubSender) SendMessages(context.Context, string, string, []agent.Message, int) (agent.Reply, error) {
	return agent.Reply{}, nil
}
