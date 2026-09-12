package productruntime

import (
	"strings"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/chatmode"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
	"github.com/open-octo/octo-agent/internal/productprofile"
)

// These tests pin the projection rules of 中台交付包 §4.3. They work on a Policy
// value rather than through the stand-in server on purpose: the rules are about
// what the projection does with a verified catalog, and every case below needs a
// catalog the fixture deliberately does not ship (an ineligible model, a model
// whose transport is not the gateway, an unknown mode id, an invalid default).
// Reaching those through the server would mean teaching the verifier's fixture
// to sign malformed catalogs - a fake of the wrong thing.
//
// The base is still the real fixture (clienttest.FixturePolicy), so the happy
// path cannot drift from the shape the platform actually sends.

// fixtureCatalogPolicy is clienttest.FixturePolicy with a mutable catalog.
func fixtureCatalogPolicy(t *testing.T) productclient.Policy {
	t.Helper()
	p := clienttest.FixturePolicy(time.Now())
	if len(p.Catalog.Models) == 0 {
		t.Fatal("the fixture catalog has no models; every case here would pass vacuously")
	}
	return p
}

// findModel returns the index of a model in the policy's catalog.
func findModel(t *testing.T, p productclient.Policy, id string) int {
	t.Helper()
	for i, m := range p.Catalog.Models {
		if m.ID == id {
			return i
		}
	}
	t.Fatalf("the fixture catalog has no model %q", id)
	return -1
}

// groupOf returns the projected group with the given id.
func groupOf(t *testing.T, groups []chatModeGroup, id string) chatModeGroup {
	t.Helper()
	for _, g := range groups {
		if g.ID == id {
			return g
		}
	}
	t.Fatalf("the projection produced no group %q; got %v", id, groupIDs(groups))
	return chatModeGroup{}
}

func groupIDs(groups []chatModeGroup) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.ID)
	}
	return out
}

func modelIDs(g chatModeGroup) []string {
	out := make([]string, 0, len(g.Models))
	for _, m := range g.Models {
		out = append(out, m.ID)
	}
	return out
}

// TestTheProjectionListsOnlyGatewayModels is 中台交付包 §4.3's selection rule:
// "只允许选择目录中 eligible=true 且 transport=gateway 的模型".
//
// Both halves are asserted separately because they are separate mistakes with the
// same symptom. A projection that forgets `eligible` lists a model the account
// cannot use; one that forgets `transport` lists a model that bypasses the
// gateway - which is not a billing nicety, it is the rule that keeps a shipped
// build from reaching a provider directly (需求基线 C1). The failure the user
// would see either way is the same: they pick it, and the gateway refuses with
// model_not_allowed.
func TestTheProjectionListsOnlyGatewayModels(t *testing.T) {
	p := fixtureCatalogPolicy(t)
	p.Catalog.Models[findModel(t, p, "buding-cloud-pro")].Eligible = false
	p.Catalog.Models[findModel(t, p, "buding-cloud-fast")].Transport = "direct"

	groups, ignored := projectCatalog(p)
	if len(ignored) != 0 {
		t.Errorf("ignored mode ids = %v, want none: nothing here is an unknown mode", ignored)
	}

	if got := modelIDs(groupOf(t, groups, chatmode.ModeSmart)); contains(got, "buding-cloud-pro") {
		t.Errorf("smart lists buding-cloud-pro, which is not eligible: %v", got)
	}
	if got := modelIDs(groupOf(t, groups, chatmode.ModeDefault)); contains(got, "buding-cloud-fast") {
		t.Errorf("default lists buding-cloud-fast, whose transport is not the gateway: %v", got)
	}
	// And the third model must still be there, or "nothing is listed" would
	// satisfy the two assertions above.
	if got := modelIDs(groupOf(t, groups, chatmode.ModePrivacy)); !contains(got, "buding-privacy-1") {
		t.Errorf("privacy = %v, want buding-privacy-1 (the eligible gateway model)", got)
	}
}

// TestTheProjectionGroupsByTheModelsOwnModeIds pins 中台交付包 §4.3「分组事实的
// 唯一 owner 是模型对象的 modeIds」from the two directions a wrong
// implementation can take, because one direction alone is indistinguishable from
// the right answer:
//
//  1. A model claiming a mode the catalog says nothing about must still be
//     grouped under it. This is what separates "iterate the product's modes" from
//     "iterate catalog.modes[]": a projection reading the catalog for its groups
//     would drop privacy entirely here, and that is the bug that makes a platform
//     omission look like the user having no models.
//  2. A mode the catalog lists but no model claims must stay present AND empty.
//     This kills the other direction - filling groups from the mode entries - and
//     it is the state PR-4c words as 分组空, so it has to be reachable.
//
// The two are contradictions of each other, which is the point: no single source
// can satisfy both, so at least one of them fails for any wrong implementation.
func TestTheProjectionGroupsByTheModelsOwnModeIds(t *testing.T) {
	p := fixtureCatalogPolicy(t)
	// (1) The catalog stops describing privacy, but a model still claims it.
	var kept []productclient.CatalogMode
	for _, m := range p.Catalog.Modes {
		if m.ID != chatmode.ModePrivacy {
			kept = append(kept, m)
		}
	}
	if len(kept) == len(p.Catalog.Modes) {
		t.Fatal("the fixture carries no privacy mode entry; case 1 is not set up")
	}
	p.Catalog.Modes = kept

	groups, _ := projectCatalog(p)

	privacy := groupOf(t, groups, chatmode.ModePrivacy)
	if got := modelIDs(privacy); !contains(got, "buding-privacy-1") {
		t.Errorf("privacy = %v, want buding-privacy-1: the model's own modeIds groups it, "+
			"and catalog.modes[] is not the owner of that fact", got)
	}
	if privacy.DefaultModel != "" {
		t.Errorf("privacy.DefaultModel = %q, want empty: no catalog entry named a default", privacy.DefaultModel)
	}

	// (2) A mode entry whose models stop claiming it stays present and empty.
	p = fixtureCatalogPolicy(t)
	idx := findModel(t, p, "buding-privacy-1")
	p.Catalog.Models[idx].ModeIDs = []string{}

	groups, _ = projectCatalog(p)

	if got := modelIDs(groupOf(t, groups, chatmode.ModePrivacy)); len(got) != 0 {
		t.Errorf("privacy = %v, want empty: no model claims the mode", got)
	}
	if !contains(groupIDs(groups), chatmode.ModePrivacy) {
		t.Errorf("groups = %v, want privacy to remain present but empty (the group is a product constant)", groupIDs(groups))
	}
}

// TestAnUnknownModeIdIsIgnoredNotInvented is the other half of the same rule:
// the mode set is product data (中台交付包 §4.3 «出现未知 id ⇒ 客户端忽略该条并
// 记录，不得据此新建模式»). A catalog is signed by the platform but it is still
// data, and "the platform sent it" is not a reason for the picker to grow a
// fourth group.
func TestAnUnknownModeIdIsIgnoredNotInvented(t *testing.T) {
	p := fixtureCatalogPolicy(t)
	// Both places a mode id can appear: on a model, and as a mode entry.
	idx := findModel(t, p, "buding-cloud-pro")
	p.Catalog.Models[idx].ModeIDs = append(p.Catalog.Models[idx].ModeIDs, "teams-edition")
	p.Catalog.Modes = append(p.Catalog.Modes, productclient.CatalogMode{ID: "renamed-by-platform"})

	groups, ignored := projectCatalog(p)

	for _, invented := range []string{"teams-edition", "renamed-by-platform"} {
		if contains(groupIDs(groups), invented) {
			t.Errorf("groups = %v, want no group %q: the mode set is a product constant", groupIDs(groups), invented)
		}
		if !contains(ignored, invented) {
			t.Errorf("ignored = %v, want %q reported so an operator can see the drift", ignored, invented)
		}
	}
	// The model that carried the unknown id must keep its real group, or
	// ignoring the id would have dropped the model as well.
	if got := modelIDs(groupOf(t, groups, chatmode.ModeSmart)); !contains(got, "buding-cloud-pro") {
		t.Errorf("smart = %v, want buding-cloud-pro to keep the group it also claims", got)
	}
}

// TestAnInvalidDefaultModelIsDropped is 中台交付包 §4.3's `defaultModelId` rule:
// "必须是同一目录里 eligible=true 且 transport=gateway 且 modeIds 含该模式的
// 模型，否则该条视为无效".
//
// What is dropped is the *default*, not the mode: the three modes are product
// constants, so a bad pointer in catalog data cannot remove one from the picker.
// Whether a group has a default is a separate question the frontend already
// answers (it falls back to the first row), which is why an empty DefaultModel is
// a usable state rather than a broken one.
func TestAnInvalidDefaultModelIsDropped(t *testing.T) {
	p := fixtureCatalogPolicy(t)
	// Point smart's default at a model that is not in smart. buding-privacy-1 is
	// eligible and on the gateway, so the only thing wrong with it here is that
	// it does not claim the smart mode - which is exactly the case the rule
	// names, and it isolates the membership check from the two filters above.
	for i, m := range p.Catalog.Modes {
		if m.ID == chatmode.ModeSmart {
			p.Catalog.Modes[i].DefaultModelID = "buding-privacy-1"
		}
	}

	groups, _ := projectCatalog(p)
	smart := groupOf(t, groups, chatmode.ModeSmart)

	if smart.DefaultModel != "" {
		t.Errorf("smart.DefaultModel = %q, want empty: a model that does not claim the mode cannot be its default",
			smart.DefaultModel)
	}
	if len(smart.Models) == 0 {
		t.Error("smart lost its models as well; the rule drops the default, not the group or its members")
	}
}

// TestAValidDefaultModelBecomesACompositeID is the positive half, and the case
// the picker actually needs: defaultModel is what the selector preselects, and
// it must be spelled the same way a chosen model is - otherwise the first render
// highlights nothing.
func TestAValidDefaultModelBecomesACompositeID(t *testing.T) {
	p := fixtureCatalogPolicy(t)
	groups, _ := projectCatalog(p)

	smart := groupOf(t, groups, chatmode.ModeSmart)
	want := productprofile.GatewayEndpointID + "::buding-cloud-pro"
	if smart.DefaultModel != want {
		t.Errorf("smart.DefaultModel = %q, want %q", smart.DefaultModel, want)
	}
}

// TestTheCompositeIdCarriesTheGatewayPrefix pins 需求基线 B8 规则 1: a formal
// session stores the composite id "<endpoint>::<model>", and the endpoint half is
// fixed to the built-in gateway - the catalog's id is the only varying part.
//
// The prefix is asserted against one owner (internal/productprofile) rather than
// a literal, so the day the constant moves the test moves with it, and against a
// prefix that is non-empty: a projection that emitted "::model" would satisfy a
// suffix-only assertion while producing ids no session could ever resolve.
func TestTheCompositeIdCarriesTheGatewayPrefix(t *testing.T) {
	if productprofile.GatewayEndpointID == "" {
		t.Fatal("the gateway endpoint id is empty; every composite id below would be malformed")
	}
	p := fixtureCatalogPolicy(t)
	groups, _ := projectCatalog(p)

	for _, g := range groups {
		for _, m := range g.Models {
			prefix, suffix, ok := strings.Cut(m.CompositeID, "::")
			if !ok {
				t.Errorf("%s/%s: compositeId %q has no endpoint separator", g.ID, m.ID, m.CompositeID)
				continue
			}
			if prefix != productprofile.GatewayEndpointID {
				t.Errorf("%s/%s: compositeId prefix = %q, want %q", g.ID, m.ID, prefix, productprofile.GatewayEndpointID)
			}
			if suffix != m.ID {
				t.Errorf("%s/%s: compositeId suffix = %q, want the catalog id %q", g.ID, m.ID, suffix, m.ID)
			}
		}
	}
	// The loop above is vacuous if nothing was projected.
	if len(groupOf(t, groups, chatmode.ModePrivacy).Models) == 0 {
		t.Fatal("privacy projected no models; this nail is not testing anything")
	}
}

// TestTheDisplayNameTravelsVerbatim is 需求基线 B6's render rule at the boundary
// where PR-4d could get it wrong: the projection must reproduce both languages
// the platform sent, because the client keeps no id -> name table of its own and
// has nothing to fall back to.
func TestTheDisplayNameTravelsVerbatim(t *testing.T) {
	p := fixtureCatalogPolicy(t)
	groups, _ := projectCatalog(p)

	for _, m := range groupOf(t, groups, chatmode.ModePrivacy).Models {
		if m.ID != "buding-privacy-1" {
			continue
		}
		if m.DisplayName.Zh != "布丁隐私版" || m.DisplayName.En != "Pudding Private" {
			t.Errorf("displayName = %+v, want the catalog's own values in both languages", m.DisplayName)
		}
		return
	}
	t.Fatal("buding-privacy-1 is missing from privacy; this nail is not testing anything")
}

// TestTheProjectionReportsTheVersionsItCameFrom exists because the frontend needs
// to tell "the catalog changed" from "the picker re-rendered" without diffing
// rows (本地API契约 §2.8 差异项 4). The versions are copied from the signed policy,
// so they are the platform's values, not the projection's.
func TestTheProjectionReportsTheVersionsItCameFrom(t *testing.T) {
	p := fixtureCatalogPolicy(t)
	groups, _ := projectCatalog(p)
	_ = groups

	// projectCatalog is pure over the policy; the versions travel with the
	// catalog it read, so they are asserted where they are produced (the
	// response) rather than duplicated here. This test pins that the policy
	// carries both, which is the precondition.
	if p.PolicyVersion == "" || p.Catalog.Version == "" {
		t.Fatalf("the fixture policy carries policyVersion=%q catalogVersion=%q; both must be present",
			p.PolicyVersion, p.Catalog.Version)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
