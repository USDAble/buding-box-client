// The product-wide WS events: the data-root freeze, and the ledger nudge.
//
// These three handlers used to live inline in App.svelte's bootMain, where no test
// could reach them. V-97 registered the consequence: the whole consuming half of the
// freeze chain (event → `frozen` store → FrozenOverlay) had ZERO tests, while the
// server side had four nails. The server could emit a perfectly correct
// datastore:lost and a handler that never ran — or ran on a misspelt event name —
// and every one of those nails would stay green. That is the shape L-D2 was burned
// by ("the wiring point was wrong and every test stayed green"), so the mapping moves
// here, where a test drives it with a literal event body the way the socket would.
//
// Scope: these are the events consumed OUTSIDE any session, which is why they are
// global — the balance is shown in the sidebar corner and the account panel, and the
// overlay covers the whole window. input_sensitive is the one session-scoped product
// event and belongs with the session wiring, not here.
import { ws } from './ws'
import { frozen } from './stores'
import { refreshCredits } from './product'

// wireProductEvents registers the three global product handlers.
//
// Registered once, for the page's lifetime, so unlike wireMobileSession there is no
// cleanup to return: the mobile shell re-wires per session inside an $effect and must
// take its handlers back off, while the desktop shell's handlers live exactly as long
// as the socket does.
export function wireProductEvents(): void {
  // Portable data-root freeze: the shell's watchdog broadcasts datastore:lost when
  // the data/ directory vanishes (a U盘 pulled out) and datastore:restored when the
  // SAME path returns. `frozen` drives the full-screen FrozenOverlay and disables all
  // input; only a restore — or the overlay's Quit — clears it.
  //
  // The server replays datastore:lost to a connection that arrives while the root is
  // gone (internal/server/product_events.go). That replay is the only reason a reload
  // during a freeze still shows the overlay, so this must be registered in the same
  // synchronous stretch as ws.connect() — not behind an await the socket could beat.
  ws.on('datastore:lost', () => { frozen.set(true) })
  ws.on('datastore:restored', () => { frozen.set(false) })

  // The balance may have moved: ask the ledger (需求基线 E9 rule 2).
  //
  // The payload is deliberately NOT merged into anything. Before 2026-09-14 this
  // handler copied `ev.credits` straight into productState, which made it a second
  // writer of a number the ledger already owns — and a stale or reordered event would
  // have overwritten a fresher read with no way to tell. The event is a TRIGGER; the
  // number comes from refreshCredits() and nowhere else.
  // OCTO-FORK: P6 credits — see
  // the approved navigation and credits boundary.
  ws.on('credits_update', () => {
    void refreshCredits().catch(() => {
      // Swallowed on purpose: this is an unsolicited refresh the user did not ask
      // for, the number on screen keeps its last value, and the points page reports a
      // failure at the moment the user asks (the account corner refresh action).
    })
  })
}
