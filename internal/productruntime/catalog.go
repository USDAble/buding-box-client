// OCTO-FORK: the client's catalog fetch and cache — see dev-docs-usdable/需求/20260911/开发计划.md (PR-4b).
package productruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/open-octo/octo-agent/internal/catalogstore"
	"github.com/open-octo/octo-agent/internal/chatmode"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productprofile"
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
	// A platform that sends an absurd ttlSec must not be able to wrap the
	// multiplication into a negative duration, which would read as "this catalog
	// expired before it was issued" and send PR-4c down the stale path for a
	// catalog that is actually fine. Anything beyond the signature window's own
	// span cannot be the earlier of the two anyway, so it is simply ignored.
	const maxTTL = int64(100 * 365 * 24 * time.Hour / time.Second)
	if ttl := int64(policy.Catalog.TTLSec); ttl > maxTTL {
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

// chatModeModel is one row the picker renders (本地API契约 §2.8).
type chatModeModel struct {
	ID          string
	DisplayName productclient.DisplayName
	// CompositeID is "<gateway endpoint>::<catalog id>", the form a session
	// stores (需求基线 B8 规则 1). It is built here rather than accepted from the
	// catalog because the endpoint half is a product constant - the platform
	// supplies only the id.
	CompositeID string
}

// chatModeGroup is one picker group: a product mode id and the models the
// catalog put under it.
type chatModeGroup struct {
	ID string
	// Models can be empty: the mode is a product constant, so "this group has
	// nothing eligible right now" is a state the picker shows, not one that
	// removes the group (PR-4c words it as 分组空).
	Models []chatModeModel
	// DefaultModel is a composite id, or empty when the catalog's
	// defaultModelId did not name a member of this group.
	DefaultModel string
}

// projectCatalog turns a verified catalog into the picker's groups.
//
// It is pure over the policy - no store, no clock, no network - which is what
// lets every rule below be pinned by constructing a catalog rather than by
// teaching the signing fixture to produce malformed ones.
//
// The rules, and where each comes from:
//
//   - Iterate the PRODUCT mode set (internal/chatmode), not the catalog's. A mode
//     is offered because the product has it; a catalog naming an unknown one is
//     reported and ignored, never invented (中台交付包 §4.3 catalog.modes.id).
//   - Group by each model's own ModeIDs. `catalog.modes[]` deliberately carries no
//     model list, because a second copy of the grouping fact drifts and the picker
//     would then answer differently depending on which copy it read (同节).
//   - Keep only eligible && transport == "gateway" models. Both halves matter:
//     `eligible` is the account's entitlement, `transport` is the rule that keeps
//     a shipped build from reaching a provider around the gateway (需求基线 C1).
//   - A default is used only if it names a member of its own group; otherwise it
//     is dropped and the group keeps its models. The group itself is never
//     dropped - it is a product constant.
//
// It returns the ignored mode ids rather than logging them, so the caller owns
// the logging and this stays a function of its input.
func projectCatalog(policy productclient.Policy) ([]chatModeGroup, []string) {
	catalog := policy.Catalog

	// Models kept per mode id, in catalog order. Building this first means each
	// rule below is applied once rather than inside the group loop.
	byMode := map[string][]chatModeModel{}
	var ignored []string
	seenIgnored := map[string]bool{}
	note := func(id string) {
		if id == "" || chatmode.IsProductMode(id) || seenIgnored[id] {
			return
		}
		seenIgnored[id] = true
		ignored = append(ignored, id)
	}

	for _, m := range catalog.Models {
		if !m.Eligible || m.Transport != transportGateway {
			continue
		}
		row := chatModeModel{
			ID:          m.ID,
			DisplayName: m.DisplayName,
			CompositeID: productprofile.GatewayModelPrefix() + m.ID,
		}
		for _, modeID := range m.ModeIDs {
			if !chatmode.IsProductMode(modeID) {
				note(modeID)
				continue
			}
			byMode[modeID] = append(byMode[modeID], row)
		}
	}

	// The catalog's mode entries supply defaults and nothing else; a mode id the
	// product does not have is reported here too, because it can appear only here.
	defaults := map[string]string{}
	for _, m := range catalog.Modes {
		if !chatmode.IsProductMode(m.ID) {
			note(m.ID)
			continue
		}
		defaults[m.ID] = m.DefaultModelID
	}

	groups := make([]chatModeGroup, 0, len(chatmode.IDs()))
	for _, id := range chatmode.IDs() {
		models := byMode[id]
		group := chatModeGroup{ID: id, Models: models}
		// Membership, not mere existence: a default that is not in this group is
		// not this group's default (中台交付包 §4.3 `defaultModelId`).
		if want := defaults[id]; want != "" {
			for _, m := range models {
				if m.ID == want {
					group.DefaultModel = m.CompositeID
					break
				}
			}
		}
		groups = append(groups, group)
	}
	return groups, ignored
}

// transportGateway is the only transport the catalog may name for a selectable
// model (中台交付包 §4.3: "只有 eligible=true 且 transport=gateway"). Anything
// else - including a provider-direct transport a future contract adds - is not
// selectable from a shipped build, so the constant is the allow-list rather than
// a deny-list of known-bad values.
const transportGateway = "gateway"
