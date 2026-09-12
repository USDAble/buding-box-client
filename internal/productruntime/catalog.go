// OCTO-FORK: the client's catalog fetch and cache — see dev-docs-usdable/需求/20260911/开发计划.md (PR-4b).
package productruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/open-octo/octo-agent/internal/catalogstore"
	"github.com/open-octo/octo-agent/internal/productclient"
)

// catalogOutcome says why the catalog is or is not usable.
//
// Five values, not two, because PR-4c has to word four different situations and
// decide per situation whether a new turn may start (需求基线 B4): "the network
// is down", "the platform gave us something we cannot trust", "the platform
// offered an older catalog than we already have" and "this build carries no
// catalog at all" are not the same message, and collapsing them into one
// boolean would force PR-4c to recover the distinction from an error string.
//
// PR-4b only classifies and logs. Nothing here decides wording.
type catalogOutcome string

const (
	// catalogReady means a verified catalog was accepted (freshly fetched, or
	// already cached at an equal version).
	catalogReady catalogOutcome = "ready"
	// catalogTransport means the platform was not reached, or answered with a
	// failure: the cache is untouched and the user keeps whatever it holds.
	catalogTransport catalogOutcome = "transport"
	// catalogVerification means an envelope arrived and did not verify. This is
	// the fail-closed case (需求基线 B2 规则 2) and must never fall back to the
	// previous cache silently - see PR-4c.
	catalogVerification catalogOutcome = "verification"
	// catalogRolledBack means a validly signed but older catalog was refused, so
	// the cached one stays. That is the downgrade defence working, not a fault.
	catalogRolledBack catalogOutcome = "rolled_back"
	// catalogUnavailable means there was nothing to fetch: no store to keep the
	// answer, or no envelope in the response.
	catalogUnavailable catalogOutcome = "unavailable"
)

// fetchCatalog is the ONE place the catalog is fetched (开发计划 PR-4b 缺口 ④).
//
// The rule it defends is that "refresh on startup" and "refresh on login" are
// both plausible readings of B3, and implementing both gives two writers racing
// on one file with no owner. Exactly one function starts a fetch; a test asserts
// it over the source (catalog_test.go).
//
// It returns the outcome for the caller to report. An error accompanies every
// outcome except catalogReady, so a caller that logs only errors still sees a
// refusal.
func (rt *Runtime) fetchCatalog(ctx context.Context) (catalogOutcome, error) {
	store := rt.deps.Catalog
	if store == nil {
		// Nowhere to keep it. Fetching anyway would be a network call whose
		// answer nothing can read, and it would make "this build has no catalog"
		// indistinguishable from "the fetch failed".
		return catalogUnavailable, errors.New("productruntime: no catalog store configured")
	}
	if rt.deps.Platform == nil {
		return catalogUnavailable, errors.New("productruntime: no control plane configured")
	}

	data, err := rt.deps.Platform.Bootstrap(ctx)
	if err != nil {
		// A transport failure, a 5xx and a dead session all land here. They are
		// one outcome on purpose: from the catalog's point of view the only fact
		// is that nothing arrived, and the session case has already been handled
		// by the caller's own error path before this runs.
		return catalogTransport, err
	}
	if data.IsEmpty() {
		return catalogUnavailable, errors.New("productruntime: bootstrap carried no policy envelope")
	}

	trust := rt.deps.CatalogTrust
	if trust.Audience == "" || len(trust.TrustedKeys) == 0 {
		// A build that cannot answer the trust question must not answer "yes".
		// The blocked page reports this as a misconfiguration before the user
		// gets here (本地API契约 §2.13); this is the second line of defence.
		return catalogVerification, errors.New("productruntime: no trust anchor configured for catalog verification")
	}

	policy, err := data.PolicyEnvelope.Verify(productclient.VerifyOptions{
		TrustedKeys: trust.TrustedKeys,
		Audience:    trust.Audience,
		Now:         time.Now(),
		Skew:        trust.Skew,
	})
	if err != nil {
		return catalogVerification, err
	}
	if policy.Catalog.Version == "" {
		// A signed catalog without a version cannot be compared against the
		// cache, so it cannot take part in the monotonicity rule at all.
		return catalogVerification, fmt.Errorf("%w: policy carries no catalog version", productclient.ErrPolicyMalformed)
	}

	expiresAt, err := catalogExpiry(policy, time.Now())
	if err != nil {
		return catalogVerification, err
	}

	err = store.Put(catalogstore.Entry{
		CatalogVersion: policy.Catalog.Version,
		FetchedAt:      time.Now().UTC(),
		ExpiresAt:      expiresAt,
		KeyID:          data.Signature.KeyID,
		Audience:       policy.Audience,
		Envelope:       data.PolicyEnvelope,
	})
	switch {
	case err == nil:
		return catalogReady, nil
	case errors.Is(err, catalogstore.ErrRolledBack):
		// The cache is newer, which means either the platform is stale or
		// somebody replayed an old catalog. Either way the cache stays.
		return catalogRolledBack, err
	default:
		// Includes ErrNotByteExact, which is a real defect worth surfacing
		// rather than a condition to degrade around: the cache would never
		// verify again.
		return catalogVerification, err
	}
}

// catalogExpiry takes the earlier of the two clocks a catalog has to satisfy
// (需求基线 B2 规则 1): the platform's own signature window, and the freshness
// the catalog claims for itself. A catalog is usable only while both hold.
func catalogExpiry(policy productclient.Policy, now time.Time) (time.Time, error) {
	windowEnd, err := time.Parse(time.RFC3339, policy.ExpiresAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: policy expiresAt %q", productclient.ErrPolicyMalformed, policy.ExpiresAt)
	}
	issued, err := time.Parse(time.RFC3339, policy.IssuedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: policy issuedAt %q", productclient.ErrPolicyMalformed, policy.IssuedAt)
	}
	if policy.Catalog.TTLSec <= 0 {
		// No freshness claim: the signature window is the only bound there is.
		return windowEnd, nil
	}
	if ttlEnd := issued.Add(time.Duration(policy.Catalog.TTLSec) * time.Second); ttlEnd.Before(windowEnd) {
		return ttlEnd, nil
	}
	return windowEnd, nil
}

// logCatalogOutcome reports a non-ready catalog at the one level the operator can
// act on. It exists so the outcome enum has a consumer in this PR: without one,
// the five values would be dead code that PR-4c might quietly replace.
func (rt *Runtime) logCatalogOutcome(outcome catalogOutcome, err error) {
	slog.Warn("product: catalog unavailable", "outcome", string(outcome), "err", err)
}
