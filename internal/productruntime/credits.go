package productruntime

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/open-octo/octo-agent/internal/productstate"
)

// refreshCredits reads the platform ledger and writes the balance projection.
//
// IT IS THE ONLY WRITER OF THE BALANCE, and that is the whole design
// (需求基线 E9 rule 2, decided by a human 2026-09-14): the server deducts, the
// client honestly re-reads. Every other signal that the number may have moved - a
// settled terminal frame, the end of a turn, window focus, the credits page
// opening - is a reason to call this function, never a carrier of the number.
//
// WHY THAT IS THE SIMPLER CONTRACT, having chosen the busier-looking option. The
// alternative was two sources: take the balance off the terminal frame when one
// arrives and fall back to this read when it does not. That shape needs an
// arbitration rule for "which of the two wins", a story for a lost or reordered
// frame, and two acceptance criteria that disagree while both are half-true -
// which is exactly the state V-11 left the requirement in. One source has none of
// those questions, and the price is one extra round-trip per trigger. It also
// unbinds the balance from D-002: the gateway's terminal-frame shape is still
// undefined, and this path no longer waits for it.
//
// A REFUSED READ CHANGES NOTHING. Transport failures and 5xx have no verdict in
// them (V-43), and an answer without a balance is refused by the client
// (productclient.ErrLedgerMalformed) rather than read as zero - zero is the most
// likely legitimate value here, since it is what makes the gateway answer 402, so
// reading absence as zero would tell a paying user they are out of credits. The
// stored value stays what the platform last said it was.
//
// It returns the value it wrote so a caller can report it without a second read
// of the file it just wrote.
func (rt *Runtime) refreshCredits(ctx context.Context) (productstate.Credits, error) {
	ledger, err := rt.deps.Platform.CreditsLedger(ctx)
	if err != nil {
		return productstate.Credits{}, err
	}
	credits := productstate.Credits{
		Known: true,
		// Safe to dereference: CreditsLedger refuses an answer that has none, so
		// the zero-versus-absent question is settled inside the client, where the
		// wire shape lives, and not here.
		Balance: *ledger.BalanceMicroCredits,
	}
	if err := rt.deps.State.SetCredits(credits); err != nil {
		return productstate.Credits{}, err
	}
	return credits, nil
}

// handleCredits is the balance refresh endpoint (本地API契约 §2.15).
//
// WHY IT EXISTS RATHER THAN BEING FOLDED INTO ANOTHER ANSWER. The balance has one
// source and one write path; the login answer also carries a `credits` object,
// and reading the balance out of it would be a second path with a second chance
// to disagree. So login calls the same function this endpoint calls, and pays one
// round-trip for it.
//
// The answer is the whole state object, like putNickname and putPrefs: the caller
// has just written a field, and what it needs back is the persisted truth rather
// than a second shape saying the same thing (开发规范 §3.8). It is deliberately
// not the bare balance - that would be a second spelling of the credits object
// that 本地API契约 §1.3 already owns.
func (rt *Runtime) handleCredits(w http.ResponseWriter, r *http.Request) {
	if rt.deps.Platform == nil {
		writeControlPlaneUnconfigured(w)
		return
	}
	if _, err := rt.refreshCredits(r.Context()); err != nil {
		// Through the shared funnel: a refused session clears the credential and
		// returns to the blocked page (L-A6), and every other failure keeps its
		// platform code and leaves the projection untouched. Logged because a
		// screen that keeps showing an old number is otherwise invisible.
		slog.Warn("credits: the balance was not refreshed", "error", err)
		rt.failPlatform(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": rt.deps.State.PublicState()})
}
