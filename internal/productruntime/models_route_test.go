package productruntime_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/open-octo/octo-agent/internal/productprofile"
)

type modelsBody struct {
	State          string `json:"state"`
	CatalogVersion string `json:"catalogVersion"`
	Vendors        []struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Models      []struct {
			ID                   string `json:"id"`
			DisplayName          string `json:"displayName"`
			CompositeID          string `json:"compositeId"`
			Confidential         bool   `json:"confidential"`
			ConfidentialPriority *int   `json:"confidentialPriority"`
		} `json:"models"`
	} `json:"vendors"`
}

func TestModelsIsRegisteredAndProjectsTheSignedCatalog(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)
	status, raw := m.request(t, http.MethodGet, "/api/product/models", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/product/models = %d (body: %.300s)", status, raw)
	}
	var body modelsBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if body.State != "ready" || body.CatalogVersion == "" || len(body.Vendors) != 2 {
		t.Fatalf("models response = %+v", body)
	}
	private := body.Vendors[0].Models[0]
	if private.ID != "buding-privacy-1" || private.CompositeID != productprofile.GatewayEndpointID+"::buding-privacy-1" {
		t.Fatalf("private model = %+v", private)
	}
	if !private.Confidential || private.ConfidentialPriority == nil || *private.ConfidentialPriority != 100 {
		t.Fatalf("private metadata = %+v", private)
	}
}

func TestModelsWithoutCatalogReturnsExplicitStateAndNoFallback(t *testing.T) {
	m := newMountedHarness(t)
	status, raw := m.request(t, http.MethodGet, "/api/product/models", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/product/models = %d", status)
	}
	var body modelsBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if body.State != "absent" || len(body.Vendors) != 0 {
		t.Fatalf("models response = %+v, want absent and empty", body)
	}
}

func TestReasoningRouteRejectsUndeclaredChoicesForUnknownModel(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)
	status, raw := m.request(t, http.MethodPut, "/api/product/model-reasoning", map[string]any{"modelId": "buding-privacy-1", "effort": "max"}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("undeclared max accepted: %d %s", status, raw)
	}
	status, raw = m.request(t, http.MethodPut, "/api/product/model-reasoning", map[string]any{"modelId": "buding-privacy-1", "effort": "default"}, nil)
	if status != http.StatusOK {
		t.Fatalf("default refused: %d %s", status, raw)
	}
	status, raw = m.request(t, http.MethodGet, "/api/product/models", nil, nil)
	var body struct {
		Vendors []struct {
			Models []struct {
				ReasoningOptions []string `json:"reasoningOptions"`
				ReasoningEffort  string   `json:"reasoningEffort"`
			} `json:"models"`
		} `json:"vendors"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	row := body.Vendors[0].Models[0]
	if status != 200 || len(row.ReasoningOptions) != 1 || row.ReasoningOptions[0] != "default" || row.ReasoningEffort != "default" {
		t.Fatalf("unknown model options %s", raw)
	}
}
