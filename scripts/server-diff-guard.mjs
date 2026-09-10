// scripts/server-diff-guard.mjs
//
// `internal/server` is an upstream file tree that this fork must keep
// mergeable. Two kinds of drift destroy that property, and neither one is
// visible in review because every individual change looks reasonable:
//
//   R1  apiProduct(). P3 wanted a "product gate" in front of ~150 routes and
//       expressed it by rewriting the upstream route table line by line. The
//       symbol exists 0 times in upstream main and 157 times here. Each
//       upstream merge now conflicts across that rewrite.
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

// The upstream-tracking ref. Falls back to a local `main` when there is no
// remote (developer checkouts, CI on a fresh clone of a single branch).
export const UPSTREAM_REFS = ['origin/main', 'main']

// Directories the guard governs: the upstream runtime the fork must not grow.
export const GOVERNED_PREFIX = 'internal/server/'

// R1 — the apiProduct symbol count, measured on 2026-09-11:
//   upstream main: 0 (`git grep -o apiProduct main -- internal/server/` → nothing)
//   this branch: 161, made up of
//     157  `s.apiProduct(` call sites   (main has 147 `s.api(` instead)
//       1  the `func (s *Server) apiProduct` definition
//       3  comments that name it
// The ratchet target is 0 (P0-01A folds it into a top-level middleware). Do not
// raise this; lower it as the fold lands.
export const API_PRODUCT_BASELINE = 161

// R2 — per-file fork diff (added + removed lines vs upstream) ceilings for the
// upstream files that carry real product debt. Merging upstream shrinks these
// automatically, so a *lower* number is always safe; only growth fails.
//
// `convergence` names the work package that is supposed to shrink it.
export const DEBT_CEILINGS = [
  {
    file: 'internal/server/server.go',
    ceiling: 484,
    why: 'P3 product gate (apiProduct + 157 call sites), product state wiring, credit/PII sender wrapping',
    convergence: 'P0-01A C (apiProduct fold) + P0-01A D (sender/state move)',
  },
  {
    file: 'internal/server/handlers.go',
    ceiling: 83,
    why: 'product-gate and credit call sites in upstream turn handlers',
    convergence: 'P0-01A D + P0-05 (credit deletion)',
  },
  {
    file: 'internal/server/ws_handlers.go',
    ceiling: 36,
    why: 'product-gate and privacy call sites on the WebSocket turn path',
    convergence: 'P0-01A D',
  },
  {
    file: 'internal/server/native_handlers.go',
    ceiling: 31,
    why: 'product-gate call sites',
    convergence: 'P0-01A C (middleware takes over)',
  },
  {
    file: 'internal/server/attachments.go',
    ceiling: 27,
    why: 'product-gate call sites',
    convergence: 'P0-01A C',
  },
  {
    file: 'internal/server/lightapps_handlers.go',
    ceiling: 14,
    why: 'product-gate call sites',
    convergence: 'P0-01A C',
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
]

// ─── pure analyzers (unit-tested) ───────────────────────────────────────────

// analyzeApiProduct enforces R1. `upstream` is the same count measured on the
// upstream ref; it must stay 0, because a non-zero upstream count means the
// symbol is no longer a fork-only invention and the fold premise has changed.
export function analyzeApiProduct({ current, baseline, upstream }) {
  const problems = []
  const notes = []

  if (upstream > 0) {
    problems.push(
      `apiProduct appears ${upstream} time(s) upstream — it is no longer a fork-only symbol; ` +
        `re-derive the fold plan (P0-01A §2) before trusting this baseline`,
    )
  }
  if (current > baseline) {
    problems.push(
      `apiProduct grew to ${current} call sites (ceiling ${baseline}). ` +
        `The route table is rewritten line-by-line vs upstream; every added call site is a permanent merge conflict. ` +
        `Route the new handler through the product-gate middleware instead, or lower the baseline by folding call sites (P0-01A C).`,
    )
    return { problems, notes }
  }
  if (current === 0) {
    notes.push('apiProduct is gone — P0-01A fold complete, the ratchet target is reached.')
    return { problems, notes }
  }
  notes.push(
    `apiProduct: ${current} call sites remaining (ceiling ${baseline}, target 0) — converge via P0-01A C.`,
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

function git(root, args) {
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

// countApiProduct counts occurrences (not lines) under the governed prefix.
export function countApiProduct(root, ref, run = git) {
  let out = ''
  try {
    out = run(root, ['grep', '-o', 'apiProduct', ref, '--', GOVERNED_PREFIX])
  } catch (error) {
    // `git grep` exits 1 when there are no matches — that is the 0 case.
    if (error?.status === 1) return 0
    throw error
  }
  return out.split('\n').filter((line) => line.trim().length > 0).length
}

// forkDiffLines returns added + removed lines for one path vs the upstream ref.
export function forkDiffLines(root, ref, file, run = git) {
  const out = run(root, ['diff', '--numstat', ref, '--', file])
  const line = out.split('\n').find((l) => l.trim().length > 0)
  if (!line) return 0
  const [added, removed] = line.split('\t')
  // Binary files report "-\t-"; treat them as zero changed lines.
  const a = Number.parseInt(added, 10)
  const r = Number.parseInt(removed, 10)
  return (Number.isNaN(a) ? 0 : a) + (Number.isNaN(r) ? 0 : r)
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

  // R1 — apiProduct ratchet.
  const current = countApiProduct(root, 'HEAD', run)
  const upstreamCount = countApiProduct(root, upstream, run)
  const apiResult = analyzeApiProduct({
    current,
    baseline: API_PRODUCT_BASELINE,
    upstream: upstreamCount,
  })
  problems.push(...apiResult.problems)
  notes.push(...apiResult.notes)

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
