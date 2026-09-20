package productruntime

import (
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
	"github.com/open-octo/octo-agent/internal/productprofile"
)

func fixtureCatalogPolicy(t *testing.T) productclient.Policy {
	t.Helper()
	return clienttest.FixturePolicy(time.Now())
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestProjectCatalogPreservesVendorHierarchyAndEligibility(t *testing.T) {
	policy := fixtureCatalogPolicy(t)
	for i := range policy.Catalog.Models {
		if policy.Catalog.Models[i].ID == "buding-cloud-fast" {
			policy.Catalog.Models[i].Eligible = false
		}
	}

	vendors, err := projectCatalog(policy)
	if err != nil {
		t.Fatalf("projectCatalog: %v", err)
	}
	if len(vendors) != len(policy.Catalog.Vendors) {
		t.Fatalf("vendors = %d, want %d", len(vendors), len(policy.Catalog.Vendors))
	}
	if vendors[0].ID != "buding" || len(vendors[0].Models) != 2 {
		t.Fatalf("first vendor = %#v, want the two eligible buding models", vendors[0])
	}
	if vendors[1].ID != "partner" || len(vendors[1].Models) != 0 {
		t.Fatalf("second vendor = %#v, want an empty vendor after its model became ineligible", vendors[1])
	}
}

func TestUnavailableModelIsVisibleButCannotBeUsed(t *testing.T) {
	policy := fixtureCatalogPolicy(t)
	for i := range policy.Catalog.Models {
		if policy.Catalog.Models[i].ID == "buding-cloud-fast" {
			policy.Catalog.Models[i].Eligible = false
			policy.Catalog.Models[i].AvailabilityReason = "pricing_not_configured"
		}
	}
	display, err := projectCatalogRows(policy, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(display[1].Models) != 1 || display[1].Models[0].Eligible || display[1].Models[0].AvailabilityReason != "pricing_not_configured" {
		t.Fatalf("missing unavailable model explanation: %+v", display[1])
	}
	runtime, err := projectCatalog(policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime[1].Models) != 0 {
		t.Fatal("display-only model leaked into runtime offers")
	}
	eligibility, found := catalogEligibility(policy, "buding-cloud-fast")
	if !found || eligibility.Selectable {
		t.Fatal("unavailable model was authorized")
	}
}

func TestProjectCatalogCarriesRoutingAndConfidentialMetadata(t *testing.T) {
	vendors, err := projectCatalog(fixtureCatalogPolicy(t))
	if err != nil {
		t.Fatalf("projectCatalog: %v", err)
	}
	model := vendors[0].Models[0]
	if model.ID != "buding-privacy-1" {
		t.Fatalf("first model = %q, want fixture source order", model.ID)
	}
	if model.CompositeID != productprofile.GatewayEndpointID+"::"+model.ID {
		t.Errorf("composite id = %q", model.CompositeID)
	}
	if !model.Confidential || model.ConfidentialPriority == nil || *model.ConfidentialPriority != 100 {
		t.Errorf("confidential metadata = %v/%v", model.Confidential, model.ConfidentialPriority)
	}
}

func TestInvalidCatalogIsRejectedAsAWhole(t *testing.T) {
	policy := fixtureCatalogPolicy(t)
	policy.Catalog.Models[0].VendorID = "missing"
	if _, err := projectCatalog(policy); err == nil {
		t.Fatal("projectCatalog accepted an unknown vendor reference")
	}
}

func TestCatalogEligibilityUsesTheSameSignedRow(t *testing.T) {
	policy := fixtureCatalogPolicy(t)
	got, known := catalogEligibility(policy, "buding-privacy-1")
	if !known || !got.Selectable || !got.Confidential || got.ConfidentialPriority == nil || *got.ConfidentialPriority != 100 {
		t.Fatalf("eligibility = %+v known=%v", got, known)
	}
	missing, known := catalogEligibility(policy, "retired")
	if !known || missing.Selectable || missing.Confidential {
		t.Fatalf("missing eligibility = %+v known=%v", missing, known)
	}
}

func TestPreferredConfidentialModelUsesPriorityThenSourceOrder(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()

	got, ok := f.rt.PreferredConfidentialModel()
	if !ok {
		t.Fatal("fixture has a current confidential model")
	}
	want := productprofile.GatewayEndpointID + "::buding-privacy-1"
	if got != want {
		t.Fatalf("preferred = %q, want %q", got, want)
	}
}
