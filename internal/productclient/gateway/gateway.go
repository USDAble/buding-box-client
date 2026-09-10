// Package gateway carries the only two things the central model gateway needs
// that the upstream provider stack does not already have: the per-call identity
// (`clientRequestId`) and the terminal-state control plane (cancel / status).
//
// It deliberately contains no SSE parser, no JSON-fragment reassembly, no token
// counting, no rate limiter and no HTTP retry loop. Every one of those already
// exists and is already tested:
//
//	SSE aggregation (incl. a missing [DONE])   internal/provider/openai
//	tool_call fragment reassembly by index      internal/provider/openai
//	usage normalisation incl. cache semantics   internal/provider/openai
//	any BaseURL + arbitrary request headers     internal/app (ProviderCustom)
//	client-side rate limiting                   config.Endpoint.RPM + buildClient
//	cancellation                                context cancellation
//
// scripts/reuse-guard.mjs fails CI if a `net/http` call or a retry/backoff
// policy reappears in this package. The trigger for that rule is not style: a
// second SSE parser would drift from the first, and the two would disagree about
// usage — which is the number the user is billed on. See P0-01
// §「gateway 的复用形态」and its 判据 (§3.5).
//
// The model stream itself is therefore assembled, not implemented, here:
//
//	app.SenderOptions{Provider: app.ProviderCustom, Protocol: "openai",
//	                  BaseURL: <gatewayHost from the embedded profile>,
//	                  APIKey:  <product access token>,
//	                  Headers: {"X-Client-Request-Id": id}}
//	s, _ := app.NewSender(opts)   // app is the only place provider clients are built
package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/productclient"
)

// CallMeta identifies exactly one gateway call.
//
// Granularity matters and is easy to get wrong: one user turn can produce
// several gateway calls (multi-round tool use), and each one is reserved and
// settled independently. So ClientRequestID is per *call*, not per turn or per
// conversation — a per-turn id would make two calls collide on one ledger
// reservation, and a per-conversation id would collide across every turn.
type CallMeta struct {
	// ClientRequestID is the idempotency key and the correlation id. It is
	// sent as both `Idempotency-Key` and `X-Client-Request-Id`, and it is
	// the key `RequestStatus` and the ledger are queried by.
	ClientRequestID string
	// ModelID is the catalog `id` value. It appears as `modelId` in the
	// gateway request body — same value, wire-level name (§4.3 note).
	ModelID string
	// PricingVersion is the catalog's pricing version at call time, kept so
	// a later ledger row can be explained without guessing.
	PricingVersion string
}

// ErrEmptyClientRequestID is returned by Validate. An empty id must stop the
// call before it reaches the wire: without it the platform cannot deduplicate a
// retry, so a network timeout would be indistinguishable from a second charge.
var ErrEmptyClientRequestID = errors.New("gateway: empty clientRequestId")

// Validate reports whether the call may be issued. It is intentionally strict
// about the identity field and lenient about the descriptive ones: a missing
// pricing version costs explainability, a missing request id costs money.
func (m CallMeta) Validate() error {
	if m.ClientRequestID == "" {
		return ErrEmptyClientRequestID
	}
	if m.ModelID == "" {
		return fmt.Errorf("gateway: empty modelId")
	}
	return nil
}

// ControlClient is the central platform's terminal-state control plane. It is
// the *only* part of the gateway interaction that is not already covered by the
// upstream provider stack, which is why it is the only interface here besides
// the observer.
//
// Note what is absent: there is no Consume, no Charge, no Settle. Settlement is
// the platform's; this client can cancel and ask, and that is all. An amount
// never crosses this boundary (中台交付包 §5.1).
type ControlClient interface {
	// Cancel asks the platform to stop the upstream call. It is idempotent
	// and safe to retry.
	Cancel(ctx context.Context, clientRequestID string) error
	// RequestStatus returns the authoritative state of a call. This is the
	// only permitted reaction to a broken stream — never a local re-charge,
	// refund or blind resend (中台交付包 §5.4).
	RequestStatus(ctx context.Context, clientRequestID string) (productclient.RequestStatus, error)
}

// TerminalSink receives the outcome of one observed call. The observer reports;
// it does not decide. Presenting "settled / reversed / reconciliation_pending"
// to the user, and updating the balance display, happens above this boundary.
type TerminalSink interface {
	// OnMeta is called once, before the upstream call is made, so the UI can
	// attach the id to its in-flight state.
	OnMeta(meta CallMeta)
	// OnTerminal is called at most once, with the platform's final word. A
	// call that ends without it must be resolved through ControlClient.
	OnTerminal(status productclient.RequestStatus)
}

// Observer decorates an upstream sender with per-call identity and terminal
// observation.
//
// # The capability-preservation rule
//
// Wrap must return a sender that still satisfies every capability interface its
// base satisfied. Returning a bare `agent.Sender` for a base that was a
// ToolStreamingSender is a silent regression, not a type error: the agent loop
// probes with type assertions (Sender → StreamingSender → ToolSender →
// ToolStreamingSender) and quietly falls back to the narrowest available
// behaviour. Streaming would stop streaming and tool calls would disappear, with
// nothing failing to compile and nothing logged. So the implementation is
// required to pick the wrapper that preserves the base's capabilities, and
// TestWrapPreservesCapabilities pins it.
type Observer interface {
	Wrap(base agent.Sender, meta CallMeta, sink TerminalSink) agent.Sender
}
