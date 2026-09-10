package productclient

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeEnvelopeSuccess(t *testing.T) {
	body := []byte(`{
		"data": {"cooldownSec": 60, "expiresInSec": 600},
		"requestId": "req_1",
		"serverTime": "2026-09-10T08:00:00Z"
	}`)

	var got Cooldown
	if err := DecodeEnvelope("SendCode", 200, body, &got); err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if got.CooldownSec != 60 || got.ExpiresInSec != 600 {
		t.Errorf("decoded %+v, want {60 600}", got)
	}
}

func TestDecodeEnvelopeDecodesNestedDTOs(t *testing.T) {
	body := []byte(`{
		"data": {
			"account": {"id": "acct_123", "phoneMasked": "138****1234", "nickname": "用户1234"},
			"plan": {"id": "trial", "status": "active", "expiresAt": null, "entitlements": ["cloud_chat"]},
			"credits": {"balanceMicroCredits": 2500000, "currency": "CREDIT", "updatedAt": "2026-09-10T08:00:00Z"}
		},
		"requestId": "req_2"
	}`)

	var got Bootstrap
	if err := DecodeEnvelope("Bootstrap", 200, body, &got); err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if got.Account.PhoneMasked != "138****1234" {
		t.Errorf("PhoneMasked = %q", got.Account.PhoneMasked)
	}
	if got.Credits.BalanceMicroCredits != 2500000 {
		t.Errorf("BalanceMicroCredits = %d", got.Credits.BalanceMicroCredits)
	}
	if got.Plan.ExpiresAt != nil {
		t.Error("a null expiresAt must stay nil; a zero timestamp would read as \"expired at year 1\"")
	}
	if !got.Plan.Allows("cloud_chat") || got.Plan.Allows("other") {
		t.Error("Plan.Allows must match exactly and deny absent entitlements")
	}
}

// TestDecodeEnvelopeErrorCarriesEveryField pins the field set. §3.2 declares
// exactly code / message / field / retryAfterSec / requestId; a client that
// only reads `code` throws away the cooldown and the audit id.
func TestDecodeEnvelopeErrorCarriesEveryField(t *testing.T) {
	body := []byte(`{
		"code": "rate_limited",
		"message": "too many requests",
		"field": "phone",
		"retryAfterSec": 60,
		"requestId": "req_3"
	}`)

	err := DecodeEnvelope("SendCode", 429, body, nil)
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if pe.Code != CodeRateLimited || pe.HTTPStatus != 429 || pe.Field != "phone" || pe.RetryAfterSec != 60 || pe.RequestID != "req_3" {
		t.Errorf("decoded %+v, want the full error envelope", pe)
	}
	if pe.Op != "SendCode" {
		t.Errorf("Op = %q; the decoder should record the calling method", pe.Op)
	}
}

// TestDecodeEnvelopeNonJSONErrorBody covers a broken proxy answering with HTML.
// The status is the fact; the code must be derived rather than left empty,
// because an empty code leaves the UI nothing to map.
func TestDecodeEnvelopeNonJSONErrorBody(t *testing.T) {
	err := DecodeEnvelope("Bootstrap", 502, []byte("<html>bad gateway</html>"), nil)
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if pe.Code != CodeUpstreamDown {
		t.Errorf("Code = %q, want %q", pe.Code, CodeUpstreamDown)
	}
	if pe.HTTPStatus != 502 {
		t.Errorf("HTTPStatus = %d, want 502", pe.HTTPStatus)
	}
}

// TestDecodeEnvelopeEmptyCodeFallsBackByStatus: a 200-shaped body with an
// unknown code and no code at all must not become "ok".
func TestDecodeEnvelopeEmptyCodeFallsBackByStatus(t *testing.T) {
	cases := []struct {
		status int
		want   Code
	}{
		{401, CodeUnauthorized},
		{402, CodeNoCredits},
		{409, CodeDuplicateReq},
		{503, CodeMaintenance},
		{500, CodeUpstreamDown},
		{418, CodeInternalError},
	}
	for _, tc := range cases {
		err := DecodeEnvelope("Op", tc.status, []byte(`{"message":"no code"}`), nil)
		var pe *Error
		if !errors.As(err, &pe) {
			t.Fatalf("status %d: expected *Error, got %T", tc.status, err)
		}
		if pe.Code != tc.want {
			t.Errorf("status %d: Code = %q, want %q", tc.status, pe.Code, tc.want)
		}
	}
}

// TestDecodeEnvelopeUnknownCodeIsPreserved: an unrecognised machine code stays
// verbatim so diagnostics can see it, and is not retried.
func TestDecodeEnvelopeUnknownCodeIsPreserved(t *testing.T) {
	err := DecodeEnvelope("Op", 400, []byte(`{"code":"brand_new_code"}`), nil)
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if pe.Code != "brand_new_code" {
		t.Errorf("Code = %q; an unknown code must be preserved, not collapsed", pe.Code)
	}
	if pe.Retryable() {
		t.Error("an unknown code must not be treated as retryable")
	}
}

// TestDecodeEnvelopeRejectsMissingData: treating "200 with no data" as an empty
// result would fabricate a state the platform never asserted — most damagingly
// a balance of zero.
func TestDecodeEnvelopeRejectsMissingData(t *testing.T) {
	var out Usage
	err := DecodeEnvelope("Usage", 200, []byte(`{"requestId":"req_4"}`), &out)
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if pe.Code != CodeInternalError {
		t.Errorf("Code = %q, want %q", pe.Code, CodeInternalError)
	}
	if pe.HTTPStatus != 200 {
		t.Errorf("HTTPStatus = %d, want the real status 200", pe.HTTPStatus)
	}
}

func TestDecodeEnvelopeRejectsMalformedData(t *testing.T) {
	err := DecodeEnvelope("Usage", 200, []byte(`{"data": {"balanceMicroCredits": "not a number"}}`), &Usage{})
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if !strings.Contains(pe.Error(), "malformed") {
		t.Errorf("Error() = %q; expected it to name the malformed payload", pe.Error())
	}
	if pe.Retryable() {
		t.Error("a malformed payload must not be retried")
	}
}

func TestDecodeEnvelopeNilOutSkipsData(t *testing.T) {
	// Logout has no payload to decode.
	if err := DecodeEnvelope("Logout", 200, []byte(`{"requestId":"req_5"}`), nil); err != nil {
		t.Errorf("DecodeEnvelope with a nil out: %v", err)
	}
}

// TestForgottenFieldsAreNotInvented is the JSON-surface equivalent of a
// compile-time check: a field renamed on the platform must show up as a zero
// value here, which is why every DTO field name is quoted from §3.2 verbatim.
func TestForgottenFieldsAreNotInvented(t *testing.T) {
	// A payload using a plausible-but-wrong name must not populate the DTO.
	body := []byte(`{"data": {"balance": 2500000, "currency": "CREDIT"}}`)
	var got Usage
	if err := DecodeEnvelope("Usage", 200, body, &got); err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if got.BalanceMicroCredits != 0 {
		t.Errorf("BalanceMicroCredits = %d; a renamed field must not silently populate", got.BalanceMicroCredits)
	}
}
