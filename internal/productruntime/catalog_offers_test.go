// Tests for the turn guard's judgement (PR-5e / L-C7): is the model a session is
// bound to still offered by the catalog in hand, and can that be answered at all.
package productruntime

import (
	"encoding/json"
	"testing"

	"github.com/open-octo/octo-agent/internal/catalogstore"
	"github.com/open-octo/octo-agent/internal/productclient"
)

// The ids the stand-in's signed catalog carries: FixturePolicy (clienttest/sign.go)
// lists buding-privacy-1 / buding-cloud-pro / buding-cloud-fast, so the first is
// the "still offered" case and the second is a plausible id the catalog does NOT
// list. Nothing here asserts the fixture's contents (that would be a second copy
// of its list) - it asserts what the guard answers, so adding a model to the
// fixture can only make TestAModelTheCatalogDroppedIsNotOffered fail loudly
// rather than pass vacuously.
const (
	fixturePresentModel = "buding-cloud-pro"
	fixtureAbsentModel  = "buding-cloud-retired"
)

// TestACatalogThatListsTheModelOffersIt is the ordinary case: the guard must not
// get in the way of a turn that is perfectly servable.
func TestACatalogThatListsTheModelOffersIt(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()

	offers, known := f.rt.CatalogOffers(fixturePresentModel)
	if !known {
		t.Fatal("a primed catalog is in hand; the question is answerable")
	}
	if !offers {
		t.Errorf("the catalog lists %q, so it is offered", fixturePresentModel)
	}
}

// TestAModelTheCatalogDroppedIsNotOffered is the judgement L-C7 refuses a turn
// on. The catalog is in hand and readable - it simply no longer lists the id.
func TestAModelTheCatalogDroppedIsNotOffered(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()

	offers, known := f.rt.CatalogOffers(fixtureAbsentModel)
	if !known {
		t.Fatal("a primed catalog is in hand; the question is answerable")
	}
	if offers {
		t.Errorf("the catalog does not list %q, so it is not offered", fixtureAbsentModel)
	}
}

// TestNoCatalogIsNotAWithdrawal is the reason the answer is two-valued. Without
// it, a fresh installation - which has no catalog at all until its first login
// - would refuse every turn with a statement about a model nobody withdrew,
// and B4/B9 already own the sentences for that family.
func TestNoCatalogIsNotAWithdrawal(t *testing.T) {
	f := newCatalogFixture(t)
	// Deliberately no prime(): the store holds nothing.

	offers, known := f.rt.CatalogOffers(fixturePresentModel)
	if known {
		t.Fatal("there is no catalog, so nothing can be said about any model - not even 'offered'")
	}
	if offers {
		t.Error("an answer nobody can give must not read as 'offered' either")
	}
}

// TestACatalogThatWillNotReadBackIsNotAWithdrawal: the bytes we hold decode to
// nothing usable. The remedy is a refresh, not picking another model, so the
// guard stays quiet and the turn keeps today's behaviour (the gateway answers).
func TestACatalogThatWillNotReadBackIsNotAWithdrawal(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()

	// A policy that is valid JSON and not a policy object: Put stores bytes
	// verbatim without judging them (verification is internal/productclient's, and
	// Put is deliberately not a second opinion), so this reaches the cache and
	// fails at DecodePolicy - which is the branch under test.
	if err := f.store.Put(catalogstore.Entry{
		CatalogVersion: "9999-12-31",
		Envelope:       productclient.PolicyEnvelope{Policy: json.RawMessage(`"not a policy"`)},
	}); err != nil {
		t.Fatalf("storing an undecodable policy: %v", err)
	}

	offers, known := f.rt.CatalogOffers(fixturePresentModel)
	if known {
		t.Fatalf("a catalog that will not decode cannot support a verdict (offers=%v)", offers)
	}
}

// TestNoCatalogStoreIsNotAWithdrawal: a build wired without a store (every
// non-product build) must leave the guard inert rather than refusing turns.
func TestNoCatalogStoreIsNotAWithdrawal(t *testing.T) {
	rt := New(Deps{})

	if offers, known := rt.CatalogOffers(fixturePresentModel); known || offers {
		t.Errorf("no store means no verdict, got offers=%v known=%v", offers, known)
	}
}

// TestAnEmptyIDIsNeverJudged: the caller strips the endpoint prefix off the
// session's binding, and a binding that carried nothing but the prefix would
// otherwise be reported as a withdrawn model named "".
func TestAnEmptyIDIsNeverJudged(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()

	if offers, known := f.rt.CatalogOffers(""); known || offers {
		t.Errorf("an empty id is not a model, got offers=%v known=%v", offers, known)
	}
}

func TestTheGuardReadsEligibilityFromTheCatalogRow(t *testing.T) {
	policy := productclient.Policy{Catalog: productclient.Catalog{
		Vendors: []productclient.CatalogVendor{{ID: "vendor"}},
		Models: []productclient.CatalogModel{
			{ID: "ineligible", VendorID: "vendor", Transport: productclient.CatalogTransportGateway},
			{ID: "selectable", VendorID: "vendor", Transport: productclient.CatalogTransportGateway, Eligible: true},
		},
	}}

	ineligible, known := catalogEligibility(policy, "ineligible")
	if !known || ineligible.Selectable {
		t.Fatalf("ineligible = %+v known=%v", ineligible, known)
	}
	selectable, known := catalogEligibility(policy, "selectable")
	if !known || !selectable.Selectable {
		t.Fatalf("selectable = %+v known=%v", selectable, known)
	}
}
