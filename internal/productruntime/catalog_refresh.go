package productruntime

import (
	"context"
	"time"
)

// OCTO-FORK: the existing runtime owns one catalog request per login session.
// Waiters own only their wait; abandoning a menu or initialization cannot cancel
// another sender's refresh. The shared request has the platform's 30s budget.
type catalogRefresh struct {
	generation uint64
	done       chan struct{}
	outcome    catalogOutcome
	err        error
}

func (rt *Runtime) catalogGeneration() uint64 {
	if rt.deps.Platform == nil {
		return 0
	}
	return rt.deps.Platform.SessionGeneration()
}

func catalogRenewalDue(expires, fetched, now time.Time) bool {
	if expires.IsZero() {
		return false
	}
	lead := expires.Sub(fetched) / 5
	if lead > 30*time.Second {
		lead = 30 * time.Second
	}
	return !now.Before(expires.Add(-lead))
}

// OCTO-FORK: reopening the picker checks administrative changes before the
// signed lease expires. The caller keeps displaying the verified cache while
// this conditional request shares the existing flight and retry budget.
func (rt *Runtime) refreshCatalogForPicker(ctx context.Context) (catalogAvailability, error) {
	before := rt.currentCatalogAvailability()
	if !before.ready() {
		return rt.refreshCatalogOnDemand(ctx)
	}
	if _, err := waitCatalogRefresh(ctx, rt.startCatalogRefresh(false, false, true)); err != nil {
		rt.logCatalogOutcome(rt.lastOutcome(), err)
		return rt.currentCatalogAvailability(), err
	}
	return rt.currentCatalogAvailability(), nil
}

func (rt *Runtime) startCatalogRefresh(force, onlyIfNeeded, background bool) *catalogRefresh {
	generation := rt.catalogGeneration()
	rt.catalogMu.Lock()
	defer rt.catalogMu.Unlock()
	if flight := rt.catalogFlight; flight != nil && flight.generation == generation {
		return flight
	}
	now := time.Now()
	known := ""
	if rt.deps.Catalog != nil && rt.catalogSessionVerified && rt.catalogSession == generation {
		if entry, err := rt.deps.Catalog.Load(); err == nil {
			available := assessCatalog(entry, true, rt.lastCatalogOutcome, now)
			due := catalogRenewalDue(entry.ExpiresAt, entry.FetchedAt, now)
			if onlyIfNeeded && available.ready() && !due {
				return nil
			}
			if background && (!available.ready() || now.Before(rt.catalogRetryAfter)) {
				return nil
			}
			if !force && available.ready() && !due {
				known = entry.CatalogVersion
			}
		}
	}
	flight := &catalogRefresh{generation: generation, done: make(chan struct{})}
	rt.catalogFlight = flight
	go rt.runCatalogRefresh(flight, known)
	return flight
}

func (rt *Runtime) runCatalogRefresh(flight *catalogRefresh, known string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	outcome, err := rt.fetchCatalogForSession(ctx, known, flight.generation)
	commit := func() error {
		rt.catalogMu.Lock()
		defer rt.catalogMu.Unlock()
		rt.lastCatalogOutcome = outcome
		rt.catalogOutcomeSession = flight.generation
		if outcome == catalogReady {
			rt.catalogSession = flight.generation
			rt.catalogSessionVerified = true
		}
		// A network outage must not cause each menu render to retry renewal.
		// Expired-cache and explicit retries still run immediately.
		rt.catalogRetryAfter = time.Now().Add(5 * time.Second)
		return nil
	}
	if rt.deps.Platform != nil {
		if sessionErr := rt.deps.Platform.WithSession(flight.generation, commit); sessionErr != nil {
			if !IsSessionExpired(err) || !rt.deps.Platform.SessionEnded(flight.generation) {
				outcome, err = catalogTransport, sessionErr
			}
		}
	} else {
		_ = commit()
	}
	rt.catalogMu.Lock()
	flight.outcome, flight.err = outcome, err
	if rt.catalogFlight == flight {
		rt.catalogFlight = nil
	}
	close(flight.done)
	rt.catalogMu.Unlock()
}

func waitCatalogRefresh(ctx context.Context, flight *catalogRefresh) (catalogOutcome, error) {
	if err := ctx.Err(); err != nil {
		return catalogTransport, err
	}
	if flight == nil {
		return catalogReady, nil
	}
	select {
	case <-ctx.Done():
		return catalogTransport, ctx.Err()
	case <-flight.done:
		return flight.outcome, flight.err
	}
}
