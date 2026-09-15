// OCTO-FORK: L-D5 signed server dictionary integration nails — see dev-docs-usdable/需求/20260911/需求基线.md D5.
package productruntime_test

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

func loginAgain(t *testing.T, h *harness) map[string]any {
	t.Helper()
	if status, body := h.sendCode("13800001234"); status != http.StatusOK {
		t.Fatalf("send code = %d (%v)", status, body)
	}
	status, body := h.login(map[string]any{
		"phone": "13800001234", "code": clienttest.FixtureSMSCode, "nickname": "tester",
	})
	if status != http.StatusOK {
		t.Fatalf("login = %d (%v)", status, body)
	}
	return body
}

func dictionaryNotice(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	notice, ok := body["dictionaryNotice"].(map[string]any)
	if !ok {
		t.Fatalf("dictionaryNotice missing: %v", body)
	}
	return notice
}

func TestLoginAcceptsSignedServerDictionaryAndActivatesIt(t *testing.T) {
	h := newHarness(t)
	h.platform.SetDictionary("43", []string{"server-secret"})
	h.activate()

	entry, err := h.serverDict.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if entry.Version != "43" || entry.KeyID != clienttest.FixtureSigningKeyID || len(entry.Words) != 1 {
		t.Fatalf("entry = %#v", entry)
	}
	if got := h.engine.Filter("a server-secret value").Text; got != "a *** value" {
		t.Fatalf("filtered = %q", got)
	}
	if h.platform.DictionaryReads() != 1 {
		t.Fatalf("dictionary reads = %d, want 1", h.platform.DictionaryReads())
	}
}

func TestDictionaryRefusalsKeepTheOldCache(t *testing.T) {
	tests := []struct {
		name string
		arm  func(*clienttest.Server)
	}{
		{"tampered signature", func(s *clienttest.Server) { s.SetDictionary("44", []string{"replacement"}); s.TamperDictionary() }},
		{"unknown key", func(s *clienttest.Server) {
			s.SetDictionary("44", []string{"replacement"})
			s.SetDictionaryKeyID("unknown-key")
		}},
		{"version rollback", func(s *clienttest.Server) { s.SetDictionary("42", []string{"older"}) }},
		{"offline", func(s *clienttest.Server) {
			s.FailDictionary(http.StatusServiceUnavailable, productclient.CodeUpstreamUnavailable)
		}},
		{"malformed word", func(s *clienttest.Server) { s.SetDictionary("44", []string{"!!!"}) }},
		{"wrong checksum", func(s *clienttest.Server) {
			s.SetDictionary("44", []string{"replacement"})
			s.SetDictionaryDigest("wrong")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.platform.SetDictionary("43", []string{"accepted"})
			h.activate()
			tt.arm(h.platform)
			body := loginAgain(t, h)

			notice := dictionaryNotice(t, body)
			if notice["state"] != "degraded" || notice["fallbackVersion"] != "43" || notice["retryAt"] != "next_login" {
				t.Fatalf("notice = %v", notice)
			}
			entry, err := h.serverDict.Load()
			if err != nil || entry.Version != "43" || len(entry.Words) != 1 || entry.Words[0] != "accepted" {
				t.Fatalf("preserved entry = %#v, %v", entry, err)
			}
		})
	}
}

func TestDictionaryWriteFailureKeepsTheOldCacheInMemory(t *testing.T) {
	h := newHarness(t)
	h.platform.SetDictionary("43", []string{"accepted"})
	h.activate()
	path := filepath.Join(h.root, sensitive.ServerDictFileName)
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove temp cache: %v", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("replace temp cache with directory: %v", err)
	}
	h.platform.SetDictionary("44", []string{"replacement"})

	notice := dictionaryNotice(t, loginAgain(t, h))
	if notice["fallbackVersion"] != "43" {
		t.Fatalf("notice = %v", notice)
	}
	entry, err := h.serverDict.Load()
	if err != nil || entry.Version != "43" || entry.Words[0] != "accepted" {
		t.Fatalf("in-memory entry = %#v, %v", entry, err)
	}
}

func TestDictionaryFailureWithoutCacheNamesBuiltinFallback(t *testing.T) {
	h := newHarness(t)
	h.platform.TamperDictionary()
	body := map[string]any{}
	if status, _ := h.sendCode("13800001234"); status != http.StatusOK {
		t.Fatalf("send code = %d", status)
	}
	status, body := h.login(map[string]any{
		"phone": "13800001234", "code": clienttest.FixtureSMSCode, "nickname": "tester",
		"activationCode": clienttest.FixtureActivationCode, "boxCode": clienttest.FixtureBoxCode,
	})
	if status != http.StatusOK {
		t.Fatalf("login = %d (%v)", status, body)
	}
	notice := dictionaryNotice(t, body)
	if notice["fallbackVersion"] != "" || notice["retryAt"] != "next_login" {
		t.Fatalf("notice = %v", notice)
	}
	if _, err := h.serverDict.Load(); !errors.Is(err, sensitive.ErrNoServerDictionary) {
		t.Fatalf("Load error = %v", err)
	}
}

func TestDictionaryReportsRecoveryAfterAReportedFailure(t *testing.T) {
	h := newHarness(t)
	h.platform.SetDictionary("43", []string{"accepted"})
	h.activate()
	h.platform.FailDictionary(http.StatusServiceUnavailable, productclient.CodeUpstreamUnavailable)
	_ = dictionaryNotice(t, loginAgain(t, h))

	h.platform.FailDictionary(0, "")
	h.platform.SetDictionary("44", []string{"replacement"})
	notice := dictionaryNotice(t, loginAgain(t, h))
	if notice["state"] != "recovered" || notice["version"] != "44" {
		t.Fatalf("notice = %v", notice)
	}
}
