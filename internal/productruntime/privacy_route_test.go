package productruntime_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/pii"
	"github.com/open-octo/octo-agent/internal/productruntime"
)

func TestPrivacyRulesExposeTheBuiltinRegistryWithoutAnEngine(t *testing.T) {
	rt := productruntime.New(productruntime.Deps{})
	status, raw := requestRuntime(t, rt, http.MethodGet, "/api/product/privacy/rules", "")
	if status != http.StatusOK {
		t.Fatalf("rules status = %d, body = %s", status, raw)
	}
	var body struct {
		RuleVersion string   `json:"ruleVersion"`
		Rules       []string `json:"rules"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode rules response: %v", err)
	}
	if body.RuleVersion != pii.RuleVersion {
		t.Fatalf("ruleVersion = %q, want %q", body.RuleVersion, pii.RuleVersion)
	}
	if !reflect.DeepEqual(body.Rules, pii.RuleIDs()) {
		t.Fatalf("rules = %#v, want registered IDs %#v", body.Rules, pii.RuleIDs())
	}
}

func TestPrivacyTransformReturnsVersionedMaskedResult(t *testing.T) {
	m := newMountedHarness(t)
	status, raw := m.request(t, http.MethodPost, "/api/product/privacy/transform",
		map[string]any{"text": "电话 13800138000"}, nil)
	if status != http.StatusOK {
		t.Fatalf("transform status = %d", status)
	}
	var body struct {
		Hit         bool   `json:"hit"`
		Masked      string `json:"masked"`
		RuleVersion string `json:"ruleVersion"`
		Matches     []struct {
			Category string `json:"category"`
			Count    int    `json:"count"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode transform response: %v", err)
	}
	if !body.Hit || body.Masked != "电话 <手机号>" || body.RuleVersion != "builtin-1" {
		t.Fatal("transform response does not match the versioned masking contract")
	}
	if len(body.Matches) != 1 || body.Matches[0].Category != "cn_mobile" || body.Matches[0].Count != 1 {
		t.Fatalf("matches = %#v", body.Matches)
	}
	if strings.Contains(string(raw), "13800138000") {
		t.Fatal("transform response contains source text")
	}
}

func TestPrivacyTransformMissIsBytePreservingAndNoStore(t *testing.T) {
	m := newMountedHarness(t)
	const text = "普通文本 \t保留\n"
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, m.baseURL+"/api/product/privacy/transform", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("transform status = %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
		t.Fatalf("Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}
	var result struct {
		Hit     bool             `json:"hit"`
		Masked  string           `json:"masked"`
		Matches []map[string]any `json:"matches"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Hit || result.Masked != text || result.Matches == nil || len(result.Matches) != 0 {
		t.Fatal("a transform miss did not preserve text and an empty matches array")
	}
}

func TestPrivacyTransformFailsClosedWithoutEngine(t *testing.T) {
	rt := productruntime.New(productruntime.Deps{})
	status, body := requestRuntime(t, rt, http.MethodPost, "/api/product/privacy/transform", `{"text":"ordinary"}`)
	if status != http.StatusInternalServerError || !strings.Contains(body, `"code":"privacy_transform_failed"`) {
		t.Fatalf("missing-engine response = %d %s", status, body)
	}
}

func requestRuntime(t *testing.T, rt *productruntime.Runtime, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	rt.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}
