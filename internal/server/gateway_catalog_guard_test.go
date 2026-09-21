// Nails for PR-5e (L-C7): a session bound to a catalog model the current catalog
// no longer offers is refused BEFORE a sender is built, so a turn that cannot be
// served never spends a token exchange — and never reaches a third party.
package server

import (
	"context"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
)

// offeredCatalog is a stand-in for productruntime.Runtime.CatalogOffers: it knows
// the ids in the list and nothing else. Counting the calls lets a nail assert the
// guard was consulted at all, which a test with a nil field would not.
func offeredCatalog(ids ...string) (func(string) (bool, bool), *int) {
	in := map[string]bool{}
	for _, id := range ids {
		in[id] = true
	}
	calls := 0
	return func(id string) (bool, bool) {
		calls++
		return in[id], true
	}, &calls
}

// gatewayServerWithCatalog builds a gateway-wired server (the PR-5a shape) whose
// catalog answers from ids, with a factory that records whether it was called.
func gatewayServerWithCatalog(t *testing.T, offers func(string) (bool, bool)) (*Server, *int) {
	t.Helper()
	srv, _ := serverWithCountingSender(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
		CatalogOffers:      offers,
	}, seedOrdinaryEndpoint())

	built := 0
	srv.cfg.GatewaySender = func(app.ReasoningTuning) (agent.Sender, error) {
		built++
		return &countingSender{}, nil
	}
	return srv, &built
}

// catalogBoundSession is a session the picker bound to a catalog model: both the
// name and the binding ref carry the composite id, which is what
// handleUpdateSessionModel stores when the id comes from the signed catalog. The
// sibling file's gatewayBoundSession() sets only the name (the V-35 shape); both
// are gateway-bound, and the guard reads the BARE id either way.
func catalogBoundSession(id string) *agent.Session {
	return boundSession(testGatewayPrefix+id, testGatewayPrefix+id)
}

// The main nail, and the one that pins the ORDER: a model the catalog dropped is
// refused before the factory runs. If this check ever moves after the factory, the
// refusal still happens — and the build has already exchanged a token for a turn
// it knew could not be served.
func TestAModelTheCatalogDroppedIsRefusedBeforeTheSenderIsBuilt(t *testing.T) {
	offers, _ := offeredCatalog("buding-cloud-pro")
	srv, built := gatewayServerWithCatalog(t, offers)

	sender, model := srv.senderForSession(catalogBoundSession("buding-cloud-retired"))

	if *built != 0 {
		t.Errorf("the gateway factory ran %d time(s) for a model the catalog does not list: building the sender can exchange a token, so this is a round trip spent on a turn that cannot start", *built)
	}
	if model != "buding-cloud-retired" {
		t.Errorf("model = %q, want the bare catalog id", model)
	}
	_, err := sender.SendMessages(context.Background(), model, "", nil, 0)
	if err == nil {
		t.Fatal("SendMessages returned nil for a model the catalog no longer lists")
	}
	if !strings.Contains(err.Error(), "buding-cloud-retired") {
		t.Errorf("refusal %q must name the model, so the user can tell which one to replace", err.Error())
	}
}

// The other half of the same judgement: a model the catalog still lists is
// served. Without this, a guard that refused everything would pass the nail above.
func TestAModelTheCatalogStillListsIsServedByTheGateway(t *testing.T) {
	offers, calls := offeredCatalog("buding-cloud-pro")
	srv, built := gatewayServerWithCatalog(t, offers)

	sender, _ := srv.senderForSession(catalogBoundSession("buding-cloud-pro"))

	if *calls == 0 {
		t.Error("the catalog was never consulted: this nail cannot tell 'offered' from 'not checked'")
	}
	if *built != 1 {
		t.Fatalf("the gateway factory ran %d time(s), want exactly 1", *built)
	}
	if sender == srv.sender {
		t.Error("a catalog model rode the default sender instead of the gateway")
	}
}

// known=false is the whole reason the field is not a bool: a build that cannot
// answer must not refuse. Otherwise a fresh installation — no catalog until its
// first login — refuses every turn with a statement about a model nobody
// withdrew, and the user is told to reselect from a list that does not exist.
func TestAnUnanswerableCatalogRefusesNothing(t *testing.T) {
	never := func(string) (bool, bool) { return false, false }
	srv, built := gatewayServerWithCatalog(t, never)

	sender, _ := srv.senderForSession(catalogBoundSession("buding-cloud-pro"))

	if *built != 1 {
		t.Fatalf("the gateway factory ran %d time(s), want 1: a question nobody can answer must not refuse the turn", *built)
	}
	if sender == srv.sender {
		t.Error("the turn was handed to the default sender rather than the gateway")
	}
}

// nil means unchanged upstream behavior — the CLI, `octo serve` and every test
// that predates this field. It is the reason the field is a nilable function.
func TestNoCatalogGuardIsUnchangedUpstreamBehavior(t *testing.T) {
	srv, _ := serverWithCountingSender(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
	}, seedOrdinaryEndpoint())

	built := 0
	srv.cfg.GatewaySender = func(app.ReasoningTuning) (agent.Sender, error) {
		built++
		return &countingSender{}, nil
	}

	sender, _ := srv.senderForSession(catalogBoundSession("buding-cloud-anything"))

	if built != 1 {
		t.Fatalf("the gateway factory ran %d time(s), want 1: with no guard wired the turn proceeds as it did before PR-5e", built)
	}
	if sender == srv.sender {
		t.Error("a gateway-bound model rode the default sender")
	}
}

// A build fault outranks a session fact. "This build has no gateway at all" is
// not something the catalog can fix, and answering it with "the catalog dropped
// your model" would be a false statement about the user's session.
func TestABuildWithNoGatewayStillSaysSo(t *testing.T) {
	offers, _ := offeredCatalog() // lists nothing: every id is "dropped"
	srv, _ := serverWithCountingSender(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
		CatalogOffers:      offers,
	}, seedOrdinaryEndpoint())
	// No GatewaySender: the PR-5c/PR-4c0 wiring where the build cannot reach it.

	sender, _ := srv.senderForSession(catalogBoundSession("buding-cloud-retired"))

	_, err := sender.SendMessages(context.Background(), "x", "", nil, 0)
	if err == nil {
		t.Fatal("a build with no gateway must refuse")
	}
	if !strings.Contains(err.Error(), "built-in gateway") {
		t.Errorf("refusal %q must name the build fault, not the catalog: the user cannot fix this by picking another model", err.Error())
	}
}

// The guard is about CATALOG models. A model that names some other endpoint is
// not the catalog's business: with the control plane not required (every
// developer build), such a turn keeps riding the default sender, which is the
// local-development path D-008 protects. Refusing it here would tell a developer
// their config.yml model "was withdrawn".
func TestANonGatewayModelIsNotTheCatalogGuardsBusiness(t *testing.T) {
	offers, calls := offeredCatalog("buding-cloud-pro")
	srv, counted := serverWithCountingSender(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
		CatalogOffers:      offers,
	}, seedOrdinaryEndpoint())

	sess := boundSession("ep-kimi::kimi-k2.6", "ep-kimi::kimi-k2.6")
	sender, _ := srv.senderForSession(sess)

	if *calls != 0 {
		t.Errorf("the catalog was consulted %d time(s) for a model that does not name the gateway", *calls)
	}
	// The precise claim: this turn was not refused. Asserting `sender == srv.sender`
	// would be wrong — senderForSession builds a real sender from the resolved
	// entry, and `srv.sender` is only the fallback it never reaches.
	if _, refused := sender.(failingSender); refused {
		t.Error("a configured third-party endpoint model was refused by the catalog guard")
	}
	if counted.calls != 0 {
		t.Error("building the sender must not have sent anything")
	}
}

// The refusal must not be mistaken for the PR-5c refusal: they send the user to
// different places ("pick another model" vs "this build cannot reach the
// control plane"), and one string for both would make the distinction
// unobservable.
func TestTheCatalogRefusalIsItsOwnMessage(t *testing.T) {
	if got, other := errModelNotListed("m").Error(), errModelNotFromCatalog("m").Error(); got == other {
		t.Errorf("the two refusals are the same string: %q", got)
	}
	// The one word that matters: this refusal sends the user to the picker.
	if !strings.Contains(errModelNotListed("m").Error(), "pick one from the list") {
		t.Errorf("refusal %q must tell the user what to do about it", errModelNotListed("m"))
	}
}

// OCTO-FORK: an expired catalog cannot establish that a model was withdrawn.
func TestExpiredCatalogDoesNotReportModelWithdrawn(t *testing.T) {
	srv, built := gatewayServerWithCatalog(t, nil)
	srv.cfg.CatalogModel = func(string) (CatalogModelStatus, bool) {
		return CatalogModelStatus{Current: false, Selectable: false}, true
	}
	sender, model := srv.senderForSession(catalogBoundSession("buding-cloud-pro"))
	_, err := sender.SendMessages(context.Background(), model, "", nil, 0)
	if err == nil {
		t.Fatal("expired catalog must not start a turn")
	}
	if got := agent.ErrorCodeOf(err); got != "catalog_unavailable" {
		t.Fatalf("expired catalog code=%q, want catalog_unavailable; error=%v", got, err)
	}
	if *built != 0 {
		t.Fatal("expired catalog must not build an upstream sender")
	}
}
