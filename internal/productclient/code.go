// Package productclient defines the desktop client's contract with the central
// platform: the narrow client interfaces, their DTOs, and the typed error
// surface. It deliberately depends on nothing from internal/server, the Web UI,
// or an agent session, and it holds no business authority — no prices, no credit
// arithmetic, no credential persistence, no local "developer" adapter.
//
// The method signatures live here; the field names, error envelope and `code`
// values are specified by 中台交付包 §3.2, which is the single registry. See
// P0-01 §合同骨架.
package productclient

import (
	"errors"
	"fmt"
	"time"
)

// Code is a machine-readable failure identifier. The values below are the
// client's copy of the registry in 中台交付包 §3.2; a second spelling for the
// same condition is a defect (开发规范 §3.8).
//
// Callers select user-visible copy from Code alone. Message is retained for
// logs and diagnostics only: the server may reword it at any time, and a
// client that branches on message text breaks silently the day it changes.
type Code string

// Server-issued codes. Every one of these is registered in 中台交付包 §3.2 with
// the HTTP status it travels with.
const (
	CodeInvalidPhone    Code = "invalid_phone"
	CodeInvalidCode     Code = "invalid_code"
	CodeCodeNotSent     Code = "code_not_sent"
	CodeActivationBad   Code = "activation_invalid"
	CodeInvalidRequest  Code = "invalid_request"
	CodeUnauthorized    Code = "unauthorized"
	CodeTokenExpired    Code = "token_expired"
	CodeNoCredits       Code = "insufficient_credits"
	CodeActivationReq   Code = "activation_required"
	CodePlanExpired     Code = "plan_expired"
	CodeNotEntitled     Code = "feature_not_entitled"
	CodeModelNotAllowed Code = "model_not_allowed"
	CodePhoneMismatch   Code = "phone_mismatch"
	CodeSafetyBlocked   Code = "safety_blocked"
	CodeContentRestrict Code = "content_restricted"
	CodeAccountRestrict Code = "account_restricted"
	CodeModelNotFound   Code = "model_not_found"
	CodeRequestRunning  Code = "request_in_progress"
	CodeDuplicateReq    Code = "duplicate_request"
	CodeRateLimited     Code = "rate_limited"
	CodeMaintenance     Code = "maintenance"
	CodeUpstreamDown    Code = "upstream_unavailable"
	CodeInternalError   Code = "internal_error"
)

// Codes produced locally by the client's own fail-closed checks. They never
// appear in a central-platform response, but they are registered in
// 中台交付包 §3.2 too, so the frontend mapping table has exactly one source.
const (
	// CodeCatalogSignatureInvalid means the catalog/policy envelope failed
	// verification or named an unknown keyId. Fail closed: treat it as "no
	// catalog", never as "verify later".
	CodeCatalogSignatureInvalid Code = "catalog_signature_invalid"
	// CodePolicyExpired means a verified envelope is past its expiresAt and
	// could not be refreshed. It blocks new turns but keeps the session
	// readable. The local clock is not trusted unconditionally: a client
	// clock outside the envelope's window is treated as this code too.
	CodePolicyExpired Code = "policy_expired"
	// CodeBootstrapStale means bootstrap data is older than the policy
	// window allows. Showing it is fine; acting on it is not.
	CodeBootstrapStale Code = "bootstrap_stale"
	// CodeUsagePending means a stream broke and settlement is undetermined.
	// The only permitted next step is RequestStatus — never a local
	// re-charge or refund (中台交付包 §5.4).
	CodeUsagePending Code = "usage_pending"
	// CodeNetworkUnavailable means the request never reached the platform.
	// Like the other local codes it is fail-closed: it never licenses a
	// fallback to a local model or to config.yml. It is distinct from
	// CodeUpstreamDown, which means the platform answered with a 5xx.
	CodeNetworkUnavailable Code = "network_unavailable"
)

// localStatus maps a locally produced code to the status a UI would otherwise
// have seen. It exists so the presentation layer has one code path; the value
// is a client-side convention, not something the platform sends.
func localStatus(code Code) int {
	switch code {
	case CodeNetworkUnavailable, CodeUpstreamDown, CodeMaintenance:
		return 503
	case CodeCatalogSignatureInvalid, CodePolicyExpired, CodeBootstrapStale:
		return 403
	case CodeUsagePending, CodeRequestRunning:
		return 409
	default:
		return 0
	}
}

// Error is the single failure type every client method returns. Transport and
// decode failures are wrapped, not flattened into a bare NotFound, so a caller
// can always distinguish "the platform said no" from "we never got an answer".
type Error struct {
	Code          Code
	HTTPStatus    int
	Message       string
	Field         string
	RetryAfterSec int
	RequestID     string

	// Op names the client method for diagnostics, e.g. "Login".
	Op string
	// Err carries the underlying transport/decode failure, if any.
	Err error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	op := e.Op
	if op == "" {
		op = "productclient"
	}
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (%s, field %s)", op, e.Code, e.Message, e.Field)
	}
	if e.Message != "" {
		return fmt.Sprintf("%s: %s (%s)", op, e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s", op, e.Code)
}

func (e *Error) Unwrap() error { return e.Err }

// RetryAfter reports the server-mandated cooldown. The boolean is false when
// the platform gave no usable value, so a caller cannot mistake a zero
// duration for "retry immediately" (中台交付包 §3.3).
func (e *Error) RetryAfter() (time.Duration, bool) {
	if e == nil || e.RetryAfterSec <= 0 {
		return 0, false
	}
	return time.Duration(e.RetryAfterSec) * time.Second, true
}

// Retryable reports whether an automatic bounded retry is permitted. It is
// deliberately conservative: 4xx never retries (the request is wrong, not
// unlucky), and a 5xx retry must still go through the "check the request state
// first" rule in 中台交付包 §3.3, so this is only a necessary condition.
func (e *Error) Retryable() bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case CodeNetworkUnavailable, CodeUpstreamDown, CodeMaintenance, CodeRateLimited:
		return true
	}
	return e.HTTPStatus >= 500
}

// CodeOf extracts the registry code from err, or "" when err is not a
// productclient error. An unrecognised server code is preserved verbatim rather
// than collapsed, because "unknown code" must stay distinguishable in
// diagnostics — but it is never treated as retryable.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// IsCode reports whether err carries any of codes.
func IsCode(err error, codes ...Code) bool {
	got := CodeOf(err)
	if got == "" {
		return false
	}
	for _, c := range codes {
		if got == c {
			return true
		}
	}
	return false
}

// LocalError builds a client-originated fail-closed failure. There is no
// HTTPStatus to invent, so it derives the conventional one, and it always sets
// Op so a log line names the method that refused to proceed.
func LocalError(op string, code Code, format string, args ...any) *Error {
	return &Error{
		Op:         op,
		Code:       code,
		HTTPStatus: localStatus(code),
		Message:    fmt.Sprintf(format, args...),
	}
}
