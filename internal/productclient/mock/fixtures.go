//go:build !product_production

package mock

import (
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
)

// FixedNow is the reference instant every fixture is built around. Fixtures take
// the clock as input rather than calling time.Now() so a contract test is
// deterministic and does not start failing at midnight.
var FixedNow = time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// ModelID is the model id used by the fixtures. It is an illustrative ASCII
// data key, not a brand string, and it matches the examples in 中台交付包 §4.3.
const ModelID = "buding-cloud-pro"

// Catalog returns a catalog with one eligible gateway model in two modes.
func Catalog(at time.Time) productclient.Catalog {
	return productclient.Catalog{
		Version: "2026-09-10.1",
		TTLSec:  3600,
		Models: []productclient.Model{{
			ID:               ModelID,
			DisplayName:      "布丁专业版",
			ModeIDs:          []string{"smart", "default"},
			Transport:        productclient.TransportGateway,
			Capabilities:     productclient.ModelCapabilities{Vision: true, Tools: true, Stream: true},
			MaxContextTokens: 128000,
			MaxOutputTokens:  8192,
			Eligible:         true,
			Default:          true,
			PricingVersion:   "2026-09-a",
		}},
		Modes: []productclient.Mode{
			{ID: "smart", DefaultModel: ModelID},
			{ID: "default", DefaultModel: ModelID},
		},
	}
}

// Capabilities returns a small matrix that exercises all three policy values
// and both "present but not granted" states.
func Capabilities() []productclient.Capability {
	return []productclient.Capability{
		{ID: "mcp", Available: true, Visible: false, Entitled: false,
			PermissionPolicy: productclient.PermissionAsk, MinClientVersion: "1.2.0"},
		{ID: "terminal", Available: true, Visible: true, Entitled: true,
			PermissionPolicy: productclient.PermissionAsk, MinClientVersion: "1.2.0"},
		{ID: "local_read", Available: true, Visible: true, Entitled: true,
			PermissionPolicy: productclient.PermissionAllow, MinClientVersion: "1.2.0"},
		{ID: "install_extension", Available: true, Visible: true, Entitled: true,
			PermissionPolicy: productclient.PermissionDeny, MinClientVersion: "1.2.0"},
		{ID: "absent_feature", Available: false, Visible: false, Entitled: true,
			PermissionPolicy: productclient.PermissionAllow, MinClientVersion: "1.2.0"},
	}
}

// Policy returns a policy envelope valid at `at`: issued an hour earlier and
// expiring an hour later. The signature is a placeholder — this is a contract
// sample, not something a Verifier accepts.
func Policy(at time.Time) productclient.PolicyEnvelope {
	return productclient.PolicyEnvelope{
		PolicyVersion:    "2026-09-10.1",
		IssuedAt:         rfc3339(at.Add(-time.Hour)),
		ExpiresAt:        rfc3339(at.Add(time.Hour)),
		MinClientVersion: "1.2.0",
		Audience:         "puddingbox",
		KeyID:            "policy-2026-a",
		Catalog:          Catalog(at),
		Capabilities:     Capabilities(),
		Signature:        "mock-signature-not-verifiable",
	}
}

// ExpiredPolicy returns the same envelope with a window that ended an hour ago.
// The signature is still "valid" — expiry is a separate check from integrity,
// which is why both exist (P0-04 §过期语义).
func ExpiredPolicy(at time.Time) productclient.PolicyEnvelope {
	p := Policy(at)
	p.PolicyVersion = "2026-09-10.0"
	p.IssuedAt = rfc3339(at.Add(-3 * time.Hour))
	p.ExpiresAt = rfc3339(at.Add(-time.Hour))
	return p
}

// Bootstrap returns a healthy cold-start payload valid at `at`.
func Bootstrap(at time.Time) productclient.Bootstrap {
	return productclient.Bootstrap{
		Account:    productclient.Account{ID: "acct_123", PhoneMasked: "138****1234", Nickname: "用户1234"},
		Activation: productclient.Activation{Status: productclient.ActivationActive, ExpiresAt: rfc3339(at.AddDate(1, 0, 0))},
		Plan: productclient.Plan{
			ID:           "trial",
			Status:       "active",
			Entitlements: []string{"cloud_chat"},
		},
		Credits: productclient.Credits{
			BalanceMicroCredits: 2_500_000,
			Currency:            "CREDIT",
			UpdatedAt:           rfc3339(at),
		},
		Policy:              Policy(at),
		SensitiveDictionary: productclient.DictionaryRef{Version: "42", ETag: `W/"42"`},
		Legal: productclient.LegalSummary{Documents: []productclient.LegalDocument{{
			Type:               "privacy",
			Version:            "2026-09-10",
			EffectiveAt:        rfc3339(at),
			RequiresAcceptance: false,
		}}},
	}
}

// ExpiredBootstrap returns a payload whose policy window has closed. Paired with
// a cache that cannot be refreshed, this is the "no new turns" state.
func ExpiredBootstrap(at time.Time) productclient.Bootstrap {
	b := Bootstrap(at)
	b.Policy = ExpiredPolicy(at)
	return b
}

// LoginResult returns a successful login with a fresh token pair.
func LoginResult(at time.Time) productclient.LoginResult {
	return productclient.LoginResult{
		Session: productclient.Session{
			AccessToken:           "mock-access-token",
			RefreshToken:          "mock-refresh-token",
			AccessTokenExpiresSec: 7200,
		},
		Account:    productclient.Account{ID: "acct_123", PhoneMasked: "138****1234", Nickname: "用户1234"},
		Activation: productclient.Activation{Status: productclient.ActivationActive, ActivatedAt: rfc3339(at)},
	}
}

// The five failures B0's acceptance list names, as server-shaped errors. They
// are typed *productclient.Error so callers exercise the real code path rather
// than a string match.

// Unauthorized is the 401 the single-flight refresh exists to handle.
func Unauthorized(op string) *productclient.Error {
	return &productclient.Error{
		Op: op, Code: productclient.CodeTokenExpired, HTTPStatus: 401,
		Message: "token expired", RequestID: "req_mock_401",
	}
}

// InsufficientCredits is the 402 the gateway returns *before* calling a
// supplier. The client must show it and must not fall back to a local model.
func InsufficientCredits(op string) *productclient.Error {
	return &productclient.Error{
		Op: op, Code: productclient.CodeNoCredits, HTTPStatus: 402,
		Message: "balance exhausted", Field: "modelId", RequestID: "req_mock_402",
	}
}

// RateLimited exercises the retryAfterSec cooldown.
func RateLimited(op string, retryAfterSec int) *productclient.Error {
	return &productclient.Error{
		Op: op, Code: productclient.CodeRateLimited, HTTPStatus: 429,
		Message: "slow down", RetryAfterSec: retryAfterSec, RequestID: "req_mock_429",
	}
}

// Maintenance exercises the reviewer-facing 503 that must not be retried
// aggressively and must not be reworded from a web page.
func Maintenance(op string, retryAfterSec int) *productclient.Error {
	return &productclient.Error{
		Op: op, Code: productclient.CodeMaintenance, HTTPStatus: 503,
		Message: "maintenance", RetryAfterSec: retryAfterSec, RequestID: "req_mock_503",
	}
}
