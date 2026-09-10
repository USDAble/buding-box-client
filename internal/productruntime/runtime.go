// Package productruntime is the composition root for the product side of the
// desktop app. It holds the narrow clients and answers the questions that must
// be answered before a turn is allowed to start.
//
// It is a *composition* root, not a second application layer: it calls
// productclient / gateway / productpolicy / credentialstore and maps their
// results, and it deliberately owns no HTTP detail, no URL assembly, no header,
// no signature verification and no price arithmetic (开发计划 §4). Anything in
// here that reaches for net/http or builds a URL is in the wrong package — the
// transport lives inside productclient, verified once, and the model stream is
// assembled from internal/app rather than implemented here.
//
// It does not import internal/server and it must never be wired into it: the
// desktop constructs this directly, which is what keeps product logic out of the
// upstream runtime (P0-01A).
package productruntime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/gateway"
	"github.com/open-octo/octo-agent/internal/productpolicy"
)

// Deps are the collaborators the desktop supplies. Every field is required:
// an optional narrow client would mean a code path where "the platform was not
// consulted" is normal, and that is the shape every fail-closed rule in P0
// exists to prevent.
type Deps struct {
	Auth         productclient.AuthClient
	ControlPlane productclient.ControlPlaneClient
	Usage        productclient.UsageClient
	Sessions     productclient.SessionLister
	// Gateway is the terminal-state control plane (cancel / status). It is
	// not the model stream, which is assembled from internal/app.
	Gateway gateway.ControlClient
	// Observer adds per-call identity and terminal observation to an
	// upstream sender.
	Observer gateway.Observer
	// Credentials persists the rotating refresh token.
	Credentials credentialstore.Store
}

// ErrMissingDeps reports an incomplete composition. It lists every missing
// field at once so a first run does not fail one dependency at a time.
type ErrMissingDeps struct{ Fields []string }

func (e *ErrMissingDeps) Error() string {
	return "productruntime: missing required dependencies: " + strings.Join(e.Fields, ", ")
}

// Runtime is the composed product runtime. It is safe for concurrent use: the
// only mutable state is the verified policy, guarded by setPolicy being called
// from a single refresh path.
type Runtime struct {
	deps Deps

	policy *productpolicy.Policy
}

// New validates the composition and returns a runtime with no policy yet.
//
// Starting without a policy is correct, not degraded: it means "bootstrap has
// not succeeded", which Preflight reports as a stale bootstrap rather than
// pretending a default policy exists.
func New(deps Deps) (*Runtime, error) {
	var missing []string
	if deps.Auth == nil {
		missing = append(missing, "Auth")
	}
	if deps.ControlPlane == nil {
		missing = append(missing, "ControlPlane")
	}
	if deps.Usage == nil {
		missing = append(missing, "Usage")
	}
	if deps.Sessions == nil {
		missing = append(missing, "Sessions")
	}
	if deps.Gateway == nil {
		missing = append(missing, "Gateway")
	}
	if deps.Observer == nil {
		missing = append(missing, "Observer")
	}
	if deps.Credentials == nil {
		missing = append(missing, "Credentials")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, &ErrMissingDeps{Fields: missing}
	}
	return &Runtime{deps: deps}, nil
}

// SetPolicy installs a freshly verified policy.
//
// It enforces monotonicity against the policy already held, so a correctly
// signed but *older* envelope cannot be replayed to re-grant a capability the
// platform withdrew. The first policy installed is always accepted: there is
// nothing to go backwards from, and refusing it would make a fresh install
// permanently unusable.
func (r *Runtime) SetPolicy(p *productpolicy.Policy) error {
	if p == nil {
		return errors.New("productruntime: nil policy")
	}
	if r.policy != nil {
		if err := p.AcceptVersion(r.policy.Version()); err != nil {
			return err
		}
	}
	r.policy = p
	return nil
}

// Policy returns the current verified policy, or nil when bootstrap has not
// succeeded.
func (r *Runtime) Policy() *productpolicy.Policy { return r.policy }

// PolicyVersion returns the version of the policy in force, for turn auditing.
// It returns "" when there is none, so a caller cannot mistake it for a version.
func (r *Runtime) PolicyVersion() string {
	if r.policy == nil {
		return ""
	}
	return r.policy.Version()
}

// Evaluate runs one policy-enforcement-point question. With no policy in force
// the answer is Deny, not Ask: there is no signed basis for a prompt, and
// prompting would imply the action might be permitted.
func (r *Runtime) Evaluate(req productpolicy.Request, now time.Time) productpolicy.Verdict {
	if r.policy == nil {
		return productpolicy.Verdict{
			Decision: productpolicy.Deny,
			Reason:   "no verified policy in force",
		}
	}
	return r.policy.Evaluate(req, now)
}

// Preflight is the gate in front of every new turn that would call the model
// gateway. It answers one question — may we start a turn with this model — and
// every failure is a registered, fail-closed code:
//
//	catalog_signature_invalid  the policy in force could not be trusted
//	bootstrap_stale            bootstrap has not succeeded, or the catalog is empty
//	policy_expired             the signed window has closed (or the clock is outside it)
//	model_not_found            the model is absent from the catalog
//	model_not_allowed          the model is present but not usable
//
// It does not fall back. There is no branch here that selects a local model,
// reads config.yml, or uses "the last model that worked": all three are the R4
// violation that P0-04 and P0-07 exist to prevent.
func (r *Runtime) Preflight(modelID string, now time.Time) error {
	if r.policy == nil {
		return productclient.LocalError("Preflight", productclient.CodeBootstrapStale,
			"no verified policy: bootstrap has not completed")
	}
	if r.policy.NotYetValid(now) {
		// A clock behind issuing time is reported separately from expiry
		// because the remedy differs (fix the clock vs. refresh), but it is
		// equally blocking.
		return productclient.LocalError("Preflight", productclient.CodePolicyExpired,
			"policy is not yet valid at the current clock (%s)", now.UTC().Format(time.RFC3339))
	}
	if r.policy.Expired(now) {
		return productclient.LocalError("Preflight", productclient.CodePolicyExpired,
			"policy %s expired; refusing to start a turn", r.policy.Version())
	}
	if strings.TrimSpace(modelID) == "" {
		return productclient.LocalError("Preflight", productclient.CodeModelNotFound,
			"no model selected")
	}
	model, ok := r.model(modelID)
	if !ok {
		return &productclient.Error{
			Op: "Preflight", Code: productclient.CodeModelNotFound, HTTPStatus: 404,
			Message: fmt.Sprintf("model %q is not in the signed catalog", modelID),
		}
	}
	if !model.Usable() {
		// Present but not selectable: the platform said so via `eligible`
		// or by advertising a non-gateway transport. Either way the client
		// must not route around it.
		return &productclient.Error{
			Op: "Preflight", Code: productclient.CodeModelNotAllowed, HTTPStatus: 403,
			Message: fmt.Sprintf("model %q is not eligible for gateway use", modelID),
		}
	}
	return nil
}

// model looks a model up in the catalog of the policy in force.
func (r *Runtime) model(id string) (productclient.Model, bool) {
	for _, m := range r.policy.Catalog().Models {
		if m.ID == id {
			return m, true
		}
	}
	return productclient.Model{}, false
}

// Observe decorates a sender for exactly one gateway call, after validating the
// call identity.
//
// The validation is the point: an empty clientRequestId means the platform
// cannot deduplicate a retry, so a network timeout would become indistinguishable
// from a second charge. Refusing here — before any request leaves — is the only
// cheap place to catch it.
func (r *Runtime) Observe(base agent.Sender, meta gateway.CallMeta, sink gateway.TerminalSink) (agent.Sender, error) {
	if base == nil {
		return nil, errors.New("productruntime: nil sender")
	}
	if sink == nil {
		return nil, errors.New("productruntime: nil terminal sink")
	}
	if err := meta.Validate(); err != nil {
		return nil, &productclient.Error{
			Op: "Observe", Code: productclient.CodeInvalidRequest, HTTPStatus: 400,
			Message: err.Error(), Err: err,
		}
	}
	return r.deps.Observer.Wrap(base, meta, sink), nil
}

// Cancel asks the platform to stop one gateway call.
func (r *Runtime) Cancel(ctx context.Context, clientRequestID string) error {
	if strings.TrimSpace(clientRequestID) == "" {
		return productclient.LocalError("Cancel", productclient.CodeInvalidRequest,
			"empty clientRequestId")
	}
	return r.deps.Gateway.Cancel(ctx, clientRequestID)
}

// RequestStatus returns the authoritative state of one gateway call. It is the
// only permitted reaction to a broken stream: the client asks, and never
// re-charges, refunds or blindly resends (中台交付包 §5.4).
func (r *Runtime) RequestStatus(ctx context.Context, clientRequestID string) (productclient.RequestStatus, error) {
	if strings.TrimSpace(clientRequestID) == "" {
		return productclient.RequestStatus{}, productclient.LocalError("RequestStatus",
			productclient.CodeInvalidRequest, "empty clientRequestId")
	}
	return r.deps.Gateway.RequestStatus(ctx, clientRequestID)
}

// Usage returns the read-only balance snapshot.
func (r *Runtime) Usage(ctx context.Context) (productclient.Usage, error) {
	return r.deps.Usage.Usage(ctx)
}

// Ledger returns one read-only page of the ledger.
func (r *Runtime) Ledger(ctx context.Context, query productclient.LedgerQuery) (productclient.LedgerPage, error) {
	return r.deps.Usage.Ledger(ctx, query)
}

// Sessions lists the account's device sessions.
func (r *Runtime) Sessions(ctx context.Context) ([]productclient.DeviceSession, error) {
	return r.deps.Sessions.Sessions(ctx)
}
