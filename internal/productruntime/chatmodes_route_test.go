package productruntime_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/open-octo/octo-agent/internal/chatmode"
	"github.com/open-octo/octo-agent/internal/productprofile"
)

// The projection's rules are pinned in chatmodes_test.go against a Policy value.
// This file drives the same road the window takes - real mux, real auth, real
// socket - because the deliverable L-C1b names is "the picker can read it", and
// 开发规范 §6.4.3 records what unit tests alone are worth here: PR-2b1 had every
// branch correct and the endpoints unreachable. V-24 is the same lesson from the
// other side, where the frontend called a route no handler had registered.

// chatModesBody is 本地API契约 §2.8's response, spelled out field by field rather
// than reused from the implementation: it is a Go/JS boundary shape, and a test
// that shares a struct with the code cannot catch a rename that breaks the
// browser. `fallback` is deliberately absent - its presence is asserted against.
type chatModesBody struct {
	Modes []struct {
		ID      string `json:"id"`
		Default string `json:"defaultModel"`
		Models  []struct {
			ID          string `json:"id"`
			CompositeID string `json:"compositeId"`
			DisplayName struct {
				Zh string `json:"zh"`
				En string `json:"en"`
			} `json:"displayName"`
		} `json:"models"`
	} `json:"modes"`
	CatalogVersion string `json:"catalogVersion"`
	PolicyVersion  string `json:"policyVersion"`
}

// TestChatModesIsRegisteredAndReachable is L-C1b's road: after a login the
// picker's data source answers, and it answers with the signed catalog's own
// contents rather than anything local.
func TestChatModesIsRegisteredAndReachable(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)

	status, raw := m.request(t, http.MethodGet, "/api/product/chat-modes", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/product/chat-modes = %d, want 200 (body: %.300s)", status, raw)
	}

	var body chatModesBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode chat-modes (%v): %.300s", err, raw)
	}

	// The three groups, in the product's order. Asserting the order as well as
	// the membership keeps a projection that iterates the catalog instead of the
	// mode set from passing.
	wantOrder := []string{chatmode.ModePrivacy, chatmode.ModeSmart, chatmode.ModeDefault}
	if len(body.Modes) != len(wantOrder) {
		t.Fatalf("modes = %d groups, want %d", len(body.Modes), len(wantOrder))
	}
	for i, want := range wantOrder {
		if body.Modes[i].ID != want {
			t.Errorf("modes[%d].id = %q, want %q", i, body.Modes[i].ID, want)
		}
	}

	// The names travel from the catalog (需求基线 B6). The fixture's values are
	// asserted rather than "non-empty": a projection that sent the id as the name
	// would satisfy non-empty, and that is precisely the local-table behaviour B6
	// forbids.
	var privacyNames []string
	for _, g := range body.Modes {
		for _, m := range g.Models {
			if m.ID == "buding-privacy-1" {
				privacyNames = append(privacyNames, m.DisplayName.Zh+"|"+m.DisplayName.En)
				if m.CompositeID != productprofile.GatewayEndpointID+"::buding-privacy-1" {
					t.Errorf("compositeId = %q, want the gateway prefix plus the catalog id", m.CompositeID)
				}
			}
		}
	}
	if len(privacyNames) == 0 {
		t.Fatal("buding-privacy-1 is missing from the response; the catalog did not reach the picker")
	}
	if privacyNames[0] != "布丁隐私版|Pudding Private" {
		t.Errorf("displayName = %q, want the catalog's values in both languages", privacyNames[0])
	}

	if body.CatalogVersion == "" || body.PolicyVersion == "" {
		t.Errorf("catalogVersion=%q policyVersion=%q, want both from the signed policy",
			body.CatalogVersion, body.PolicyVersion)
	}
}

// TestChatModesNeverFallsBackToALocalList is 需求基线 B1 规则 1 and 4 on the wire:
// with no catalog the picker is *empty*, and the response says nothing about a
// local list - the `fallback` field's whole job was to announce one, and its
// presence would be the built-in list coming back.
//
// The two assertions are a pair on purpose. "modes is empty" alone would also be
// satisfied by a response that carries a local list under another key; "no
// fallback field" alone would be satisfied by a response that lists models.
func TestChatModesNeverFallsBackToALocalList(t *testing.T) {
	// No login: the data root has no catalog.json at all, which is the state a
	// fresh installation is in (需求基线 E6.2 规则 2).
	m := newMountedHarness(t)
	if m.hasCatalog() {
		t.Fatal("the harness starts with a catalog; this test needs the no-cache state")
	}

	status, raw := m.request(t, http.MethodGet, "/api/product/chat-modes", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/product/chat-modes = %d, want 200 (body: %.300s)", status, raw)
	}

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("decode chat-modes (%v): %.300s", err, raw)
	}
	if _, present := generic["fallback"]; present {
		t.Error("the response carries a `fallback` field; that field announced the built-in list (本地API契约 §2.8 差异项 1)")
	}

	var body chatModesBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode chat-modes (%v): %.300s", err, raw)
	}
	if len(body.Modes) != 0 {
		t.Errorf("modes = %d groups with no catalog on disk, want none (fail-closed, 需求基线 B1 规则 1)",
			len(body.Modes))
	}
}
