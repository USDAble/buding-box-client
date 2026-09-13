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

// catalogState is the answer to "can this build use a catalog, and if not why",
// as served by GET /api/product/catalog (本地API契约 §2.14).
//
// Four values, and the first three exist because 需求基线 B9 demands four
// distinct sentences and 中台交付包 §1.2 names three distinct kinds of
// unusability. They are not severities of one problem: "we have never had a
// catalog", "ours expired and the refresh failed" and "what arrived could not be
// trusted" call for three different sentences and three different recoveries.
type catalogState string

const (
	// catalogStateReady means a verified, unexpired catalog is in hand: the
	// picker has something to show and a new turn may start.
	catalogStateReady catalogState = "ready"
	// catalogStateAbsent means there is no cached catalog at all. On a fresh
	// installation this is the ordinary state, and it is NOT a missing key: B4
	// forbids answering it with the key wizard.
	catalogStateAbsent catalogState = "absent"
	// catalogStateStale means a catalog exists but has expired and the refresh
	// did not succeed. B4 rule 1: no new turn, the old conversation stays
	// readable, and the user is told the list needs a network update.
	catalogStateStale catalogState = "stale"
	// catalogStateUnverifiable means an envelope arrived and did not verify.
	// Fail-closed (B2 规则 2 / B4): the offending bytes are never shown and the
	// cache is never overwritten with them.
	catalogStateUnverifiable catalogState = "unverifiable"
)

// catalogAvailability is the wire-adjacent result of the judgement. It carries
// no wording: the sentences live in the frontend's i18n dictionary, and the
// reason is machine-readable so there is exactly one place that decides it
// (开发规范 §3.8).
type catalogAvailability struct {
	State catalogState
	// Retryable answers "is pressing retry useful", which is a different
	// question from "is this a failure" — the same distinction the control-plane
	// tiers draw (web/src/lib/product.ts, tierRetryable).
	Retryable bool
	// CatalogVersion and ExpiresAt describe the cache the state is about. Both
	// are empty/zero when there is no cache to describe, and they are always
	// present on the wire rather than omitted: "absent" and "present but empty"
	// would otherwise be two spellings of one state.
	CatalogVersion string
	ExpiresAt      time.Time
}

// assessCatalog is the ONE judgement (开发计划 PR-4c 交付物 1).
//
// Three inputs and one output. The inputs are the three facts the answer depends
// on, and each comes from its own owner: `haveCache`/`entry` from
// internal/catalogstore (which owns data/catalog.json), `outcome` from the last
// refresh this package ran, `now` from the caller so the rule is testable
// without sleeping.
//
// The precedence, and why it is this order rather than any other:
//
//  1. A verification failure wins over a usable cache. This is the one ordering
//     decision in the function and it is deliberate. B4 groups the three kinds
//     of unusability and B9 requires each to have its own sentence on screen —
//     and a sentence needs a face to appear on. Serving the cache and letting
//     turns continue would leave the picker populated, which is exactly the
//     "the picker says one thing, the banner says another" drift PR-4c's 第 0 步
//     was called in to stop. It is also the fail-closed direction B2 规则 2
//     names, and §3.9 forbids degrading quietly to an untrusted source.
//  2. No cache. Nothing to describe, so nothing else can be true.
//  3. An expired cache. This is the only case B4 rule 1 marks "no new turn".
//  4. Otherwise ready.
//
// It takes no lock and touches no disk: whether a refresh is *due* is a separate
// question, asked by refreshCatalogOnDemand.
func assessCatalog(entry catalogstore.Entry, haveCache bool, outcome catalogOutcome, now time.Time) catalogAvailability {
	if outcome == catalogVerification {
		// Version and expiry are reported anyway when they exist: an operator
		// reading a log wants to know which catalog the refused answer was
		// supposed to replace.
		return catalogAvailability{State: catalogStateUnverifiable, Retryable: false, CatalogVersion: entry.CatalogVersion}
	}
	if !haveCache {
		// Retryable: the recovery B4 names for "no cache" is the first
		// successful refresh, and a refresh is something the user can ask for.
		return catalogAvailability{State: catalogStateAbsent, Retryable: true}
	}
	if !entry.ExpiresAt.IsZero() && !now.Before(entry.ExpiresAt) {
		// Retryable: B4's recovery is "下次刷新成功即自动恢复", so trying again is
		// exactly what helps.
		return catalogAvailability{State: catalogStateStale, Retryable: true, CatalogVersion: entry.CatalogVersion, ExpiresAt: entry.ExpiresAt}
	}
	return catalogAvailability{State: catalogStateReady, Retryable: false, CatalogVersion: entry.CatalogVersion, ExpiresAt: entry.ExpiresAt}
}

// needsRefresh reports whether a refresh is due before this state can be
// believed.
//
// `unverifiable` is deliberately excluded. B4 makes a failed verification
// terminal for the response that failed ("不自动重试同一份响应"), so retrying it
// on every picker open would be precisely the automatic retry the rule forbids.
// Recovery is the platform's to cause — a new signature or key — and then the
// next refresh event (a login, which is forced) picks it up. Recovery is not
// reachable by pressing the same button again, which is also why Retryable is
// false above: two places saying the same thing, one of them by silence.
func (a catalogAvailability) needsRefresh() bool {
	return a.State == catalogStateAbsent || a.State == catalogStateStale
}

// recordCatalogOutcome remembers the last refresh verdict.
func (rt *Runtime) recordCatalogOutcome(outcome catalogOutcome) {
	rt.catalogMu.Lock()
	defer rt.catalogMu.Unlock()
	rt.lastCatalogOutcome = outcome
}

// lastOutcome reads the most recent verdict.
func (rt *Runtime) lastOutcome() catalogOutcome {
	rt.catalogMu.Lock()
	defer rt.catalogMu.Unlock()
	return rt.lastCatalogOutcome
}

// currentCatalogAvailability runs the judgement against the current cache and
// the last refresh verdict. It is a read: no network, no disk, no lock held
// across the load beyond the store's own.
func (rt *Runtime) currentCatalogAvailability() catalogAvailability {
	var entry catalogstore.Entry
	have := false
	if rt.deps.Catalog != nil {
		if got, err := rt.deps.Catalog.Load(); err == nil {
			entry, have = got, true
		}
	}
	return assessCatalog(entry, have, rt.lastOutcome(), time.Now())
}

// refreshCatalogOnDemand performs B3's "陈旧时按需刷新" and reports the state
// afterwards, which is what GET /api/product/catalog answers with.
//
// It refreshes at most once per call, and only when needsRefresh says so, so the
// two rules that matter are both visible here: opening the picker cannot become a
// retry loop, and a failed verification cannot be retried by looking at it.
func (rt *Runtime) refreshCatalogOnDemand(ctx context.Context) catalogAvailability {
	before := rt.currentCatalogAvailability()
	if !before.needsRefresh() {
		return before
	}
	if _, err := rt.refreshCatalog(ctx, false); err != nil {
		// Reported, not returned: the state below is the answer to the question
		// the caller asked ("can I use a catalog"), and it already carries the
		// refusal. Returning an HTTP error instead would make the frontend
		// choose between "we could not ask" and "the answer is no", which is the
		// distinction the four states exist to make.
		rt.logCatalogOutcome(rt.lastOutcome(), err)
	}
	return rt.currentCatalogAvailability()
}

// catalogAvailabilityDTO is the wire shape of 本地API契约 §2.14.
//
// All four fields are always present, for the same reason controlPlaneDTO's are:
// the four states are distinguished by their VALUES, so omitting a false or an
// empty string would collapse two of them for a consumer that reads absence as
// "not applicable".
type catalogAvailabilityDTO struct {
	State          string `json:"state"`
	Retryable      bool   `json:"retryable"`
	CatalogVersion string `json:"catalogVersion"`
	ExpiresAt      string `json:"expiresAt"`
}

// toDTO renders the judgement for the wire. The expiry is formatted here rather
// than by the JSON encoder so that "no cache" is an empty string and not the
// year-one timestamp Go would otherwise emit.
func (a catalogAvailability) toDTO() catalogAvailabilityDTO {
	expires := ""
	if !a.ExpiresAt.IsZero() {
		expires = a.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return catalogAvailabilityDTO{
		State:          string(a.State),
		Retryable:      a.Retryable,
		CatalogVersion: a.CatalogVersion,
		ExpiresAt:      expires,
	}
}

// fetchCatalog is the ONE place the catalog is fetched (开发计划 PR-4b 缺口 ④).
//
// The rule it defends is that "refresh on startup" and "refresh on login" are
// both plausible readings of B3, and implementing both gives two writers racing
// on one file with no owner. Exactly one function starts a fetch; a test asserts
// it over the source (catalog_test.go).
//
// knownVersion chooses which platform call it is, and the choice is the whole of
// the conditional-request feature (PR-4c, 中台交付包 §4.3):
//
//   - empty — an unconditional snapshot, taken from the login response. That is
//     the login path (the account may have changed, so a version comparison
//     would be answering the wrong question) and the no-cache path (there is no
//     version to send).
//   - non-empty — the dedicated refresh endpoint, told which version we hold, so
//     the platform can answer "nothing new". Standing up a second endpoint for
//     the no-cache case would be a second way to fetch one catalog.
//
// It returns the outcome for the caller to report. An error accompanies every
// outcome except catalogReady, so a caller that logs only errors still sees a
// refusal.
func (rt *Runtime) fetchCatalog(ctx context.Context, knownVersion string) (catalogOutcome, error) {
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

	envelope, unchanged, err := rt.obtainCatalogEnvelope(ctx, knownVersion)
	if err != nil {
		// A transport failure, a 5xx and a dead session all land here. They are
		// one outcome on purpose: from the catalog's point of view the only fact
		// is that nothing arrived, and the session case has already been handled
		// by the caller's own error path before this runs.
		return catalogTransport, err
	}
	if unchanged {
		// B3's acceptance ("同一份缓存收到 304 后文件 mtime 不变") is a disk
		// assertion, so the skip has to happen here rather than in the store: a
		// Put of an identical version is already a no-op, but reaching it would
		// still mean a conditional request rewrote a file it was told not to.
		return catalogReady, nil
	}
	if envelope.IsEmpty() {
		return catalogUnavailable, errors.New("productruntime: the response carried no policy envelope")
	}

	trust := rt.deps.CatalogTrust
	if trust.Audience == "" || len(trust.TrustedKeys) == 0 {
		// A build that cannot answer the trust question must not answer "yes".
		// The blocked page reports this as a misconfiguration before the user
		// gets here (本地API契约 §2.13); this is the second line of defence.
		return catalogVerification, errors.New("productruntime: no trust anchor configured for catalog verification")
	}

	policy, err := envelope.Verify(productclient.VerifyOptions{
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
		KeyID:          envelope.Signature.KeyID,
		Audience:       policy.Audience,
		Envelope:       envelope,
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

// obtainCatalogEnvelope performs the platform call fetchCatalog needs, reporting
// whether the platform said the held copy is still current.
//
// It is split out so fetchCatalog reads as the verify-and-store rule it is,
// while the "which call" decision stays explicit and in one place. It must not
// grow a second caller: two places that decide how to ask is the same defect as
// two places that fetch (开发规范 §3.8).
func (rt *Runtime) obtainCatalogEnvelope(ctx context.Context, knownVersion string) (productclient.PolicyEnvelope, bool, error) {
	if knownVersion != "" {
		data, err := rt.deps.Platform.CatalogModels(ctx, knownVersion)
		if err != nil {
			return productclient.PolicyEnvelope{}, false, err
		}
		return data.PolicyEnvelope, data.Unchanged, nil
	}
	data, err := rt.deps.Platform.Bootstrap(ctx)
	if err != nil {
		return productclient.PolicyEnvelope{}, false, err
	}
	return data.PolicyEnvelope, false, nil
}

// refreshCatalog is the ONE entry point that starts a fetch, for callers that
// are not the login handler (开发计划 PR-4c 交付物 1).
//
// force selects between B3's two refresh triggers:
//
//   - true — "换账号强制刷新". The account may have changed, so a version
//     comparison would answer the wrong question; the snapshot comes from the
//     login response.
//   - false — "陈旧时按需刷新". The version we hold is offered so the platform
//     can answer "nothing new" and the cache is left untouched.
//
// It records the outcome, because the availability judgement below needs the
// most recent verdict and no store can answer it: the cache records what was
// ACCEPTED, never what was refused (PR-4c 缺口 ①).
func (rt *Runtime) refreshCatalog(ctx context.Context, force bool) (catalogOutcome, error) {
	known := ""
	if !force {
		// A cache that will not load has no version to offer, which is the same
		// request as having none at all. A nil store is that case too: this
		// method is also reached from the endpoint, which must answer with the
		// absent state rather than panic when the build has nowhere to keep a
		// catalog.
		if rt.deps.Catalog != nil {
			if entry, err := rt.deps.Catalog.Load(); err == nil {
				known = entry.CatalogVersion
			}
		}
	}
	outcome, err := rt.fetchCatalog(ctx, known)
	rt.recordCatalogOutcome(outcome)
	return outcome, err
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
