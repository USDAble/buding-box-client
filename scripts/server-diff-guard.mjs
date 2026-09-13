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
// Why compare against a local branch rather than a remote: this repo
// deliberately does not configure an `upstream` remote (问题盘点 §G1). `main`
// is the upstream-tracking branch, and it is what the fork merges from.
//
// Usage:
//   node scripts/server-diff-guard.mjs
//
// No npm dependencies. Run it in CI (.github/workflows/go.yml,
// server-diff-guard job) and locally via `make server-diff-check`.

import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { isMarkerLine } from './fork-marker.mjs'

// The upstream-tracking ref. Falls back to a local `main` when there is no
// remote (developer checkouts, CI on a fresh clone of a single branch).
export const UPSTREAM_REFS = ['origin/main', 'main']

// Directories the guard governs: the upstream runtime the fork must not grow.
export const GOVERNED_PREFIX = 'internal/server/'

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
export const ROUTE_TABLE_CEILING = 1

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
// A raise that cannot answer those three points should be a fold instead.
export const DEBT_CEILINGS = [
  {
    file: 'internal/server/server.go',
    ceiling: 496,
    why:
      'the product seam and the data-root migration: Config.MountAPI/WindowToken/RequireGateway/ControlPlaneReady plumbing, the productAPI registrar (a method value, not a call site), V-36/PR-5c/PR-5b1 gates on the turn path. ' +
      'Measured 2026-09-13 at 496 after excluding marker lines (see the marker note in forkDiffLines); of the added lines the large majority are prose explaining those seams',
    convergence:
      'P0-01A C (the apiProduct fold is dead — see the R1 note; what remains is the registrar and the product-state move, P0-01A D)',
  },
  {
    file: 'internal/server/handlers.go',
    ceiling: 88,
    why:
      'the data-root migration (a datapath import and the directory joins) plus sessionItem.ChatMode (PR-4d1, V-46 — see the raise notes above)',
    convergence: 'P0-01A D (+8 folds when the session descriptor moves); the datapath lines are the finished cost of hard rule 1',
  },
  {
    // Not modified by this fork at the time of writing. The 0 is the point: any
    // future change to an upstream WebSocket turn handler has to come here and
    // justify itself, which a ceiling of 36 lines did not require.
    file: 'internal/server/ws_handlers.go',
    ceiling: 0,
    why: 'this fork does not modify this file; the recorded debt was a 36-line allowance for a change that is not there',
    convergence: 'nothing to converge — keep it at 0',
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

// resolveUpstream returns the first upstream ref that exists, or null.
export function resolveUpstream(root, refs = UPSTREAM_REFS, run = git) {
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

// ─── guard ──────────────────────────────────────────────────────────────────

export async function check(root, run = git) {
  const problems = []
  const notes = []

  const upstream = resolveUpstream(root, UPSTREAM_REFS, run)
  if (!upstream) {
    return {
      problems: [
        `cannot find an upstream ref (tried ${UPSTREAM_REFS.join(', ')}) — ` +
          `this guard compares the fork against upstream, so it needs one fetched. ` +
          `In CI, check out with fetch-depth: 0 or fetch the branch explicitly.`,
      ],
      notes,
    }
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

  return { problems, notes }
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

async function main() {
  const root = repositoryRoot(import.meta.url)
  const { problems, notes } = await check(root)

  for (const note of notes) console.log(`server-diff-guard: ${note}`)

  if (problems.length > 0) {
    console.error('server-diff-guard failed:')
    for (const problem of problems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }
  console.log('server-diff-guard passed: internal/server debt is within the recorded ceilings.')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
