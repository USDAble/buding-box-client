package productclient

import (
	"encoding/json"
	"fmt"
)

// Envelope is the success shape shared by every JSON endpoint
// (中台交付包 §3.2). Data is the endpoint's own DTO, left raw here so the
// envelope stays one type instead of one per endpoint.
type Envelope struct {
	Data       json.RawMessage `json:"data"`
	RequestID  string          `json:"requestId"`
	ServerTime string          `json:"serverTime"`
}

// ErrorEnvelope is the failure shape. The field names are fixed by
// 中台交付包 §3.2 — there is no `messageKey` or `traceId`, and adding one here
// would create the second spelling that §3.8 forbids.
type ErrorEnvelope struct {
	Code          Code   `json:"code"`
	Message       string `json:"message"`
	Field         string `json:"field"`
	RetryAfterSec int    `json:"retryAfterSec"`
	RequestID     string `json:"requestId"`
}

// statusFallbackCode maps an HTTP status to a registered code when the body
// carried no usable one — a proxy error page, a truncated response, or a
// platform bug. Guessing is safe here only because every value chosen is
// fail-closed and non-retryable beyond the bounded 5xx rule; the alternative
// (returning a code-less error) would leave the UI with nothing to map.
func statusFallbackCode(status int) Code {
	switch status {
	case 400, 422:
		return CodeInvalidRequest
	case 401:
		return CodeUnauthorized
	case 402:
		return CodeNoCredits
	case 403:
		return CodeNotEntitled
	case 404:
		return CodeModelNotFound
	case 409:
		return CodeDuplicateReq
	case 429:
		return CodeRateLimited
	case 503:
		return CodeMaintenance
	default:
		if status >= 500 {
			return CodeUpstreamDown
		}
		return CodeInternalError
	}
}

// DecodeEnvelope parses one endpoint response into out.
//
// It is the only place a wire response becomes a *Error, so the error-mapping
// rules live in exactly one place instead of being re-derived per method
// (P0-01 §「中台交付包 §2」). op names the calling method for diagnostics.
func DecodeEnvelope(op string, status int, body []byte, out any) error {
	if status >= 400 {
		var ee ErrorEnvelope
		// A non-JSON error body is expected under broken proxies, so the
		// decode failure is not itself reported — the status is the fact.
		_ = json.Unmarshal(body, &ee)
		code := ee.Code
		if code == "" {
			code = statusFallbackCode(status)
		}
		return &Error{
			Op:            op,
			Code:          code,
			HTTPStatus:    status,
			Message:       ee.Message,
			Field:         ee.Field,
			RetryAfterSec: ee.RetryAfterSec,
			RequestID:     ee.RequestID,
		}
	}

	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return &Error{
			Op:         op,
			Code:       CodeInternalError,
			HTTPStatus: status,
			Message:    "malformed success response",
			Err:        err,
		}
	}
	if out == nil {
		return nil
	}
	if len(env.Data) == 0 {
		// A 200 with no `data` is a protocol violation, not an empty
		// result: treating it as zero values would silently fabricate a
		// state (e.g. "balance 0") that the platform never asserted.
		return &Error{
			Op:         op,
			Code:       CodeInternalError,
			HTTPStatus: status,
			Message:    "success response has no data object",
		}
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return &Error{
			Op:         op,
			Code:       CodeInternalError,
			HTTPStatus: status,
			Message:    fmt.Sprintf("malformed data object for %T", out),
			Err:        err,
		}
	}
	return nil
}
