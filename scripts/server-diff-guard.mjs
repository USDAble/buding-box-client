// scripts/server-diff-guard.mjs
//
// `internal/server` is an upstream file tree that this fork must keep
// mergeable. Two kinds of drift destroy that property, and neither one is
// visible in review because every individual change looks reasonable:
//
//   R1  The upstream route table rewritten line by line. P3 wanted a "product
//       gate" in front of ~150 routes, and the first attempt expressed it by
//       editing upstream's registrar call sites — each one a permanent merge
//       conflict. The approved design (P0-01A C) is a registrar the fork hands
//       to Config.MountAPI instead, so the target is zero added `s.api(` lines.
//
//   R2  Product logic parked in the upstream tree. `product_*.go`,
//       `chatmode_handlers.go`, `privacy.go`, `sensitive_dict_handlers.go` are
//       new files that upstream will never have; they are supposed to move to
//       internal/productruntime (P0-01A), not to accumulate here.
//
// The guard is a **ratchet**: it records today's debt as a ceiling that may
// only shrink. Adding a change that grows the debt fails CI, and the fix is to
// *reduce* the recorded number (as P0-01A does), not to raise it. Raising a
// ceiling is an explicit, reviewable edit to this file — see 开发规范 §3.4
// (破例流程) and §3.7 (人工确认清单).
//
// A ceiling is a *budget*, so an unearned one is worse than none: it silently
// authorises that much growth. Two of the six were 36 and 31 lines for files
// this fork does not modify at all, and one was 33 lines of slack above the
// real number. All three were tightened to the measured value on 2026-09-13
// (V-49), which is the direction the ratchet only ever allows.
//
// Why a pinned commit rather than a fetched branch: a ref that moves makes the
// verdict a function of when the job ran, which is how every branch in the repo
// went red on 2026-09-16 without any of them touching internal/server. The pin
// lives in scripts/upstream-baseline.txt, with the incident and the procedure
// for advancing it written next to the value. `main` is still the branch the
// fork merges from; the pin is what it is measured against.
//
// Usage:
//   node scripts/server-diff-guard.mjs
//
// No npm dependencies. Run it in CI (.github/workflows/go.yml,
// server-diff-guard job) and locally via `make server-diff-check`.

import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { isMarkerLine } from './fork-marker.mjs'

// The upstream commit the ratchets are measured against. It is a *pin*, not a
// ref: resolving `origin/main` at run time made the verdict depend on when the
// job ran rather than on the branch under review — see the header of
// scripts/upstream-baseline.txt for the 2026-09-16 incident that proved it.
export const UPSTREAM_BASELINE_PATH = 'scripts/upstream-baseline.txt'

// A commit id, full length and lowercase, as `git rev-parse` prints it. Short
// ids are rejected: a pin that can be ambiguous is not a pin.
export const BASELINE_PATTERN = /^[0-9a-f]{40}$/

// Directories the guard governs: the upstream runtime the fork must not grow.
export const GOVERNED_PREFIX = 'internal/server/'

// WHAT IS AND IS NOT RATCHETED — the scope, stated here because the two are
// easy to conflate and the conflation is silent (开发规范 §3.10).
//
//   R1 counts added `s.api(` lines in server.go: one number, ceiling 2.
//   R2's per-file ceilings cover a *named handful* of files — the ones carrying
//     product debt that has a convergence plan. Everything else under
//     GOVERNED_PREFIX is unratcheted *by design*: no drift ceiling, no
//     comparison, however large the fork's diff against upstream grows.
//   R2's registration rule covers fork-ADDED files (PRODUCT_FILE_PATTERN): such
//     a file must be listed in PRODUCT_FILES with a convergence plan.
//
// The count of fork-modified upstream files with no ceiling is measured at run
// time and printed in the coverage note, not written here: a frozen number goes
// stale on the next change while still reading as current (§3.8), which is the
// defect V-101 registered. What the guard must not do is let a pass read as a
// statement about the whole tree — so `main` names the scope in its pass line.
//
// R1 — the route table.
//
// Measured 2026-09-13: upstream main has 147 `s.api(` calls in server.go and
// this branch has 148. The single added line is
//   s.api("PATCH /api/sessions/{id}/chat_mode", s.handleUpdateSessionChatMode)
// which PR-4d1 needed to persist the session-level chat mode (V-46). The
// ratchet target is 0: P0-01A C folds it into the registrar the fork hands to
// Config.MountAPI, which needs no upstream call site at all.
//
// WHY THIS REPLACED A COUNT OF `apiProduct`. Until 2026-09-13 this check
// counted `apiProduct` occurrences against a baseline of 161, described as
// "157 call sites". `apiProduct` does not exist — it appears 0 times upstream
// and 0 times here; the registrar is `productAPI`, handed to MountAPI as a
// method value. Measured at `HEAD` it was 0, so the check took the "target
// reached" branch and printed
//
//   apiProduct is gone — P0-01A fold complete, the ratchet target is reached.
//
// on every run, while the risk it was written to catch stayed exactly as live.
// A guard aimed at a symbol that does not exist cannot fail, and a guard that
// cannot fail is a sentence, not a check (V-49). This version cannot no-op:
// `upstreamCount` is asserted to be non-zero, so if `s.api(` ever stops being
// upstream's own symbol the premise breaks loudly instead of silently passing.
export const ROUTE_TABLE_FILE = 'internal/server/server.go'
export const ROUTE_TABLE_CEILING = 2

// R2 — per-file fork diff (added + removed lines vs upstream) ceilings for the
// upstream files that carry real product debt. Merging upstream shrinks these
// automatically, so a *lower* number is always safe; only growth fails.
//
// `convergence` names the work package that is supposed to shrink it, or says
// plainly that nothing will — an honest "this is the finished cost of hard rule
// 1" is more useful than a work package that does not exist.
//
// Three raises have happened. They are the bar to judge the next one by:
//
//   server.go 484 → 543 for the host SenderFactory port (P0-01 B1, P0-01A
//   §3.1). Raised rather than avoided because the port is an explicitly
//   approved architectural change and no smaller form exists (the `Config`
//   field is the only construction channel; the alternative the guard
//   suggested — a package-level setter read by the server — is hidden global
//   state, which is a real regression, not a smaller diff). The reason for the
//   *policy* lines was moved out of the file into the fork-owned package first
//   (−23 lines before the ceiling was touched).
//
//   handlers.go 83 → 91 for `sessionItem.ChatMode` (PR-4d1, V-46).
//   (a) what the 8 lines are: one field on the session descriptor the WebUI
//       reads plus the assignment in its builder. Without them the session
//       list cannot report the mode, and the "reload still shows it" half of
//       L-C8 is unreachable (web/src/components/chat/Composer.svelte reads
//       currentSession?.chat_mode).
//   (b) no smaller form: sessionItem is declared in handlers.go and
//       toSessionItem is its only builder. There is no seam for "add a field to
//       a response struct" — the alternative, a fork-side wrapper re-emitting
//       the session JSON, would be a second place defining the same shape
//       (开发规范 §3.8) and strictly more code.
//   (c) the bulk was moved out first: the ~85-line handler lives in the
//       fork-owned internal/server/chatmode_handlers.go — a file this guard
//       already registers — which is what turned "grew by 87" into "grew by 8".
//       The remaining 8 fold with P0-01A D, when the descriptor moves too.
//
//   server.go 543 → 510 (V-49, 2026-09-13): a *lowering*, to the measured
//   value. The 33 lines above it were slack, and slack is silent permission to
//   grow by that much.
//
//   server.go 510 → 496, handlers.go 91 → 88, attachments.go 27 → 25,
//   lightapps_handlers.go 14 → 13 (V-49, 2026-09-13): another *lowering*, after
//   `forkDiffLines` stopped counting OCTO-FORK marker lines as debt. The
//   measured marker lines inside governed files were 14/3/2/1. This is the one
//   case where the number moves for a reason that is not a code change: hard
//   rule 3 *mandates* those lines, so a ratchet that charged for them would
//   make the two guards contradict (see scripts/fork-marker.mjs). The ceilings
//   were re-measured rather than left with slack, so the tightening is visible
//   here instead of silently absorbed.
//
//   server.go 496 → 533 for the turn guard's catalog predicate (PR-5e,
//   2026-09-14). The fourth raise, judged by the same three points:
//
//   (a) what the 37 lines are: one nilable `Config` field (2 lines of Go, the
//       rest the prose a caller cannot do without — a two-valued answer whose
//       second half is the whole reason it is not a bool), a 5-line guard inside
//       senderForSession, and one error constructor. Every one of the 37 is
//       comment or declaration; no logic was added to upstream's own control flow.
//   (b) no smaller form: the guard has to sit in senderForSession because that is
//       the single funnel every turn path goes through, and it has to sit BEFORE
//       the gateway factory — building a sender can exchange a token
//       (GatewayEndpoint.Sender), so checking afterwards spends a network round
//       trip on a turn the build already knows cannot be served. `Config` is the
//       only construction channel (the same argument as P0-01 B1). The alternative
//       the guard suggests by default — a fork-side wrapper sender — is the option
//       that got rejected explicitly: it would have to forward the whole capability
//       stack (StreamingSender/ToolSender/ToolStreamingSender), and missing one
//       silently downgrades streaming, which is a worse defect than the one the
//       wrapper would prevent.
//   (c) the bulk was moved out first: the reasoning lives in
//       dev-docs-usdable/需求/20260911/开发计划.md §PR-5e, and trimming the three
//       comment blocks to pointers took the measured diff from 550 to 533 before
//       the ceiling was touched (−17). The remaining 37 are not slack: they are
//       the feature's floor for this file.
//
//   server.go 517 → 531 for the compliance filter's assembly (PR-6a,
//   2026-09-14). The fifth raise, judged by the same three points:
//
//   (a) what the 14 lines are: one import, one `*sensitive.Engine` field, its
//       assignment in the constructor, three one-line changes at the places a
//       turn's agent is built (each now goes through `s.wrapSensitive`), and the
//       same wrap on the resolved lite sender. Three of the fourteen are a
//       modified line, which counts as a fork line. No logic was added to
//       upstream's own control flow: the wrapper, the engine and the reasoning
//       all live in fork-owned files (internal/app/sensitive_sender.go,
//       internal/server/sensitive.go, internal/sensitive/).
//   (b) no smaller form: an agent is built where it is built, and the three sites
//       are server.go's own. The seam one level up — `senderForSession` — was
//       rejected for a measured reason: four upstream assertions pin that
//       function's IDENTITY (server_test.go:1970/:1980 assert the plain path
//       hands back `srv.sender` itself; gateway_route_test.go:72 asserts the
//       gateway path hands back the injected sender; gateway_model_guard_test.go
//       :153 asserts the opposite), and those tests are about ROUTING, so
//       accommodating a filter would mean editing four tests to say something
//       they do not mean. The other alternative — wrapping at the sender's
//       source — is a silent hole rather than a smaller form: the product's own
//       turn path does not use the startup sender at all (a gateway-bound turn
//       gets one built per turn, GatewayEndpoint.Sender), so wrapping there would
//       leave every product turn unfiltered while the whole suite stayed green.
//   (c) what was moved out first: the engine, its construction, the data-root
//       resolution and the explanation are in the fork-owned
//       internal/server/sensitive.go; the prose at the four call sites was cut to
//       one to three lines each (−19 measured before the ceiling was touched), so
//       what the ceiling records is the wiring and not the argument for it.
//
//   server.go 565 → 576 for the shutdown joins (V-105, 2026-09-16). The ninth
//   raise, judged by the same three points:
//
//   (a) what the 11 lines are: three `Server` fields — the channel the store
//       watch closes when it returns, the `atomic.Bool` that lets a join skip
//       itself when the watch never ran, and the nil-in-production test seam
//       that makes the ordering observable at all — plus their three comment
//       lines, the one `make(chan struct{})` in `New`, and the two call lines in
//       `doShutdown`. Six lines of Go, five of prose.
//   (b) no smaller form: the fields have to be declared on `Server`, which is
//       declared here, and `Config` is the only construction channel — the same
//       argument as P0-01 B1 and the PR-5e raise. `doShutdown` is upstream's own
//       method, so a fork-side wrapper cannot reach it. `watchStop` is the ask;
//       a join needs the second channel that answers, and "the watcher has
//       returned" is not learnable from a channel whose only writer is the side
//       doing the waiting. Measured: stripping every comment still leaves six
//       lines (571), so even the comment-free form breaches the old 565.
//   (c) the bulk was moved out first: the substance of these two joins is in
//       `internal/server/store_watch.go` (+48) and
//       `internal/server/tasks_handlers.go` (+34), neither of which this guard
//       ratchets. What reaches this file is the wiring that has nowhere else to
//       go.
//
//   The 2026-09-18 personal-information pipeline raises server.go 576 → 660,
//   handlers.go 101 → 280, ws_handlers.go 26 → 105 and the route ceiling
//   1 → 2. Measured values are 646/265/91 and 2; the explicitly approved
//   14/15/14-line margin is small headroom, not a budget for unrelated work.
//   Rule logic, error/event construction, the product transform endpoint and
//   tests remain in fork-owned files. The upstream files only carry the
//   Config/Server seam and calls at existing REST/WS entry points, where the
//   transform must run before queue, broadcast, persistence or model work.
//   A sender wrapper would run too late. The second route is the atomic session
//   protection endpoint; product routes still use Config.MountAPI.
//
//   NOTE ON THE PR-5e PRECEDENT. That raise rejected a fork-side wrapper sender
//   because forwarding the capability stack by hand can silently downgrade
//   streaming. This wrapper is the same shape and is still the right answer,
//   because that failure mode is excluded by construction rather than promised:
//   `filteringSender` carries compile-time assertions for all six sender
//   interfaces (internal/app/sensitive_sender.go), so failing to forward one is a
//   build error instead of a runtime degradation.
//
//   A raise that cannot answer those three points should be a fold instead.
export const DEBT_CEILINGS = [
  {
    file: 'internal/server/server.go',
    ceiling: 660,
    why:
      'the product seam and the data-root migration: Config.MountAPI/WindowToken/RequireGateway/ControlPlaneReady plumbing, the productAPI registrar (a method value, not a call site), V-36/PR-5c/PR-5b1 gates on the turn path, and Config.CatalogOffers + its guard (PR-5e, L-C7). ' +
      'Measured 2026-09-14 at 533 after excluding marker lines (see the marker note in forkDiffLines) and after trimming the PR-5e prose to pointers into 开发计划 §PR-5e; of the added lines the large majority are prose explaining those seams. ' +
      'PR-5d3 folded 16 of them out: errModelNotListed and its doc comment moved to internal/server/turn_refusal.go when the refusal gained a code (G3), and the ceiling came down with the fold rather than keeping the headroom. ' +
      'PR-6a added 14: the import, the sensitiveEngine field, its assignment, the three agent-construction sites that now go through s.wrapSensitive, and the same wrap on the resolved lite sender — see the fifth raise note above for why no smaller form exists. ' +
      'PR-6b1 added 15 (seventh raise), all of it Config.SensitiveEngine: the field, its documentation, and the one-line assignment that now reads it. ' +
      'It cannot be folded out the way PR-5d3 folded errModelNotListed: a Config field is where a build hands this package a dependency it may not import, so it has to be declared on the struct that receives it — the alternative is a second engine built here, which is the defect the field exists to prevent (two readers of data/sensitive-words.txt, so the screen and the word-list routes could disagree; see 开发计划 §PR-6b1). ' +
      'The prose in those 15 lines is the three questions the header asks: why this package rather than a fork-owned one (the type is upstream s), why not nil-defaulted instead of injected (nil then means a second engine, and the fallback for `octo serve` already covers the no-injection case), and why no smaller form exists (a field plus one call). ' +
      'PR-6b3 added 19 (eighth raise), all of it Config.SensitiveInputGate: the field and its documentation. ' +
      'WHY IT CANNOT BE FOLDED OUT: the gate has to fire where the three turn entry points are, and those live in this package; the same shape as SensitiveEngine above, and for the same reason — a Config field is where a build hands this package a dependency it may not import. The alternative the archived implementation used, letting internal/server hold a *productstate.Store and read the switch itself, is forbidden twice over: this package may not import a fork package, and a user preference is not this package\'s fact to own (开发规范 §3.8). The other alternative, decorating the sender, was rejected by measurement, not taste: the WS path broadcasts AND persists the user message before buildAgent runs, so a refusal at send time would leave a question in the transcript that was never asked (开发计划 §PR-6b3). ' +
      'WHAT WAS MOVED OUT FIRST: the three verdict helpers (sensInputVerdict, refuseSensitiveInput, broadcastInputSensitive) went into the fork-owned internal/server/sensitive.go rather than beside their call sites, and the call-site prose was cut to one to four lines each; what is left here is the field plus the doc answering the three header questions. ' +
      'WHY NO SMALLER FORM: the field is one line and the doc is where the next reader learns that a nil gate is the CLI shape and that the ORDER (refuse before broadcast/persist) is the requirement rather than an implementation detail — the PR-6b1 precedent for the same seam. ' +
      'V-105 added 11 (ninth raise), all of it the shutdown joins: the watchDone channel the store watch closes on its way out, the watchStarted flag that skips the join when the watch never ran, the test seam that makes the ordering observable, the make() for the channel, the two call lines in doShutdown, and five lines of prose — see the ninth raise note above. ' +
      'WHY NO SMALLER FORM: the fields are on Server because that is where the struct is declared and Config is the only construction channel; doShutdown is upstream\'s own method, so no fork-side wrapper reaches it; and watchStop is the ask, while a join needs the channel that answers. Stripping every comment still measures 571, over the previous 565 — so this raise could not have been avoided by trimming prose. ' +
      'WHAT WAS MOVED OUT FIRST: both joins themselves live in internal/server/store_watch.go (+48) and internal/server/tasks_handlers.go (+34), neither of which this guard ratchets. Only the wiring lands here. ' +
      'The 2026-09-18 raise adds the PersonalInfoTransform construction seam and includes earlier protection-policy wiring. Rules and preprocessing live in fork-owned files; Config and Server are the only injection boundary. The 660 ceiling is 14 lines above the measured 646 and is not permission for unrelated behavior.',
    convergence:
      'P0-01A C (the apiProduct fold is dead — see the R1 note; what remains is the registrar and the product-state move, P0-01A D). PR-5e adds nothing to fold: its 37 lines are the floor for a turn-path guard, and they shrink only if upstream grows a pre-send hook. The 11 V-105 lines shrink only if upstream joins its own background goroutines on Shutdown — the join belongs upstream, and this is the fork paying for it in the meantime',
  },
  {
    file: 'internal/server/handlers.go',
    ceiling: 280,
    why:
      'the data-root migration (a datapath import and the directory joins) plus sessionItem.ChatMode (PR-4d1, V-46 — see the raise notes above). ' +
      'PR-6b3 added 13: two input-gate call sites, one in handleCreateChat (before the session is minted, so a refused message leaves no session file) and one in handleTurn (before the session binding is taken, so a refused message does not take the session over). ' +
      'WHY NO SMALLER FORM: the two REST turn entry points are both in this file, so the gate has to be named twice; the alternative, one call inside runTurn, is the WRONG ORDER — that is after the session exists, and for handleCreateChat it would leave a session file behind for a question that was never asked (需求 D1, 开发计划 §PR-6b3). ' +
      'WHAT WAS MOVED OUT FIRST: the verdict itself (the switch plus the engine) is not here at all — it arrives as Config.SensitiveInputGate from the fork-owned assembly, and the refusal writer lives in internal/server/sensitive.go. These 13 lines are the wiring and the three-to-four-line marker each, nothing more. ' +
      'The 2026-09-18 raise covers authoritative preprocessing at the existing create, turn and edit entry points plus the protection-policy response fields. The engine and error/event builders stay in fork-owned files. Moving these calls into runTurn would be unsafe because create and edit side effects can happen first. The 280 ceiling is 15 lines above the measured 265.',
    convergence: 'P0-01A D (+8 folds when the session descriptor moves); the datapath lines are the finished cost of hard rule 1',
  },
  {
    // Raised 0 → 15 by PR-5d3 (2026-09-14), the first change this fork has ever
    // made to this file. The 0 was doing its job: it forced the three justifications
    // below rather than letting a 36-line allowance hide them (V-49).
    // Raised 15 → 23 by PR-6b3 (2026-09-15), the second change.
    file: 'internal/server/ws_handlers.go',
    ceiling: 105,
    why:
      'G3 (需求基线 C8): the control plane\'s error code on the turn_error event. Composition at the measured 15 — 2 added + 3 removed are the `errorInput` signature and `error`\'s call line; 3 are the `if code != "" { ev["code"] = code }` that puts the field on the wire; 4 are the two `userError`/`userErrorInput` call lines (each forwards agent.ErrorCodeOf(err)); 3 are prose plus the marker. ' +
      'WHY NO SMALLER FORM: the field can only be added where the event is built, and once a fourth parameter exists the three call sites in this file must pass it — the alternative (a second emitter function) measured 22 lines, not fewer. The code has to be read from the error object, which exists only in the two userError* callers. ' +
      'WHAT WAS MOVED OUT FIRST: nothing was available to move. The file had zero fork lines, so there was no accumulated bulk to fold into internal/server/turn_refusal.go — that is where PR-5e\'s refusal moved TO in the same PR, and it took server.go from 533 to 517 in the same change (see that entry). ' +
      'PR-6b3 added 8: the input-gate call in handleWSUserMessage, before the session binding is taken and before the user message is broadcast or persisted, plus its three-line marker. ' +
      'WHY NO SMALLER FORM for that 8: this is the only WS entry point a typed message passes through, and the position is the requirement — the two lines below it in the same function broadcast `history_user_message` and append to the session, so a gate anywhere later is a refusal of something the transcript already shows (开发计划 §PR-6b3; this is also why the sender-wrapper alternative was measured and rejected). ' +
      'WHAT WAS MOVED OUT FIRST for it: the verdict and both response shapes (the WS broadcast and the REST refusal writer) are in the fork-owned internal/server/sensitive.go; what is here is the call and the marker. ' +
      'PR-8 added 3: the global credits nudge at the turn tail, next to the session_activity companion that already exists there for the same reason (completeEvent reaches only subscribers of this session, while the balance is shown in the sidebar corner and account panel, which live outside every session). ' +
      'WHY NO SMALLER FORM: the nudge has to fire where a turn is known to be over, and every turn ends in this function; the emitter itself is one line in the fork-owned internal/server/product_events.go, so what is here is the call and its three-line marker. ' +
      'The 2026-09-18 raise covers preprocessing before the running Agent inbox, chained queue and idle turn. Shared logic stays in fork-owned user_text_preprocess.go; these calls must remain before enqueue or broadcast. The 105 ceiling is 14 lines above the measured 91.',
    convergence:
      'shrinks only if upstream grows a code channel on turn_error (or a pre-send hook that carries one) and a global companion hook on the turn tail; otherwise this is the permanent cost of the requirement that the client must not render `message` and that the balance refreshes in every window',
  },
  {
    file: 'internal/server/native_handlers.go',
    ceiling: 0,
    why: 'this fork does not modify this file; the recorded debt was a 31-line allowance for a change that is not there',
    convergence: 'nothing to converge — keep it at 0',
  },
  {
    file: 'internal/server/attachments.go',
    ceiling: 25,
    why: 'the data-root migration: the datapath import and the uploads directory join (hard rule 1)',
    convergence: 'nothing pending — this is the finished cost of hard rule 1; it shrinks only if upstream rewrites the file',
  },
  {
    file: 'internal/server/lightapps_handlers.go',
    ceiling: 13,
    why: 'the data-root migration: the datapath import and the light-apps directory join (hard rule 1)',
    convergence: 'nothing pending — this is the finished cost of hard rule 1',
  },
]

// R2 — files that exist ONLY because of the fork's product layer and are
// scheduled to leave `internal/server` entirely (P0-01A B/C/D). A file matching
// PRODUCT_FILE_PATTERN that is not listed here is a new product file parked in
// the upstream tree: fail, and either register a convergence plan or put the
// code where it belongs.
export const PRODUCT_FILE_PATTERN = /^(product_.*|privacy|chatmode_handlers|sensitive_dict_handlers)(_test)?\.go$/

export const PRODUCT_FILES = [
  { file: 'internal/server/product_handlers.go', convergence: 'P0-01A B' },
  { file: 'internal/server/product_handlers_test.go', convergence: 'P0-01A B' },
  { file: 'internal/server/product_login.go', convergence: 'P0-01A B + P0-02' },
  { file: 'internal/server/product_login_test.go', convergence: 'P0-01A B + P0-02' },
  { file: 'internal/server/product_nickname.go', convergence: 'P0-01A B' },
  { file: 'internal/server/product_prefs.go', convergence: 'P0-01A B' },
  { file: 'internal/server/product_sensitive.go', convergence: 'P0-01A B' },
  { file: 'internal/server/product_sensitive_test.go', convergence: 'P0-01A B' },
  { file: 'internal/server/product_account_panel_test.go', convergence: 'P0-01A B' },
  { file: 'internal/server/product_credits_test.go', convergence: 'P0-01A E (credit path deleted, not moved)' },
  { file: 'internal/server/product_events.go', convergence: 'P0-01A B' },
  { file: 'internal/server/product_events_test.go', convergence: 'P0-01A B' },
  { file: 'internal/server/sensitive_dict_handlers.go', convergence: 'P0-01A B + P0-06' },
  { file: 'internal/server/sensitive_dict_handlers_test.go', convergence: 'P0-01A B + P0-06' },
  { file: 'internal/server/chatmode_handlers.go', convergence: 'P0-01A D + P0-04' },
  { file: 'internal/server/chatmode_handlers_test.go', convergence: 'P0-01A D + P0-04' },
  { file: 'internal/server/privacy.go', convergence: 'P0-01A D + P0-06' },
  { file: 'internal/server/privacy_test.go', convergence: 'P0-01A D + P0-06' },
  { file: 'internal/server/product_gate_test.go', convergence: 'P0-01A C (the MountAPI seam folds with the registrar)' },
]

// ─── pure analyzers (unit-tested) ───────────────────────────────────────────

// analyzeRouteTable enforces R1.
//
// `added` is how many `s.api(` lines this branch adds to upstream's registrar.
// `upstreamCount` is how many upstream has; it must be non-zero, or the symbol
// stopped being upstream's and this check would be measuring nothing (the
// failure that made the previous version of R1 unable to fail).
export function analyzeRouteTable({ added, upstreamCount, ceiling, total }) {
  const problems = []
  const notes = []

  if (upstreamCount === 0) {
    problems.push(
      `no \`s.api(\` call site found upstream in ${ROUTE_TABLE_FILE} — the symbol this check measures is ` +
        `no longer upstream's, so a count of added lines means nothing. Re-derive R1 before trusting it.`,
    )
    return { problems, notes }
  }
  if (added > ceiling) {
    problems.push(
      `${ROUTE_TABLE_FILE}: this branch adds ${added} \`s.api(\` line(s) to upstream's route table (ceiling ${ceiling}, ` +
        `upstream has ${upstreamCount}, this branch ${total}). Every added line is a permanent merge conflict. ` +
        `Register fork routes through the registrar handed to Config.MountAPI instead, or lower the ceiling as P0-01A C folds them.`,
    )
    return { problems, notes }
  }
  if (added === 0) {
    notes.push(`route table: no added \`s.api(\` lines — the P0-01A C target is reached.`)
    return { problems, notes }
  }
  notes.push(
    `route table: ${added} added \`s.api(\` line(s) of ${total} (ceiling ${ceiling}, upstream ${upstreamCount}) — converge via P0-01A C.`,
  )
  return { problems, notes }
}

// analyzeCeiling enforces one R2 per-file ceiling.
export function analyzeCeiling({ file, actual, ceiling }) {
  if (actual <= ceiling) return []
  return [
    `${file}: fork diff is ${actual} lines vs upstream (ceiling ${ceiling}) — ` +
      `grew by ${actual - ceiling}. Move the change into a fork-owned package, or lower the ceiling in scripts/server-diff-guard.mjs as part of a fold.`,
  ]
}

// analyzeProductFiles enforces the R2 registration rule: a product file in the
// upstream tree must have a recorded convergence plan.
export function analyzeProductFiles({ found, registered }) {
  const problems = []
  const known = new Set(registered)
  for (const file of found) {
    const base = path.basename(file)
    if (!PRODUCT_FILE_PATTERN.test(base)) continue
    if (known.has(file)) continue
    problems.push(
      `${file}: product-layer file in the upstream tree with no registration — ` +
        `add it to PRODUCT_FILES with the work package that removes it (P0-01A), or put it in internal/productruntime.`,
    )
  }
  return problems
}

// ─── git-backed fact gathering ──────────────────────────────────────────────

// Exported so fork-marker-guard shares this runner and the upstream-ref
// resolution below rather than writing a second copy of them (开发规范 §3.5).
export function git(root, args) {
  return execFileSync('git', args, { cwd: root, encoding: 'utf8' })
}

// ─── the pinned upstream baseline ───────────────────────────────────────────

// parseUpstreamBaseline reads the pin. The first line matching
// BASELINE_PATTERN is the baseline; blank lines and `#` comments are ignored,
// which is what lets the file carry the reasoning for its own existence next to
// the value.
//
// A file with no commit id is a problem rather than an empty result: an
// unreadable pin must not become "measure against something else" (the failure
// mode the moving ref had) and must not become "nothing to compare, report
// success" (V-49).
export function parseUpstreamBaseline(text) {
  const lines = text.split('\n')
  for (const [index, raw] of lines.entries()) {
    const line = raw.trim()
    if (line.length === 0 || line.startsWith('#')) continue
    if (!BASELINE_PATTERN.test(line)) {
      return {
        sha: null,
        problems: [
          `${UPSTREAM_BASELINE_PATH}:${index + 1}: expected a 40-character lowercase commit id, got: ${line}`,
        ],
      }
    }
    return { sha: line, problems: [] }
  }
  return {
    sha: null,
    problems: [
      `${UPSTREAM_BASELINE_PATH}: no commit id — the fork's ratchets are measured against the pin ` +
        `in this file, so an empty one stops the guard rather than letting it choose a ref.`,
    ],
  }
}

// readUpstreamBaseline reads and parses the pin from disk.
export function readUpstreamBaseline(root, read = (p) => fs.readFileSync(p, 'utf8')) {
  let text = ''
  try {
    text = read(path.join(root, UPSTREAM_BASELINE_PATH))
  } catch {
    return {
      sha: null,
      problems: [
        `${UPSTREAM_BASELINE_PATH} is missing — it names the upstream commit the fork's ratchets ` +
          `are measured against (the file's header says why it is a pin and not a ref).`,
      ],
    }
  }
  return parseUpstreamBaseline(text)
}

// resolvePinnedUpstream returns the pinned commit if this checkout has it, else
// null. Both guards go through here, so "which upstream" has exactly one answer
// (开发规范 §3.8) and neither can quietly fall back to a moving ref.
export function resolvePinnedUpstream(root, run = git, read) {
  const { sha, problems } = readUpstreamBaseline(root, read)
  if (problems.length > 0) return null
  return resolveUpstream(root, [sha], run)
}

// missingBaselineProblem explains an unresolvable pin, naming the exact command
// that fixes it — an unresolvable pin is a checkout problem, not a verdict on
// the branch under review.
export function missingBaselineProblem(sha) {
  return (
    `the pinned upstream baseline ${sha} (${UPSTREAM_BASELINE_PATH}) is not in this checkout — ` +
    `the fork's ratchets are measured against it, so this guard cannot run. ` +
    `Fetch it (\`git fetch --no-tags origin ${sha}\`) or use a full clone; CI checks out with ` +
    `fetch-depth: 0, which already contains it. ` +
    `Do NOT point this guard at a moving ref instead — that is what made every branch red on ` +
    `2026-09-16, see ${UPSTREAM_BASELINE_PATH}'s header.`
  )
}

// resolveUpstream returns the first ref that exists, or null.
//
// `refs` has no default on purpose: the only caller that decides which upstream
// the fork is measured against is resolvePinnedUpstream above.
export function resolveUpstream(root, refs, run = git) {
  for (const ref of refs) {
    try {
      run(root, ['rev-parse', '--verify', '--quiet', `${ref}^{commit}`])
      return ref
    } catch {
      // try the next candidate
    }
  }
  return null
}

// countRouteLines counts `s.api(` registrar call sites. A null `ref` counts the
// working tree, which is what `countAddedRouteLines` diffs — the guard must not
// report "this branch N" from one revision and "added" from another.
export function countRouteLines(root, ref, run = git) {
  let out = ''
  const args = ['grep', '-o', 's\\.api(']
  if (ref) args.push(ref)
  args.push('--', ROUTE_TABLE_FILE)
  try {
    out = run(root, args)
  } catch (error) {
    // `git grep` exits 1 when there are no matches — that is the 0 case.
    if (error?.status === 1) return 0
    throw error
  }
  return out.split('\n').filter((line) => line.trim().length > 0).length
}

// countAddedRouteLines counts the `s.api(` lines THIS BRANCH adds to upstream's
// route table. These are the lines that conflict on every merge, which is what
// R1 is about — not the total, which upstream owns.
//
// Anchored after the `+` so a comment that merely mentions `s.api(` is not
// counted as a call site.
export function countAddedRouteLines(root, ref, run = git) {
  const out = run(root, ['diff', ref, '--', ROUTE_TABLE_FILE])
  return out.split('\n').filter((l) => /^\+[ \t]*s\.api\(/.test(l)).length
}

// forkDiffLines returns added + removed lines for one path vs the upstream ref.
//
// It reads the diff body rather than `--numstat` so it can exclude OCTO-FORK
// marker lines. A marker is *mandated* by hard rule 3, so counting it as debt
// would make the two guards contradict: complying with the marker guard would
// grow the debt the ratchet forbids, and the only fix would be a ceiling raise
// that hard rule 3 had caused. Only the marker lines the fork *added* are
// excluded — a marker upstream itself deletes is still a removed line.
export function forkDiffLines(root, ref, file, run = git) {
  let out = ''
  try {
    out = run(root, ['diff', '-U0', ref, '--', file])
  } catch {
    return 0
  }
  let count = 0
  for (const line of out.split('\n')) {
    // `+++ b/file` and `--- a/file` are the header, not content.
    if (line.startsWith('+++') || line.startsWith('---')) continue
    if (line.startsWith('+')) {
      if (!isMarkerLine(line.slice(1))) count += 1
      continue
    }
    if (line.startsWith('-')) count += 1
  }
  return count
}

// listGovernedFiles returns every tracked .go file under the governed prefix on
// the current branch.
export function listGovernedFiles(root, run = git) {
  const out = run(root, ['ls-files', GOVERNED_PREFIX])
  return out.split('\n').filter((l) => l.trim().length > 0)
}

// listUpstreamFiles returns the governed files that exist at the upstream ref.
//
// The complement of this set is what the fork ADDED. Those are not "modified
// upstream" files: the R2 registration rule (PRODUCT_FILES) governs them, and a
// coverage note that counted them as unratcheted upstream drift would name the
// wrong gap.
export function listUpstreamFiles(root, ref, run = git) {
  const out = run(root, ['ls-tree', '-r', '--name-only', ref, GOVERNED_PREFIX])
  return out.split('\n').filter((l) => l.trim().length > 0)
}

// summarizeCoverage splits the fork-modified upstream files under the governed
// prefix into the ones an R2 ceiling governs and the ones it does not.
//
// The ceilings are per-file and deliberately name a handful of files — the ones
// carrying product debt with a convergence plan. The rest of the governed tree
// is unratcheted *by design*, and that design is fine; what is not fine is a
// pass message reading "internal/server debt is within the recorded ceilings",
// which reads as a statement about the whole tree. That over-claim is how the
// coverage gap stays invisible, and it is the same shape as the two guard
// defects this file already carries notes about: the counter that could not
// fail (V-49) and the guard that measured a different upstream than CI (V-101).
//
// The numbers are measured at run time rather than written into the header as
// prose, because a frozen count goes stale on the next change while still
// reading as current (开发规范 §3.8).
export function summarizeCoverage({ modified, ceilings }) {
  const capped = new Set(ceilings.map((c) => c.file))
  const withCeiling = modified.filter((m) => capped.has(m.file))
  const without = modified.filter((m) => !capped.has(m.file))
  const sum = (rows) => rows.reduce((total, r) => total + r.lines, 0)
  return {
    withCeilingFiles: withCeiling.length,
    withCeilingLines: sum(withCeiling),
    withoutCeilingFiles: without.length,
    withoutCeilingLines: sum(without),
    withoutCeiling: without.map((r) => r.file).sort(),
  }
}

// formatCoverage renders one coverage sentence, so the note and the pass line
// cannot disagree about what was measured.
export function formatCoverage(coverage) {
  return (
    `R2 coverage: ${coverage.withCeilingFiles} fork-modified upstream file(s) carry a ceiling ` +
    `(${coverage.withCeilingLines} lines); ${coverage.withoutCeilingFiles} more are fork-modified upstream ` +
    `files with no ceiling (${coverage.withoutCeilingLines} lines). The ceilings are per-file and named, ` +
    `not tree-wide — the uncapped files are unratcheted by design, and their line counts are not compared ` +
    `against anything.`
  )
}

// ─── guard ──────────────────────────────────────────────────────────────────

export async function check(root, run = git, read) {
  const problems = []
  const notes = []

  const { sha, problems: baselineProblems } = readUpstreamBaseline(root, read)
  if (baselineProblems.length > 0) {
    return { problems: baselineProblems, notes }
  }
  const upstream = resolveUpstream(root, [sha], run)
  if (!upstream) {
    return { problems: [missingBaselineProblem(sha)], notes }
  }

  // R1 — route-table ratchet.
  const added = countAddedRouteLines(root, upstream, run)
  const upstreamCount = countRouteLines(root, upstream, run)
  // null ref = the working tree, the same revision countAddedRouteLines diffs.
  const total = countRouteLines(root, null, run)
  const routeResult = analyzeRouteTable({
    added,
    upstreamCount,
    total,
    ceiling: ROUTE_TABLE_CEILING,
  })
  problems.push(...routeResult.problems)
  notes.push(...routeResult.notes)

  // R2 — per-file debt ceilings.
  for (const entry of DEBT_CEILINGS) {
    const actual = forkDiffLines(root, upstream, entry.file, run)
    problems.push(...analyzeCeiling({ file: entry.file, actual, ceiling: entry.ceiling }))
    notes.push(`${entry.file}: ${actual}/${entry.ceiling} lines (${entry.convergence})`)
  }

  // R2 — unregistered product files.
  const found = listGovernedFiles(root, run)
  problems.push(
    ...analyzeProductFiles({
      found,
      registered: PRODUCT_FILES.map((p) => p.file),
    }),
  )

  // R2 — what the ceilings above do NOT cover, measured rather than implied.
  // Only files upstream also has: a file the fork added is governed by the
  // registration rule above, not by a drift ceiling.
  const upstreamGoverned = new Set(listUpstreamFiles(root, upstream, run))
  const modified = []
  for (const file of found) {
    if (!upstreamGoverned.has(file)) continue
    const lines = forkDiffLines(root, upstream, file, run)
    if (lines > 0) modified.push({ file, lines })
  }
  const coverage = summarizeCoverage({ modified, ceilings: DEBT_CEILINGS })
  notes.push(formatCoverage(coverage))

  return { problems, notes, coverage }
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

async function main() {
  const root = repositoryRoot(import.meta.url)
  const { problems, notes, coverage } = await check(root)

  for (const note of notes) console.log(`server-diff-guard: ${note}`)

  if (problems.length > 0) {
    console.error('server-diff-guard failed:')
    for (const problem of problems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }
  // The scope is named rather than implied: this guard ratchets the files listed
  // in DEBT_CEILINGS, and saying "internal/server debt is within the recorded
  // ceilings" without the count reads as a statement about the whole tree.
  console.log(
    `server-diff-guard passed: the ${coverage.withCeilingFiles} file(s) with a recorded R2 ceiling are within it ` +
      `(${coverage.withCeilingLines} lines), and no product file is unregistered. ` +
      `Ceilings are per-file, not tree-wide: ${coverage.withoutCeilingFiles} fork-modified upstream file(s) ` +
      `carry no ceiling and were not compared against anything.`,
  )
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
