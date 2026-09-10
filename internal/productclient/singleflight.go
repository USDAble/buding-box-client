package productclient

import (
	"context"
	"sync"
)

// singleFlight coalesces concurrent calls into one in-flight execution.
//
// It exists because of a specific failure: N requests hit 401 at the same time
// (the access token expired while a page was loading) and each one independently
// calls Refresh. The platform's refresh token rotates on use, so the first
// refresh invalidates the token the other N-1 are still holding — they all fail,
// the session is dropped, and the user is sent back to the login page having
// done nothing wrong. One refresh, N waiters, no rotation race.
//
// This is deliberately not golang.org/x/sync/singleflight: that is a new
// dependency for ~40 lines, and its API (Do returns an `any`) would need a type
// assertion at every call site in the one place where getting it wrong logs the
// user out.
//
// Context: the *first* caller's context drives the execution. A waiter that
// gives up does not cancel the work the others are still waiting for, which is
// the behaviour that matters here — one impatient request must not abort the
// refresh the rest of the page depends on.
type singleFlight struct {
	mu   sync.Mutex
	call *flightCall
}

type flightCall struct {
	done  chan struct{}
	token string
	err   error
}

// Do runs fn, or joins the execution already in progress. The returned values
// are fn's.
func (g *singleFlight) Do(ctx context.Context, fn func(context.Context) (string, error)) (string, error) {
	g.mu.Lock()
	if c := g.call; c != nil {
		g.mu.Unlock()
		return c.wait(ctx)
	}
	c := &flightCall{done: make(chan struct{})}
	g.call = c
	g.mu.Unlock()

	// fn runs outside the lock: a slow refresh must not block a different
	// caller from *joining*, and it must not block the mutex that joining uses.
	token, err := fn(ctx)

	// Publish and clear under the lock, and close(done) last. A caller arriving
	// after this block starts a fresh execution — the correct behaviour, since
	// the token it would have joined for may already be superseded.
	g.mu.Lock()
	c.token, c.err = token, err
	g.call = nil
	close(c.done)
	g.mu.Unlock()

	return token, err
}

func (c *flightCall) wait(ctx context.Context) (string, error) {
	select {
	case <-c.done:
		return c.token, c.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
