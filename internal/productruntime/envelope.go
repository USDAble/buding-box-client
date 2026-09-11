package productruntime

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/open-octo/octo-agent/internal/productclient"
)

// The three envelopes of 本地API契约 §1.2. They are mutually exclusive shapes and
// the frontend distinguishes them by presence, so they must never be mixed: a
// field-level failure carries fieldErrors and nothing else.
//
// No user-visible text is produced here. Wording lives in web/src/lib/i18n.ts,
// keyed by these machine codes (§3.8: one owner per fact).

// writeJSON writes v with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// writeFieldErrors reports a failure that belongs under an input box. The map is
// field name -> machine code, matching 本地API契约 §1.2.
func writeFieldErrors(w http.ResponseWriter, status int, fields map[string]string) {
	writeJSON(w, status, map[string]any{"fieldErrors": fields})
}

// writeCode reports a failure that cannot be attached to one input.
//
// extra carries the optional side-channel fields the contract allows, such as
// phoneMasked on phone_mismatch - it is what lets the UI say which number the
// directory is already bound to.
func writeCode(w http.ResponseWriter, status int, code string, extra map[string]any) {
	body := map[string]any{"code": code}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, status, body)
}

// writeProductGate reports an unauthenticated call to a gated route. The frontend
// turns this into phase=blocked, deliberately not silently (§1.2).
func writeProductGate(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]any{"error": "product_gate"})
}

// writePlatformError maps a platform failure onto a local envelope.
//
// The mapping is the whole reason this package exists, and one rule in it is
// counter-intuitive: the platform's own "field" is NOT what decides the local
// envelope. Which one the UI receives is a local contract decision (本地API契约 §3),
// and the two differ. The platform marks activation_invalid with a field; the
// local contract classifies it as business-level, so it goes to the banner.
//
// The reason the level cannot be read off the code name either: invalid_code is
// field-level as a format check and business-level as a wrong-or-expired answer,
// and invalid_phone likewise differs between login and send-code. The format cases
// are caught by local validation before a request leaves, so a code arriving from
// the platform is by definition the business case, except for the handful below.
func writePlatformError(w http.ResponseWriter, err error) {
	var pe *productclient.Error
	if !errors.As(err, &pe) {
		// Not a platform envelope at all: a transport failure or a local fault.
		// 5xx so the UI treats it as an outage rather than as a rejected input.
		writeCode(w, http.StatusServiceUnavailable, productclient.CodeNetworkUnavailable, nil)
		return
	}

	if field, ok := fieldLevelCodes[pe.Code]; ok {
		writeFieldErrors(w, http.StatusBadRequest, map[string]string{field: pe.Code})
		return
	}

	extra := map[string]any{}
	if pe.RetryAfterSec > 0 {
		extra["retryAfterSec"] = pe.RetryAfterSec
	}
	if pe.PhoneMasked != "" {
		extra["phoneMasked"] = pe.PhoneMasked
	}
	writeCode(w, statusForCode(pe), pe.Code, extra)
}

// fieldLevelCodes are the platform codes that belong under an input box, mapped to
// the local field name they attach to. Anything absent is business-level, which is
// the safe default: a banner the user can read beats a message silently dropped
// onto the wrong input.
//
// 本地API契约 §3 is the owner of this classification; this table is its
// implementation, so the two change together.
var fieldLevelCodes = map[string]string{
	productclient.CodeInvalidCode: "code",
	"nickname_format":             "nickname",
	"nickname_sensitive":          "nickname",
}

// statusForCode picks the local status for a business-level platform code.
func statusForCode(pe *productclient.Error) int {
	switch {
	case pe.Status == 0:
		// No HTTP response at all: the request never reached the platform. This
		// is the shape productclient uses for a transport failure, and it must not
		// be reported as a rejected input.
		return http.StatusServiceUnavailable
	case pe.Status >= 500:
		// An upstream fault is ours to report, not the user's to fix.
		return http.StatusServiceUnavailable
	case pe.Status == http.StatusTooManyRequests:
		return http.StatusTooManyRequests
	case pe.Status == http.StatusForbidden:
		return http.StatusForbidden
	case pe.Status == http.StatusConflict:
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

// asProductError is errors.As specialised to the platform envelope. It exists so
// the mapping above reads as one rule rather than a nested type assertion.
func asProductError(err error, target **productclient.Error) bool {
	return errors.As(err, target)
}

// writeControlPlaneUnconfigured reports that this build has no platform host set.
//
// This is an expected state in a developer build before a Sandbox is configured,
// and an incident in production (需求基线 A1 规则 6). The page wording for the two
// cases is still open as S-6/S-7; the code exists so the two are at least
// distinguishable from an outage in the meantime.
func writeControlPlaneUnconfigured(w http.ResponseWriter) {
	writeCode(w, http.StatusServiceUnavailable, codeControlPlaneUnconfigured, nil)
}

// codeControlPlaneUnconfigured is registered in 本地API契约 §3.
const codeControlPlaneUnconfigured = "control_plane_unconfigured"
