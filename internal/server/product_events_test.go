// OCTO-FORK: nails for the three product events that are not session-scoped —
// see dev-docs-usdable/需求/20260911/开发计划.md §2 PR-8.
package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/datapath"
)

// windowConn registers a connection with the hub and waits until the hub has
// really taken it, so a test can read what a window would have received.
//
// Registration is what matters for the two product events here: they are
// broadcast globally, which reaches h.connections, while subscribeConn only
// puts a connection into a session's subscriber set. A window that never
// registered is invisible to a global broadcast, so a test built on the
// session-only helper would assert into the void.
//
// The wait is not politeness. Registration crosses a channel to the hub's own
// goroutine, so returning immediately would let a test broadcast before the hub
// has the connection — and the resulting "nothing arrived" reads exactly like a
// broken emitter.
func windowConn(t *testing.T, srv *Server, sid string) *wsConn {
	t.Helper()
	conn := &wsConn{hub: srv.wsHub, send: make(chan []byte, 256), subscribed: map[string]struct{}{}}
	srv.wsHub.register <- conn

	deadline := time.Now().Add(3 * time.Second)
	for {
		srv.wsHub.mu.Lock()
		_, ok := srv.wsHub.connections[conn]
		srv.wsHub.mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the connection never reached the hub's connection set")
		}
		time.Sleep(time.Millisecond)
	}
	if sid != "" {
		srv.wsHub.subscribe(conn, sid)
	}
	return conn
}

// eventsBeforeSentinel returns every event that reaches conn before a sentinel
// the test itself broadcasts, and fails if the sentinel never arrives.
//
// "Nothing was sent" needs a message that must come *after* the thing under
// test, or the claim is unprovable: reading the channel once right after
// registering can simply be too early, and that version passes on a build whose
// emitter is broken. The sentinel closes that hole — everything the registration
// path queued is ordered ahead of it, because one goroutine processes the
// registration and then the broadcast.
func eventsBeforeSentinel(t *testing.T, srv *Server, conn *wsConn) []map[string]any {
	t.Helper()
	srv.broadcastGlobal(map[string]any{"type": "sentinel"})

	var seen []map[string]any
	deadline := time.After(3 * time.Second)
	for {
		select {
		case b := <-conn.send:
			var ev map[string]any
			if err := json.Unmarshal(b, &ev); err != nil {
				t.Fatalf("unmarshal event: %v", err)
			}
			if ev["type"] == "sentinel" {
				return seen
			}
			seen = append(seen, ev)
		case <-deadline:
			t.Fatal("the sentinel never arrived — this connection receives no global events at all")
		}
	}
}

// nextEventOfType waits for the first event of that type.
func nextEventOfType(t *testing.T, conn *wsConn, want string) map[string]any {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case b := <-conn.send:
			var ev map[string]any
			if err := json.Unmarshal(b, &ev); err != nil {
				t.Fatalf("unmarshal event: %v", err)
			}
			if ev["type"] == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %q event", want)
			return nil
		}
	}
}

// TestTheCreditsNudgeReachesWindowsThatAreNotInTheSession is the nail for the
// scope decision: the balance is shown in the sidebar corner and the account
// panel, which live outside every session, so the window looking at a *different*
// session must be told too.
//
// A session-scoped event would satisfy the first assertion and fail the second —
// which is exactly the "which window receives it" bug this event was opened for,
// and why the global form cannot be proven with one connection.
func TestTheCreditsNudgeReachesWindowsThatAreNotInTheSession(t *testing.T) {
	dataRoot(t)
	srv := newFilteredServer(t, &maskingSender{reply: agent.Reply{Content: "ok"}})
	sess := savedSession(t, "余额")

	inSession := windowConn(t, srv, sess.ID)
	elsewhere := windowConn(t, srv, "")

	// Driven through runAgentTurnLoop, which is the path every window-visible
	// turn takes (WS user_message, IM channels, background tasks) and the one
	// that ends by broadcasting `complete`. The two synchronous REST turn
	// endpoints use runTurn instead, which broadcasts nothing at all — no
	// `complete`, so no nudge either; a window never learns of those turns, so
	// there is no stale number to correct.
	srv.runAgentTurnLoop(sess, "你好", nil, nil)

	for name, conn := range map[string]*wsConn{"the turn's own window": inSession, "a window in another session": elsewhere} {
		ev := nextEventOfType(t, conn, "credits_update")
		// The payload is empty on purpose: carrying a balance here would make
		// this event a second writer of a number the ledger owns (E9 rule 2),
		// and a stale push would overwrite a fresher read.
		if _, hasCredits := ev["credits"]; hasCredits {
			t.Errorf("%s: credits_update carries a `credits` payload — the ledger is the only writer of that number", name)
		}
		if len(ev) != 1 {
			t.Errorf("%s: credits_update = %v, want only the type field", name, ev)
		}
	}
}

// TestAWindowThatConnectsWhileFrozenIsToldTheRootIsGone is the nail for the
// replay, and for L-E3's visible half.
//
// The overlay in the web UI is driven by this event alone, so a window that
// connects while the data root is gone would otherwise start with frozen=false:
// no overlay, an input box that accepts typing — while every write is still
// refused. Replaying is what keeps the visible half and the enforced half
// describing the same moment.
func TestAWindowThatConnectsWhileFrozenIsToldTheRootIsGone(t *testing.T) {
	dataRoot(t)
	srv := newFilteredServer(t, &maskingSender{reply: agent.Reply{Content: "ok"}})

	// A window that was already open when the disk was pulled: it gets the
	// event from the watchdog's transition, not from a replay.
	alreadyOpen := windowConn(t, srv, "")

	datapath.Thaw()
	t.Cleanup(datapath.Thaw)
	datapath.Freeze()

	latecomer := windowConn(t, srv, "")
	if ev := nextEventOfType(t, latecomer, "datastore:lost"); len(ev) != 1 {
		t.Errorf("datastore:lost = %v, want only the type field", ev)
	}

	// The replay is for the connection that missed the transition, not a
	// re-broadcast: the window that was already open must not receive a second
	// datastore:lost here (its own event came from the watchdog, once).
	if seen := eventsBeforeSentinel(t, srv, alreadyOpen); len(seen) != 0 {
		t.Errorf("a window that was already open received %v — the replay must target the new connection only", seen)
	}
}

// TestAWindowThatConnectsWhileHealthyIsNotToldAnything is the reversal nail:
// the replay answers "is the root gone", not "is this a new connection".
//
// Without it, an unconditional replay (`always send datastore:lost on connect`)
// would freeze the UI of a perfectly healthy product and every other nail here
// would still pass.
func TestAWindowThatConnectsWhileHealthyIsNotToldAnything(t *testing.T) {
	dataRoot(t)
	srv := newFilteredServer(t, &maskingSender{reply: agent.Reply{Content: "ok"}})

	datapath.Thaw()
	t.Cleanup(datapath.Thaw)

	conn := windowConn(t, srv, "")
	if seen := eventsBeforeSentinel(t, srv, conn); len(seen) != 0 {
		t.Errorf("a fresh connection on a healthy root received %v, want nothing", seen)
	}
}

// TestBroadcastingAProductEventWithoutAHubIsNotAPanic pins the contract the
// desktop shell depends on while it is starting up: the watchdog is armed while
// the server may not be constructed yet.
func TestBroadcastingAProductEventWithoutAHubIsNotAPanic(t *testing.T) {
	srv := mustServer(t, Config{Addr: "127.0.0.1:0"})
	// mustServer builds a hub, so the absence is staged deliberately rather than
	// hunted for: what is under test is broadcastGlobal's nil contract, and the
	// point is that the emitter keeps it instead of growing its own.
	srv.wsHub = nil
	srv.BroadcastProductEvent(map[string]any{"type": "datastore:lost"})
}
