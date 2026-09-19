// OCTO-FORK: the three product WS events that are not session-scoped, and the
// one that has to be replayed — see the current implementation plan §2 PR-8.
package server

import (
	"encoding/json"

	"github.com/open-octo/octo-agent/internal/datapath"
)

// The product's four WS events are split by scope, and the split is read off
// their consumers rather than chosen: three of them are consumed by
// web/src/App.svelte, which sits outside every session, and only
// input_sensitive belongs to one session's input box (it lives in sensitive.go
// for that reason).
//
// The emitters below are deliberately thin. Each event has exactly one trigger
// owner — the desktop shell's watchdog knows when the data root goes away, the
// turn tail knows when a turn ended — and this file owns only the wire shape,
// the scope, and the replay rule. A second place that decides *when* to fire
// would be a second answer to the same question (§3.8).

// BroadcastProductEvent sends a product event to every open window.
//
// Exported because the producer of datastore:lost/restored is the desktop
// shell's watchdog (cmd/octo-desktop), which runs in this process but in a
// different package. It is the exported form of broadcastGlobal and keeps the
// same contract, including nil-safety: a Server built without a hub (tests,
// non-WS deployments) can call it and nothing happens.
func (s *Server) BroadcastProductEvent(event any) {
	s.broadcastGlobal(event)
}

// replayProductState hands this connection the product state that events alone
// cannot tell it.
//
// Only datastore:lost has a replay, and it needs one. The freeze has two
// halves: the gate in internal/datapath refuses writes, and the overlay in the
// web UI tells the user. The overlay is driven by this event alone, so a window
// that connects or reloads *while* the data root is gone would otherwise start
// with frozen=false — no overlay, an input box that accepts typing — while
// every write is still refused. Replaying is what keeps the visible half and
// the enforced half describing the same moment.
//
// The truth is read straight off datapath.Frozen(): it is process-wide, and it
// is the same bit the watchdog flips, so there is no second detector and no
// cached copy to drift (§3.8). The other three events are deliberately not
// replayed — credits_update is a nudge to re-read the ledger (a cold start
// reads it anyway), datastore:restored describes the state a fresh connection
// is already in, and a refused input belongs to a turn that is over.
func (h *wsHub) replayProductState(conn *wsConn) {
	if !datapath.Frozen() {
		return
	}
	b, err := json.Marshal(map[string]any{"type": "datastore:lost"})
	if err != nil {
		return
	}
	select {
	case conn.send <- b:
	default:
		// Slow consumer. The window is behind on a full buffer, so it will
		// learn the truth from the next broadcast rather than from this
		// replay; blocking here would stall the hub's registration loop for
		// every other connection.
	}
}

// broadcastCreditsMayHaveMoved tells every window that the balance may have
// changed so each asks the ledger again — 需求基线 E9 rule 2 makes the ledger
// the only writer of that number.
//
// Global on purpose. The turn that spent the credits has just ended, and the
// window that ran it is subscribed to its session, so `complete` already
// reaches that one. But the balance is shown in the sidebar corner and the
// account panel, which live outside every session: a window looking at a
// *different* session shows the same number and gets nothing from `complete`.
// That gap is exactly the "which window receives it" question N-5 was opened
// for.
//
// The payload is empty by design. Carrying a balance would make this event a
// second writer of a number the ledger owns, and a stale or reordered push
// would overwrite a fresher read with no way to tell (web/src/App.svelte's
// handler documented that reasoning when it stopped merging the payload).
func (s *Server) broadcastCreditsMayHaveMoved() {
	s.broadcastGlobal(map[string]any{"type": "credits_update"})
}
