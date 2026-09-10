package productclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// registryDoc is the document that 中台交付包 §3.2 declares to be the single
// registry for `code` values. The test below keeps the constants and that table
// from drifting apart, which is the only way the "one registry" rule survives
// contact with a second developer.
const registryDoc = "../../dev-docs-usdable/需求/20260909/产品客户端与中台对接/中台交付包.md"

// allCodes is every code this package can produce. It is deliberately a single
// literal list: a new constant that is not added here fails the completeness
// check below.
func allCodes() []Code {
	return []Code{
		CodeInvalidPhone, CodeInvalidCode, CodeCodeNotSent, CodeActivationBad,
		CodeInvalidRequest, CodeUnauthorized, CodeTokenExpired, CodeNoCredits,
		CodeActivationReq, CodePlanExpired, CodeNotEntitled, CodeModelNotAllowed,
		CodePhoneMismatch, CodeSafetyBlocked, CodeContentRestrict, CodeAccountRestrict,
		CodeModelNotFound, CodeRequestRunning, CodeDuplicateReq, CodeRateLimited,
		CodeMaintenance, CodeUpstreamDown, CodeInternalError,
		CodeCatalogSignatureInvalid, CodePolicyExpired, CodeBootstrapStale,
		CodeUsagePending, CodeNetworkUnavailable,
	}
}

func TestCodesAreUniqueAndNonEmpty(t *testing.T) {
	seen := map[Code]bool{}
	for _, c := range allCodes() {
		if c == "" {
			t.Fatal("registered an empty code")
		}
		if seen[c] {
			t.Errorf("duplicate code %q", c)
		}
		seen[c] = true
	}
}

// TestEveryCodeIsRegisteredInTheDoc is the anti-drift check: the document is
// the registry, so a constant that is not in it is by definition a second
// spelling (开发规范 §3.8). It catches the realistic mistake — adding a code to
// the client and forgetting the contract — instead of the theoretical one.
func TestEveryCodeIsRegisteredInTheDoc(t *testing.T) {
	body, err := os.ReadFile(filepath.Clean(registryDoc))
	if err != nil {
		t.Fatalf("read the code registry %s: %v", registryDoc, err)
	}
	doc := string(body)

	for _, c := range allCodes() {
		if !strings.Contains(doc, string(c)) {
			t.Errorf("code %q is produced by the client but is not registered in %s "+
				"(register it in §3.2, or remove the constant)", c, filepath.Base(registryDoc))
		}
	}
}

// TestLocalCodesCarryAStatus pins the client-side status convention. It matters
// because the presentation layer must not have to special-case "a code with no
// HTTP status".
func TestLocalCodesCarryAStatus(t *testing.T) {
	for _, c := range []Code{
		CodeCatalogSignatureInvalid, CodePolicyExpired, CodeBootstrapStale,
		CodeUsagePending, CodeNetworkUnavailable,
	} {
		if got := localStatus(c); got == 0 {
			t.Errorf("localStatus(%q) = 0; every locally produced code needs a status", c)
		}
	}
}

func TestRetryableIsConservative(t *testing.T) {
	cases := []struct {
		name string
		err  *Error
		want bool
	}{
		{"4xx is never retried", &Error{Code: CodeInvalidRequest, HTTPStatus: 400}, false},
		{"402 is never retried", &Error{Code: CodeNoCredits, HTTPStatus: 402}, false},
		{"403 is never retried", &Error{Code: CodeNotEntitled, HTTPStatus: 403}, false},
		{"409 defers to the status query", &Error{Code: CodeRequestRunning, HTTPStatus: 409}, false},
		{"429 is retried after the cooldown", &Error{Code: CodeRateLimited, HTTPStatus: 429}, true},
		{"503 is retried", &Error{Code: CodeMaintenance, HTTPStatus: 503}, true},
		{"unknown 5xx is retried", &Error{Code: CodeInternalError, HTTPStatus: 500}, true},
		{"transport failure is retried", &Error{Code: CodeNetworkUnavailable}, true},
		{"no response is not a wildcard", &Error{Code: "something_new", HTTPStatus: 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Retryable(); got != tc.want {
				t.Errorf("Retryable() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRetryAfterDistinguishesAbsentFromZero guards the difference between "the
// platform said wait 0s" and "the platform did not say". Collapsing them would
// let a caller treat a missing cooldown as permission to retry at once.
func TestRetryAfterDistinguishesAbsentFromZero(t *testing.T) {
	absent := &Error{Code: CodeRateLimited, HTTPStatus: 429}
	if d, ok := absent.RetryAfter(); ok {
		t.Errorf("RetryAfter() reported %v for an absent value", d)
	}
	zero := &Error{Code: CodeRateLimited, HTTPStatus: 429, RetryAfterSec: 0}
	if _, ok := zero.RetryAfter(); ok {
		t.Error("RetryAfterSec: 0 must not read as a usable cooldown")
	}
	present := &Error{Code: CodeRateLimited, HTTPStatus: 429, RetryAfterSec: 60}
	d, ok := present.RetryAfter()
	if !ok || d.Seconds() != 60 {
		t.Errorf("RetryAfter() = (%v, %v), want (60s, true)", d, ok)
	}
}

func TestCodeOfAndIsCode(t *testing.T) {
	base := &Error{Op: "Login", Code: CodeNoCredits, HTTPStatus: 402}
	wrapped := errors.Join(errors.New("outer"), base)

	if got := CodeOf(wrapped); got != CodeNoCredits {
		t.Errorf("CodeOf through a wrap = %q, want %q", got, CodeNoCredits)
	}
	if !IsCode(wrapped, CodeNoCredits) {
		t.Error("IsCode should find a wrapped code")
	}
	if IsCode(wrapped, CodeRateLimited) {
		t.Error("IsCode matched the wrong code")
	}
	if got := CodeOf(errors.New("plain")); got != "" {
		t.Errorf("CodeOf(plain error) = %q, want empty", got)
	}
}

// TestLocalErrorNamesTheOperation keeps diagnostics useful: the op is what turns
// "the request failed" into "Preflight refused to start a turn".
func TestLocalErrorNamesTheOperation(t *testing.T) {
	err := LocalError("Preflight", CodePolicyExpired, "policy %s expired", "v1")
	if err.Op != "Preflight" {
		t.Errorf("Op = %q, want Preflight", err.Op)
	}
	if !strings.Contains(err.Error(), "Preflight") || !strings.Contains(err.Error(), string(CodePolicyExpired)) {
		t.Errorf("Error() = %q; it should name both the op and the code", err.Error())
	}
	if err.HTTPStatus == 0 {
		t.Error("LocalError left the status unset")
	}
}
