package productpolicy

import (
	"errors"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
)

const testAudience = "puddingbox"

// base is the reference instant. Everything below is expressed relative to it so
// the tests are deterministic and do not depend on the wall clock.
var base = time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)

func signedFixture(at time.Time) Signed {
	return Signed{
		PolicyVersion:    "2026-09-10.1",
		IssuedAt:         at.Add(-time.Hour),
		ExpiresAt:        at.Add(time.Hour),
		Audience:         testAudience,
		KeyID:            "policy-2026-a",
		MinClientVersion: "1.0.0",
		Catalog: productclient.Catalog{
			Version: "2026-09-10.1",
			Models: []productclient.Model{{
				ID:        "buding-cloud-pro",
				Transport: productclient.TransportGateway,
				Eligible:  true,
			}},
		},
		Capabilities: []productclient.Capability{
			{ID: "ask_cap", Available: true, Entitled: true, PermissionPolicy: productclient.PermissionAsk},
			{ID: "allow_cap", Available: true, Entitled: true, PermissionPolicy: productclient.PermissionAllow},
			{ID: "deny_cap", Available: true, Entitled: true, PermissionPolicy: productclient.PermissionDeny},
			{ID: "unentitled_cap", Available: true, Entitled: false, PermissionPolicy: productclient.PermissionAllow},
			{ID: "absent_cap", Available: false, Entitled: true, PermissionPolicy: productclient.PermissionAllow},
		},
	}
}

func mustPolicy(t *testing.T, s Signed, clientVersion string) *Policy {
	t.Helper()
	p, err := NewPolicy(s, testAudience, clientVersion)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return p
}

func TestNewPolicyRejectsAudienceMismatch(t *testing.T) {
	s := signedFixture(base)
	s.Audience = "some-other-product"

	_, err := NewPolicy(s, testAudience, "1.2.0")
	if !errors.Is(err, ErrAudienceMismatch) {
		t.Fatalf("err = %v, want ErrAudienceMismatch", err)
	}
}

// TestNewPolicyRejectsOldClient: a client below minClientVersion must not be
// able to interpret the policy at all, so the gate is at construction rather
// than at each decision.
func TestNewPolicyRejectsOldClient(t *testing.T) {
	s := signedFixture(base)
	s.MinClientVersion = "1.2.0"

	if _, err := NewPolicy(s, testAudience, "1.1.9"); !errors.Is(err, ErrClientTooOld) {
		t.Fatalf("err = %v, want ErrClientTooOld", err)
	}
	if _, err := NewPolicy(s, testAudience, "1.2"); err != nil {
		t.Fatalf("1.2 should satisfy 1.2.0 (missing components are zero): %v", err)
	}
	if _, err := NewPolicy(s, testAudience, "1.2.1"); err != nil {
		t.Fatalf("1.2.1 should satisfy 1.2.0: %v", err)
	}
}

// TestNewPolicyFailsClosedOnUnparseableVersion: "we could not tell" must not
// become "yes".
func TestNewPolicyFailsClosedOnUnparseableVersion(t *testing.T) {
	s := signedFixture(base)
	s.MinClientVersion = "1.2.0"

	for _, v := range []string{"", "1.x.0", "v1.2.0", "1.-2.0", "1..2", "-1.0.0"} {
		if _, err := NewPolicy(s, testAudience, v); err == nil {
			t.Errorf("client version %q was accepted; an unparseable version must be denied", v)
		}
	}
}

func TestNewPolicyRejectsBadWindow(t *testing.T) {
	cases := map[string]func(*Signed){
		"expires before issued": func(s *Signed) { s.ExpiresAt = s.IssuedAt.Add(-time.Minute) },
		"expires equals issued": func(s *Signed) { s.ExpiresAt = s.IssuedAt },
		"zero issued":           func(s *Signed) { s.IssuedAt = time.Time{} },
		"zero expires":          func(s *Signed) { s.ExpiresAt = time.Time{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := signedFixture(base)
			mutate(&s)
			if _, err := NewPolicy(s, testAudience, "1.2.0"); !errors.Is(err, ErrBadWindow) {
				t.Fatalf("err = %v, want ErrBadWindow", err)
			}
		})
	}
}

func TestNewPolicyRejectsEmptyIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*Signed){
		"no keyId":         func(s *Signed) { s.KeyID = "" },
		"no policyVersion": func(s *Signed) { s.PolicyVersion = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s := signedFixture(base)
			mutate(&s)
			if _, err := NewPolicy(s, testAudience, "1.2.0"); !errors.Is(err, ErrEmptyIdentity) {
				t.Fatalf("err = %v, want ErrEmptyIdentity", err)
			}
		})
	}
}

// TestEvaluateFailsClosedOnUnknownCapability is the single most important rule
// here: the matrix is the only naming authority, so an id it does not name is
// denied rather than treated as unrestricted.
func TestEvaluateFailsClosedOnUnknownCapability(t *testing.T) {
	p := mustPolicy(t, signedFixture(base), "1.2.0")

	got := p.Evaluate(Request{Capability: "never_heard_of_it"}, base)
	if got.Decision != Deny {
		t.Errorf("Decision = %q, want deny for an unknown capability", got.Decision)
	}
}

func TestEvaluateHonoursTheMatrix(t *testing.T) {
	p := mustPolicy(t, signedFixture(base), "1.2.0")

	cases := []struct {
		capability string
		want       Decision
	}{
		{"ask_cap", Ask},
		{"allow_cap", Allow},
		{"deny_cap", Deny},
		// entitle / availability are checked before permissionPolicy, so an
		// explicit "allow" cannot override them.
		{"unentitled_cap", Deny},
		{"absent_cap", Deny},
	}
	for _, tc := range cases {
		t.Run(tc.capability, func(t *testing.T) {
			if got := p.Evaluate(Request{Capability: tc.capability}, base); got.Decision != tc.want {
				t.Errorf("Decision = %q, want %q (reason: %s)", got.Decision, tc.want, got.Reason)
			}
		})
	}
}

// TestEvaluateBackgroundCanOnlyTighten: a scheduled action has nobody watching
// it, so it must not inherit an interactive "allow".
func TestEvaluateBackgroundCanOnlyTighten(t *testing.T) {
	p := mustPolicy(t, signedFixture(base), "1.2.0")

	if got := p.Evaluate(Request{Capability: "allow_cap", Background: true}, base); got.Decision != Ask {
		t.Errorf("background allow_cap = %q, want ask", got.Decision)
	}
	if got := p.Evaluate(Request{Capability: "ask_cap", Background: true}, base); got.Decision != Ask {
		t.Errorf("background ask_cap = %q, want ask", got.Decision)
	}
	if got := p.Evaluate(Request{Capability: "deny_cap", Background: true}, base); got.Decision != Deny {
		t.Errorf("background deny_cap = %q, want deny", got.Decision)
	}
}

// TestEvaluateUnrecognisedPermissionPolicyDenies: a missing or misspelled
// permissionPolicy is not permission.
func TestEvaluateUnrecognisedPermissionPolicyDenies(t *testing.T) {
	s := signedFixture(base)
	s.Capabilities = []productclient.Capability{
		{ID: "empty_policy", Available: true, Entitled: true, PermissionPolicy: ""},
		{ID: "typo_policy", Available: true, Entitled: true, PermissionPolicy: "allowed"},
	}
	p := mustPolicy(t, s, "1.2.0")

	for _, id := range []string{"empty_policy", "typo_policy"} {
		if got := p.Evaluate(Request{Capability: id}, base); got.Decision != Deny {
			t.Errorf("%s = %q, want deny", id, got.Decision)
		}
	}
}

// TestExpiredPolicyKeepsOnlyLocalReadOnlyWork is the P0-04 §过期语义 rule: an
// expired policy must not silently become "no policy", and must not keep opening
// new fronts either.
func TestExpiredPolicyKeepsOnlyLocalReadOnlyWork(t *testing.T) {
	s := signedFixture(base)
	s.ExpiresAt = base.Add(-time.Minute)
	p := mustPolicy(t, s, "1.2.0")

	if !p.Expired(base) {
		t.Fatal("fixture is not expired")
	}

	if got := p.Evaluate(Request{Capability: "allow_cap", LocalReadOnly: true}, base); got.Decision != Allow {
		t.Errorf("expired + local read-only = %q, want allow", got.Decision)
	}
	if got := p.Evaluate(Request{Capability: "allow_cap"}, base); got.Decision != Deny {
		t.Errorf("expired + no read-only assertion = %q, want deny", got.Decision)
	}
	if got := p.Evaluate(Request{Capability: "allow_cap", LocalReadOnly: true, Background: true}, base); got.Decision != Deny {
		t.Errorf("expired + background = %q, want deny", got.Decision)
	}
	if got := p.Evaluate(Request{Capability: "ask_cap", LocalReadOnly: true}, base); got.Decision != Deny {
		t.Errorf("expired + ask_cap = %q, want deny; only an explicit allow survives", got.Decision)
	}
	if got := p.Evaluate(Request{Capability: "allow_cap", Target: "/tmp/x", LocalReadOnly: false}, base); got.Decision != Deny {
		t.Errorf("expired + undeclared action = %q, want deny", got.Decision)
	}
}

// TestClockBehindIssuedAtIsNotValid: a client clock far in the past would make an
// expired policy look valid indefinitely, so the window is closed in both
// directions — and a clock anomaly denies everything, including the local
// read-only work that plain expiry still permits (P0-04 groups it with
// signature failure, not with expiry).
func TestClockBehindIssuedAtIsNotValid(t *testing.T) {
	p := mustPolicy(t, signedFixture(base), "1.2.0")

	if !p.NotYetValid(base.Add(-2 * time.Hour)) {
		t.Error("a clock two hours behind issuedAt must be treated as not yet valid")
	}
	if p.NotYetValid(base.Add(-time.Minute)) {
		t.Error("a small lag is inside the allowance and must be tolerated")
	}

	skewed := base.Add(-2 * time.Hour)
	if got := p.Evaluate(Request{Capability: "allow_cap", LocalReadOnly: true}, skewed); got.Decision != Deny {
		t.Errorf("clock anomaly = %q, want deny even for local read-only work", got.Decision)
	}
	if !p.Expired(base.Add(2 * time.Hour)) {
		t.Error("the same envelope must read as expired well past expiresAt")
	}
}

// TestAcceptVersionRejectsRollback: a replayed older envelope is correctly
// signed, so only monotonicity stops it from re-granting a withdrawn capability.
// The version format here is the real date-based one, which is what makes this
// test meaningful.
func TestAcceptVersionRejectsRollback(t *testing.T) {
	s := signedFixture(base)
	s.PolicyVersion = "2026-09-10.2"
	p := mustPolicy(t, s, "1.2.0")

	if err := p.AcceptVersion(""); err != nil {
		t.Errorf("the first policy must always be accepted: %v", err)
	}
	if err := p.AcceptVersion("2026-09-10.2"); err != nil {
		t.Errorf("accepting the same version again must be a no-op: %v", err)
	}
	if err := p.AcceptVersion("2026-09-10.1"); err != nil {
		t.Errorf("a newer version must be accepted: %v", err)
	}
	if err := p.AcceptVersion("2026-09-10.10"); !errors.Is(err, ErrVersionRollback) {
		t.Errorf("err = %v, want ErrVersionRollback", err)
	}
	if err := p.AcceptVersion("not-a-version"); !errors.Is(err, ErrVersionRollback) {
		t.Errorf("an unparseable version must not be accepted: %v", err)
	}
}

// TestAcceptVersionOrdersTheCounterNumerically guards the specific replay the
// string comparator would miss: "2026-09-10.10" sorts *after* "2026-09-10.2"
// numerically but *before* it lexicographically.
func TestAcceptVersionOrdersTheCounterNumerically(t *testing.T) {
	s := signedFixture(base)
	s.PolicyVersion = "2026-09-10.10"
	p := mustPolicy(t, s, "1.2.0")

	if err := p.AcceptVersion("2026-09-10.2"); err != nil {
		t.Errorf("2026-09-10.10 is newer than 2026-09-10.2 and must be accepted: %v", err)
	}
}

// TestDuplicateCapabilityIDsResolveStricter: a duplicated id is a platform bug,
// but resolving it by last-write-wins would make the verdict depend on JSON
// ordering, which is not a security property.
func TestDuplicateCapabilityIDsResolveStricter(t *testing.T) {
	s := signedFixture(base)
	s.Capabilities = []productclient.Capability{
		{ID: "dup", Available: true, Entitled: true, PermissionPolicy: productclient.PermissionAllow},
		{ID: "dup", Available: true, Entitled: true, PermissionPolicy: productclient.PermissionDeny},
	}
	p := mustPolicy(t, s, "1.2.0")

	if got := p.Evaluate(Request{Capability: "dup"}, base); got.Decision != Deny {
		t.Errorf("duplicate id resolved to %q, want the stricter deny", got.Decision)
	}

	// Order must not matter.
	s.Capabilities[0], s.Capabilities[1] = s.Capabilities[1], s.Capabilities[0]
	p2 := mustPolicy(t, s, "1.2.0")
	if got := p2.Evaluate(Request{Capability: "dup"}, base); got.Decision != Deny {
		t.Errorf("reversed order resolved to %q, want the stricter deny", got.Decision)
	}
}

// TestTargetDoesNotWidenTheVerdict documents that Target is audit metadata, not
// a second policy input. If it ever starts changing the answer, a weaker
// path-based policy has crept in.
func TestTargetDoesNotWidenTheVerdict(t *testing.T) {
	p := mustPolicy(t, signedFixture(base), "1.2.0")

	for _, target := range []string{"", "/etc/passwd", "example.com", "\x00"} {
		if got := p.Evaluate(Request{Capability: "deny_cap", Target: target}, base); got.Decision != Deny {
			t.Errorf("target %q changed the verdict to %q", target, got.Decision)
		}
	}
}

func TestParseDecisionRejectsUnknown(t *testing.T) {
	for _, ok := range []string{"deny", "ask", "allow", " deny "} {
		if _, found := ParseDecision(ok); !found {
			t.Errorf("ParseDecision(%q) should be recognised", ok)
		}
	}
	for _, bad := range []string{"", "ALLOW", "yes", "permit"} {
		if _, found := ParseDecision(bad); found {
			t.Errorf("ParseDecision(%q) should not be recognised", bad)
		}
	}
}

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		// Client-facing semver.
		{"1.0.0", "1.0.1", true},
		{"1.2", "1.2.0", false},
		{"1.10.0", "1.9.0", false},
		{"0.0.1", "1.0.0", true},
		// The policy/catalog format is date-based, not semver. Missing this
		// is a real defect: the comparator would reject every policy.
		{"2026-09-10.1", "2026-09-10.2", true},
		{"2026-09-10.2", "2026-09-10.10", true},
		{"2026-09-11.0", "2026-09-10.9", false},
		{"2026-09-10", "2026-09-10.1", true},
	}
	for _, tc := range cases {
		got, err := VersionLess(tc.a, tc.b)
		if err != nil {
			t.Errorf("VersionLess(%q, %q): %v", tc.a, tc.b, err)
			continue
		}
		if got != tc.want {
			t.Errorf("VersionLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestCompareVersionsRejectsUnsupportedForms: "we could not order these" must be
// an error the caller denies on, never a silent 0 that reads as "equal".
func TestCompareVersionsRejectsUnsupportedForms(t *testing.T) {
	for _, v := range []string{"", "1.2.0-rc1", "v1.2.0", "1.x", "1..2", "-1.0.0", "1.-2.0", "1.2.", ".1.2"} {
		if _, err := CompareVersions(v, "1.0.0"); err == nil {
			t.Errorf("CompareVersions(%q, ...) accepted an unsupported form", v)
		}
	}
	if cmp, err := CompareVersions("1.2.0", "1.2.0"); err != nil || cmp != 0 {
		t.Errorf("CompareVersions equal versions = (%d, %v), want (0, nil)", cmp, err)
	}
	// Collisions are the failure mode this guards: two distinct strings must
	// not compare equal, or a replay check silently passes.
	if cmp, err := CompareVersions("1..2", "1.2"); err == nil && cmp == 0 {
		t.Error(`"1..2" must not compare equal to "1.2"`)
	}
}

// TestCatalogIsTheOnlyModelSource pins that the policy carries the catalog, so
// there is no second place a model list can come from.
func TestCatalogIsTheOnlyModelSource(t *testing.T) {
	p := mustPolicy(t, signedFixture(base), "1.2.0")

	cat := p.Catalog()
	if len(cat.Models) != 1 || cat.Models[0].ID != "buding-cloud-pro" {
		t.Fatalf("catalog = %+v", cat)
	}
	if !cat.Models[0].Usable() {
		t.Error("the fixture model should be usable (eligible + gateway)")
	}
}

// TestModelUsableRejectsNonGatewayTransport: a catalog entry that is not
// gateway-routed is the platform trying to hand the client a direct supplier
// path, which the client must not accept.
func TestModelUsableRejectsNonGatewayTransport(t *testing.T) {
	m := productclient.Model{ID: "x", Eligible: true, Transport: "direct"}
	if m.Usable() {
		t.Error("a non-gateway model must never be selectable")
	}
	m.Transport = productclient.TransportGateway
	m.Eligible = false
	if m.Usable() {
		t.Error("an ineligible model must never be selectable")
	}
	m.Eligible = true
	m.ID = ""
	if m.Usable() {
		t.Error("a model with no id must never be selectable")
	}
}

func TestRequestStateFinal(t *testing.T) {
	final := []productclient.RequestState{productclient.StateSettled, productclient.StateReversed}
	for _, s := range final {
		if !s.Final() {
			t.Errorf("%q should be final", s)
		}
	}
	// reconciliation_pending is explicitly not final: its charge is not
	// authoritative yet, so it cannot be collapsed into settled or reversed.
	notFinal := []productclient.RequestState{
		productclient.StateReceived, productclient.StateReserved, productclient.StateStreaming,
		productclient.StateReconciliationPending, productclient.StateFailed, "unknown_state",
	}
	for _, s := range notFinal {
		if s.Final() {
			t.Errorf("%q must not be final", s)
		}
	}
}
