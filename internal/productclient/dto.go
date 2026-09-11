// Package productclient is the client half of the central platform contract:
// envelopes, error codes, and the account lifecycle calls.
//
// It is the single place where central-facing field names live (fork spec
// §3.8). The contract is a proposal until the platform confirms it - see
// dev-docs-usdable/需求/20260911/中台交付包.md - so keeping every name here means a
// contract change is a single-point edit.
//
// This package does not talk to model providers. The gateway is reached through
// internal/provider, which already parses the OpenAI protocol; see 需求基线 C3.
package productclient

import (
	"errors"
	"fmt"
)

// Envelope is the success shape of every response: the payload lives in data
// and is accompanied by a request id (中台交付包 §3.2).
type Envelope[T any] struct {
	Data       T      `json:"data"`
	RequestID  string `json:"requestId"`
	ServerTime string `json:"serverTime,omitempty"`
}

// Error is the failure envelope. Callers map UI from Code; Message is for
// humans only and must never be rendered (§3.2), so it is not translated here.
type Error struct {
	Code          string `json:"code"`
	Message       string `json:"message,omitempty"`
	Field         string `json:"field,omitempty"`
	RetryAfterSec int    `json:"retryAfterSec,omitempty"`
	RequestID     string `json:"requestId,omitempty"`

	// Status is the HTTP status that carried the envelope. It is how a caller
	// distinguishes a 401 that needs a refresh from a 403 that needs a person.
	Status int `json:"-"`
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("productclient: %s (http %d)", e.Code, e.Status)
	}
	return "productclient: " + e.Code
}

// ErrSessionExpired means the refresh token is gone or the platform refused it.
// The caller clears local credentials and returns to the blocked screen; it does
// not retry, because a rejected refresh token does not heal (需求基线 E12).
var ErrSessionExpired = errors.New("productclient: session expired")

// Business error codes. Values come from the platform's registry
// (中台交付包 §3.2) - add a new value there before using it here, so the
// spelling has one owner.
const (
	CodeInvalidPhone       = "invalid_phone"
	CodeInvalidCode        = "invalid_code"
	CodeCodeNotSent        = "code_not_sent"
	CodeActivationInvalid  = "activation_invalid"
	CodeActivationCodeUsed = "activation_code_used"
	CodeBoxCodeUnknown     = "box_code_unknown"
	CodeBoxCodeMismatch    = "box_code_mismatch"
	CodeInvalidRequest     = "invalid_request"

	CodeUnauthorized = "unauthorized"
	CodeTokenExpired = "token_expired"

	CodePhoneMismatch       = "phone_mismatch"
	CodeActivationRequired  = "activation_required"
	CodePlanExpired         = "plan_expired"
	CodeFeatureNotEntitled  = "feature_not_entitled"
	CodeModelNotAllowed     = "model_not_allowed"
	CodeInsufficientCredits = "insufficient_credits"
	CodeRateLimited         = "rate_limited"
	CodeMaintenance         = "maintenance"
	CodeUpstreamUnavailable = "upstream_unavailable"
	CodeInternalError       = "internal_error"
)

// CodeNetworkUnavailable is produced locally when the request never reached the
// platform. It is deliberately distinct from CodeUpstreamUnavailable, where the
// platform answered with a 5xx; neither is a reason to fall back to a local
// model source (§3.2).
const CodeNetworkUnavailable = "network_unavailable"

// Request headers every authenticated call carries (§3.1).
const (
	HeaderClientVersion  = "X-Client-Version"
	HeaderClientPlatform = "X-Client-Platform"
	HeaderClientArch     = "X-Client-Arch"
	HeaderInstallID      = "X-Install-Id"
	HeaderIdempotencyKey = "Idempotency-Key"
)

// Paths this package speaks to. The full registry is 中台交付包 §4.1.
const (
	pathSendSMS   = "/v1/auth/sms/send"
	pathLogin     = "/v1/auth/login"
	pathRefresh   = "/v1/auth/refresh"
	pathBootstrap = "/v1/client/bootstrap"
)

// PurposeLogin is the only code purpose this build requests.
const PurposeLogin = "login"

// SendSMSRequest asks for a login code (§4.2.1).
type SendSMSRequest struct {
	Phone           string `json:"phone"`
	Purpose         string `json:"purpose"`
	ClientRequestID string `json:"clientRequestId"`
}

// SendSMSData reports how long the user must wait and how long the code lives.
type SendSMSData struct {
	CooldownSec  int `json:"cooldownSec"`
	ExpiresInSec int `json:"expiresInSec"`
}

// LoginRequest covers both first activation and every later login (§4.2.2).
type LoginRequest struct {
	Phone    string `json:"phone"`
	Code     string `json:"code"`
	Nickname string `json:"nickname,omitempty"`

	// ActivationCode and BoxCode are required on first activation only, and are
	// omitted on later logins. They are independent checks: the code must exist
	// and be unused, and the box code must be the one that code was issued with.
	// The three activation failures must stay distinguishable in the UI, or a
	// user who mistypes one digit is stuck for good (需求基线 E1 规则 2).
	ActivationCode string `json:"activationCode,omitempty"`
	BoxCode        string `json:"boxCode,omitempty"`

	InstallID       string `json:"installId"`
	ClientRequestID string `json:"clientRequestId"`
}

// Account is the masked identity the platform returns.
type Account struct {
	ID          string `json:"id"`
	PhoneMasked string `json:"phoneMasked"`
	Nickname    string `json:"nickname"`
}

// Activation is server data, not local input. BoxCode is rendered verbatim and
// is never masked (需求基线 E5); older records may lack it, and the UI shows a
// dash rather than blocking on the missing field.
type Activation struct {
	Status      string `json:"status"`
	ActivatedAt string `json:"activatedAt"`
	ExpiresAt   string `json:"expiresAt"`
	BoxCode     string `json:"boxCode,omitempty"`
}

// LoginData is what a successful login hands back.
type LoginData struct {
	AccessToken             string      `json:"accessToken"`
	RefreshToken            string      `json:"refreshToken"`
	AccessTokenExpiresInSec int         `json:"accessTokenExpiresInSec"`
	Account                 Account     `json:"account"`
	Activation              *Activation `json:"activation,omitempty"`
}

// RefreshRequest carries the rotating refresh token (§4.2.3).
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// RefreshData is the rotated pair. The platform must rotate the refresh token;
// a refresh that returns the same one is a contract violation.
type RefreshData struct {
	AccessToken             string `json:"accessToken"`
	RefreshToken            string `json:"refreshToken"`
	AccessTokenExpiresInSec int    `json:"accessTokenExpiresInSec"`
}

// BootstrapData is the account summary gathered at startup.
//
// Catalog, capabilities and the signed policy envelope are deliberately NOT
// modelled here yet: they arrive with the signed-catalog work, which is a
// separate change. Bootstrap without them is a valid state, not an error - the
// model picker stays empty until the catalog lands (需求基线 B4).
type BootstrapData struct {
	Account    Account     `json:"account"`
	Activation *Activation `json:"activation,omitempty"`
	Balance    Balance     `json:"balance"`
}

// Balance is a read-only projection of the platform ledger. The client never
// computes credits locally (需求基线 E9).
type Balance struct {
	BalanceMicroCredits int64 `json:"balanceMicroCredits"`
}
