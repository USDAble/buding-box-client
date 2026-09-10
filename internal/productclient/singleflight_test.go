package productclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The single-flight exists for one failure: N concurrent 401s each calling
// Refresh, where the platform rotates the refresh token on use, so the first
// refresh invalidates what the other N-1 hold. Everyone fails and the user is
// logged out having done nothing wrong.

func TestSingleFlightCoalescesConcurrentCalls(t *testing.T) {
	var g singleFlight
	var runs atomic.Int32
	release := make(chan struct{})

	const callers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			token, err := g.Do(context.Background(), func(context.Context) (string, error) {
				runs.Add(1)
				<-release
				return "fresh", nil
			})
			if err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			if token != "fresh" {
				t.Errorf("token = %q, want %q", token, "fresh")
			}
		}()
	}
	close(start)
	// Give the goroutines a moment to pile up on the same flight; without this
	// the test could pass against a singleFlight that never actually coalesces,
	// because the first call would finish before the others arrive.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := runs.Load(); got != 1 {
		t.Fatalf("fn ran %d times, want exactly 1 — a rotating refresh token would be spent %d times", got, got)
	}
}

// TestSingleFlightRunsAgainAfterCompletion: coalescing must not become caching.
// A later expiry in the same session is normal and has to trigger a real refresh.
func TestSingleFlightRunsAgainAfterCompletion(t *testing.T) {
	var g singleFlight
	var runs atomic.Int32

	for i := 0; i < 3; i++ {
		if _, err := g.Do(context.Background(), func(context.Context) (string, error) {
			runs.Add(1)
			return "t", nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := runs.Load(); got != 3 {
		t.Fatalf("fn ran %d times, want 3 — the flight was cached, so a second expiry would reuse a dead token", got)
	}
}

// TestSingleFlightWaiterGivesUpWithoutAborting is the behaviour that keeps one
// impatient request from logging out everyone else: a waiter whose context ends
// returns its own error, but the refresh the others depend on still completes.
func TestSingleFlightWaiterGivesUpWithoutAborting(t *testing.T) {
	var g singleFlight
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		if _, err := g.Do(context.Background(), func(context.Context) (string, error) {
			<-release
			return "fresh", nil
		}); err != nil {
			t.Errorf("leader err = %v, want nil", err)
		}
	}()
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Do(ctx, func(context.Context) (string, error) {
		t.Error("the waiter started its own execution instead of joining")
		return "", nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter err = %v, want context.Canceled", err)
	}

	close(release)
	<-done
}

func TestSingleFlightPropagatesTheError(t *testing.T) {
	var g singleFlight
	boom := errors.New("refresh rejected")
	_, err := g.Do(context.Background(), func(context.Context) (string, error) { return "", boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}

	// A failed flight must not be remembered either: the next attempt has to be
	// able to succeed (the network may have come back).
	if token, err := g.Do(context.Background(), func(context.Context) (string, error) { return "ok", nil }); err != nil || token != "ok" {
		t.Fatalf("after a failure: token = %q, err = %v; want ok, nil", token, err)
	}
}
