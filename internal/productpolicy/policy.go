// Package productpolicy verifies signed policy snapshots and answers the local
// policy-enforcement-point question: may this capability act, right now?
//
// Two boundaries define it:
//
//   - It never replaces server-side authorization. `entitled` and
//     `permissionPolicy` come from a verified envelope; this package can only
//     make the answer *narrower*, never wider. A policy that is missing,
//     unverifiable, expired or from an unknown key yields `deny`.
//   - It is not a feature flag store. `visible` decides what the UI shows and is
//     deliberately not consulted here: a hidden menu is not a permission and a
//     shown menu is not an authorization (P0-00A, P0-04 §2.4).
//
// The capability *id table* is P0-04's contract deliverable, not this package's.
// Nothing here switches on a hardcoded id, so there is exactly one place —
// the signed matrix — that names capabilities. That is deliberate: a local id
// list would be a second source of truth that silently disagrees after a
// platform-side rename (开发规范 §3.8).
package productpolicy

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
)

// Decision is the outcome of one enforcement-point evaluation.
type Decision string

const (
	// Deny blocks the action outright.
	Deny Decision = "deny"
	// Ask defers to the user. The agent loop already owns how an "ask" is
	// resolved (interactive prompt, or deny when non-interactive), so this
	// package only returns the verdict.
	Ask Decision = "ask"
	// Allow lets the action proceed without further prompting.
	Allow Decision = "allow"
)

// ParseDecision maps a wire value to a Decision. An unrecognised value is not
// an error the caller should guess at — it is treated as Deny by callers, so
// the boolean is false rather than returning a zero Decision that would read as
// "deny" by accident and hide the mismatch.
func ParseDecision(s string) (Decision, bool) {
	switch Decision(strings.TrimSpace(s)) {
	case Deny:
		return Deny, true
	case Ask:
		return Ask, true
	case Allow:
		return Allow, true
	default:
		return "", false
	}
}

// Request is one enforcement-point question. It carries no conversation content:
// the PEP decides *whether* an action may run, not what it may say.
type Request struct {
	// Capability is a matrix id. An id absent from the verified matrix is
	// denied — the matrix is the only naming authority.
	Capability string
	// Target is the path or host the action would touch, when there is one.
	// It is recorded for audit; it does not widen or narrow the verdict,
	// because a path-based rule set would be a second, weaker policy.
	Target string
	// Background marks an action that runs without a user waiting on it
	// (scheduled or background task). It can only tighten a verdict.
	Background bool
	// LocalReadOnly asserts that the action touches only local data and has
	// no external effect. The caller declares it; it cannot be inferred here,
	// and a dishonest declaration gains nothing while the policy is live —
	// it only matters once the policy has expired, where it is the single
	// thing that keeps read-only local work possible (P0-04 §2.3).
	LocalReadOnly bool
}

// Verdict is a Decision plus a reason suitable for an audit log or a prompt.
// Reason is not UI copy: the presentation layer maps Decision (and the error
// code, when there is one) to text, never this string.
type Verdict struct {
	Decision Decision
	Reason   string
}

// ClockSkewAllowance is how far a client clock may lag `issuedAt` before the
// snapshot is treated as not-yet-valid. A clock far in the past would otherwise
// make an expired policy look valid indefinitely, so the window is closed in
// both directions rather than only checking `expiresAt`.
const ClockSkewAllowance = 5 * time.Minute

// Signed is the verified content of one policy envelope, already checked
// against a trusted key. Constructing it is the Verifier's job; a Signed value
// is trusted only in the sense that its bytes match the signature — every
// policy question is still answered by Policy.
type Signed struct {
	PolicyVersion    string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	Audience         string
	KeyID            string
	MinClientVersion string
	Catalog          productclient.Catalog
	Capabilities     []productclient.Capability
}

// Verifier checks a raw signed envelope against the compiled-in trusted keys
// and returns its content. This is the policy-side port, not the crypto: the
// single envelope verifier lives in internal/productclient (ed25519 over the
// JCS payload, keyId lookup, keys injected by the assembly root) and serves
// both the policy/catalog envelope and the sensitive-word envelope. An adapter
// in internal/productruntime turns its verified payload into Signed, so that
// 04 and 06 cannot each grow a second verifier.
//
// Anything satisfying this interface must:
//
//   - use only keys injected from productprofile.TrustedKeyIDs, never a key
//     from the network — a fetched key makes the signature meaningless;
//   - treat an unknown `keyId` as failure, not as "skip verification";
//   - verify over the JCS-normalised payload with the `signature` field
//     removed;
//   - return a *productclient.Error with
//     productclient.CodeCatalogSignatureInvalid on failure, so the UI has one
//     code surface for "the platform's word could not be trusted".
type Verifier interface {
	Verify(ctx context.Context, raw []byte) (Signed, error)
}

// Errors returned by NewPolicy. Each is a fail-closed condition: the caller
// keeps the current session readable and refuses to start new work.
var (
	ErrAudienceMismatch = errors.New("productpolicy: policy audience is not this product")
	ErrClientTooOld     = errors.New("productpolicy: client version below minClientVersion")
	ErrEmptyIdentity    = errors.New("productpolicy: policy has no keyId or version")
	ErrBadWindow        = errors.New("productpolicy: policy validity window is malformed")
	ErrVersionRollback  = errors.New("productpolicy: policy version went backwards")
)

// Policy is a verified snapshot plus the local decisions derived from it.
type Policy struct {
	version   string
	issuedAt  time.Time
	expiresAt time.Time
	audience  string
	keyID     string
	catalog   productclient.Catalog

	caps map[string]productclient.Capability
}

// NewPolicy validates a verified envelope and indexes its capability matrix.
//
// clientVersion is this build's version; audience must equal the value compiled
// into the product profile by the caller (it is passed in rather than read here
// so that productprofile stays the only place that knows the brand).
func NewPolicy(s Signed, audience, clientVersion string) (*Policy, error) {
	if strings.TrimSpace(s.KeyID) == "" || strings.TrimSpace(s.PolicyVersion) == "" {
		return nil, ErrEmptyIdentity
	}
	if s.Audience != audience {
		return nil, fmt.Errorf("%w: got %q want %q", ErrAudienceMismatch, s.Audience, audience)
	}
	if s.IssuedAt.IsZero() || s.ExpiresAt.IsZero() || !s.ExpiresAt.After(s.IssuedAt) {
		return nil, ErrBadWindow
	}
	if older, err := VersionLess(clientVersion, s.MinClientVersion); err != nil {
		// An unparseable version pair is not a reason to admit the client:
		// "we could not tell" must not become "yes".
		return nil, fmt.Errorf("%w: %v", ErrClientTooOld, err)
	} else if older {
		return nil, fmt.Errorf("%w: client %s < required %s", ErrClientTooOld, clientVersion, s.MinClientVersion)
	}

	caps := make(map[string]productclient.Capability, len(s.Capabilities))
	for _, c := range s.Capabilities {
		// A duplicate id is a platform bug, but resolving it by
		// last-write-wins would make the verdict depend on JSON ordering.
		// The stricter row wins, deterministically.
		if prev, ok := caps[c.ID]; ok {
			caps[c.ID] = stricter(prev, c)
			continue
		}
		caps[c.ID] = c
	}

	return &Policy{
		version:   s.PolicyVersion,
		issuedAt:  s.IssuedAt,
		expiresAt: s.ExpiresAt,
		audience:  s.Audience,
		keyID:     s.KeyID,
		catalog:   s.Catalog,
		caps:      caps,
	}, nil
}

// stricter returns whichever capability grants less. Used only to make a
// duplicate-id envelope deterministic.
func stricter(a, b productclient.Capability) productclient.Capability {
	if rank(b.PermissionPolicy) < rank(a.PermissionPolicy) ||
		(b.PermissionPolicy == a.PermissionPolicy && b.Entitled == false && a.Entitled == true) {
		return b
	}
	return a
}

func rank(policy string) int {
	switch policy {
	case productclient.PermissionAllow:
		return 2
	case productclient.PermissionAsk:
		return 1
	default: // deny, empty, or anything unrecognised
		return 0
	}
}

// Version returns the policy version. Callers persist this to enforce
// monotonicity across restarts.
func (p *Policy) Version() string { return p.version }

// Audience returns the verified audience, for diagnostics.
func (p *Policy) Audience() string { return p.audience }

// KeyID returns the key that signed this snapshot, so a rotation can be
// diagnosed without re-parsing the envelope.
func (p *Policy) KeyID() string { return p.keyID }

// Catalog returns the signed model catalog carried by this policy. It is the
// only source of selectable models.
func (p *Policy) Catalog() productclient.Catalog { return p.catalog }

// Capability returns one matrix row.
func (p *Policy) Capability(id string) (productclient.Capability, bool) {
	c, ok := p.caps[id]
	return c, ok
}

// NotYetValid reports a clock that is behind `issuedAt` by more than the
// allowance. It is separate from Expired because the two need different
// messages, but both are fail-closed.
func (p *Policy) NotYetValid(now time.Time) bool {
	return now.Before(p.issuedAt.Add(-ClockSkewAllowance))
}

// Expired reports a snapshot past its validity window. An expired policy does
// not become "no policy": it keeps permitting only what Evaluate decides below.
func (p *Policy) Expired(now time.Time) bool {
	return now.After(p.expiresAt)
}

// AcceptVersion enforces monotonicity: a snapshot older than the one already
// held is rejected. Without this, a replayed older envelope (still correctly
// signed) would silently re-grant a capability the platform had withdrawn.
func (p *Policy) AcceptVersion(previous string) error {
	if previous == "" || previous == p.version {
		return nil
	}
	older, err := VersionLess(p.version, previous)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrVersionRollback, err)
	}
	if older {
		return fmt.Errorf("%w: %s < %s", ErrVersionRollback, p.version, previous)
	}
	return nil
}

// Evaluate answers one enforcement-point question.
//
// The rule order is the whole design, and every branch fails closed:
//
//  1. An id the matrix does not name is denied. The matrix is the only naming
//     authority, so "unknown" cannot mean "unrestricted".
//  2. `available == false` is denied — the capability's code is not present,
//     so there is nothing to authorize.
//  3. `entitled == false` is denied, with a reason that distinguishes it from
//     "not available", because the user-facing remedy differs.
//  4. `permissionPolicy` is applied, but a background action can never come
//     out better than Ask: a scheduled action has no user watching it, so it
//     must not inherit an interactive "allow".
//  5. A clock outside the envelope's window — behind `issuedAt` beyond the
//     allowance, or past `expiresAt` — fails closed. The two are handled
//     differently because the spec treats them differently: a clock anomaly is
//     grouped with signature failure and denies everything, while plain expiry
//     still permits declared local read-only work under an explicit `allow`
//     (P0-04 §过期语义).
//
// now is passed in rather than read from the clock so the expiry rules are
// testable without sleeping.
func (p *Policy) Evaluate(req Request, now time.Time) Verdict {
	c, known := p.caps[req.Capability]
	if !known {
		return Verdict{Deny, "capability is not in the verified matrix"}
	}
	if !c.Available {
		return Verdict{Deny, "capability is not available in this build"}
	}
	if !c.Entitled {
		return Verdict{Deny, "account is not entitled to this capability"}
	}

	base, ok := ParseDecision(c.PermissionPolicy)
	if !ok {
		// Missing or malformed permissionPolicy: the platform did not say
		// "allow", so we must not infer it.
		return Verdict{Deny, "capability has no recognised permissionPolicy"}
	}

	if req.Background && base == Allow {
		base = Ask
	}

	// A clock anomaly invalidates the whole window rather than just its end:
	// the envelope may in fact be expired, and we cannot tell. Treat it as
	// "no policy", not as "a policy that happens to be current".
	if p.NotYetValid(now) {
		return Verdict{Deny, "clock is outside the policy window; treating the policy as unverifiable"}
	}

	if p.Expired(now) {
		if base == Allow && req.LocalReadOnly && !req.Background {
			return Verdict{Allow, "policy expired: local read-only action remains permitted"}
		}
		return Verdict{Deny, "policy expired"}
	}

	return Verdict{base, "policy grants " + string(base)}
}

// CompareVersions orders two dotted numeric versions and returns -1, 0 or 1.
//
// It handles both formats the contract actually uses:
//
//	client / minClientVersion   1.2.0      semver-style
//	policyVersion / catalog     2026-09-10.1   date plus a counter
//
// Segments are split on `.` and `-` and compared numerically, so
// `2026-09-10.10` correctly sorts after `2026-09-10.2` — a plain string compare
// would get that backwards and would therefore accept a replayed older policy.
// A shorter version is zero-padded, so "1.2" equals "1.2.0".
//
// Pre-release and build metadata are deliberately unsupported: silently
// ordering "1.2.0-rc1" as if it were "1.2.0" is the kind of guess that fails
// open. An unsupported form is an error, and every caller turns that into a
// denial.
func CompareVersions(a, b string) (int, error) {
	pa, err := parseVersion(a)
	if err != nil {
		return 0, fmt.Errorf("version %q: %w", a, err)
	}
	pb, err := parseVersion(b)
	if err != nil {
		return 0, fmt.Errorf("version %q: %w", b, err)
	}
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1, nil
			}
			return 1, nil
		}
	}
	return 0, nil
}

// VersionLess reports whether a sorts before b. It is CompareVersions < 0, kept
// as a named helper because "is the client too old" reads better as a boolean.
func VersionLess(a, b string) (bool, error) {
	cmp, err := CompareVersions(a, b)
	if err != nil {
		return false, err
	}
	return cmp < 0, nil
}

// parseVersion splits a version into numeric segments. All segments must be
// non-negative integers: a letter anywhere is an unsupported form, not a
// segment to skip.
//
// Empty segments are rejected rather than skipped. Dropping them would make
// "1..2" and "1.-1.0" compare equal to "1.2.0" and "1.1.0" respectively, and
// two different version strings comparing equal is exactly how a replay check
// quietly stops working.
func parseVersion(v string) ([]int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, errors.New("empty")
	}
	if isVersionSep(v[0]) || isVersionSep(v[len(v)-1]) {
		return nil, errors.New("leading or trailing separator")
	}
	for i := 1; i < len(v); i++ {
		if isVersionSep(v[i]) && isVersionSep(v[i-1]) {
			return nil, errors.New("empty component")
		}
	}

	parts := strings.FieldsFunc(v, func(r rune) bool { return isVersionSep(byte(r)) })
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, errors.New("component is not a non-negative integer")
		}
		out = append(out, n)
	}
	return out, nil
}

// isVersionSep reports whether b separates version segments. Both `.` and `-`
// are separators because the contract uses two formats: "1.2.0" for client
// versions and "2026-09-10.1" for policy versions.
func isVersionSep(b byte) bool { return b == '.' || b == '-' }
