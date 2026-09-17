// Tests for scripts/server-diff-guard.mjs. The guard's value is the ratchet
// semantics, so the tests pin exactly those: growth fails, standing still
// passes, shrinking passes, and reaching zero is announced. The git-backed
// gathering is exercised separately against the real repository.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import {
  analyzeCeiling,
  analyzeProductFiles,
  analyzeRouteTable,
  check,
  countAddedRouteLines,
  countRouteLines,
  forkDiffLines,
  missingBaselineProblem,
  parseUpstreamBaseline,
  readUpstreamBaseline,
  resolvePinnedUpstream,
  resolveUpstream,
  listGovernedFiles,
  listUpstreamFiles,
  summarizeCoverage,
  formatCoverage,
  PRODUCT_FILE_PATTERN,
  GOVERNED_PREFIX,
  UPSTREAM_BASELINE_PATH,
} from './server-diff-guard.mjs'

// ─── R1: the route table ────────────────────────────────────────────────────
//
// This replaced a count of `apiProduct`, which does not exist: it was 0 at HEAD,
// so the check reported "the ratchet target is reached" on every run while the
// risk stayed live. These tests pin the property that was missing — the check
// must fail loudly when its own subject disappears.

test('route table: standing still is not a failure (the fold has not landed yet)', () => {
  const { problems, notes } = analyzeRouteTable({ added: 1, upstreamCount: 147, total: 148, ceiling: 1 })
  assert.deepEqual(problems, [])
  assert.equal(notes.length, 1)
  assert.match(notes[0], /1 added `s\.api\(` line\(s\) of 148/)
})

test('route table: growth fails and names the registrar as the fix', () => {
  const { problems } = analyzeRouteTable({ added: 3, upstreamCount: 147, total: 150, ceiling: 1 })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /adds 3 `s\.api\(` line\(s\)/)
  assert.match(problems[0], /ceiling 1, upstream has 147, this branch 150/)
  assert.match(problems[0], /Config\.MountAPI/)
})

test('route table: reaching zero is announced as the target', () => {
  const { problems, notes } = analyzeRouteTable({ added: 0, upstreamCount: 147, total: 147, ceiling: 1 })
  assert.deepEqual(problems, [])
  assert.match(notes[0], /target is reached/)
})

test('route table: a zero upstream count invalidates the measurement', () => {
  // The exact way the previous R1 died: measuring a symbol that is not there.
  // It must fail, not pass, and it must not also emit a reassuring note.
  const { problems, notes } = analyzeRouteTable({ added: 0, upstreamCount: 0, total: 0, ceiling: 1 })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /no `s\.api\(` call site found upstream/)
  assert.match(problems[0], /no longer upstream's/)
  assert.deepEqual(notes, [])
})

// ─── R2: debt ceilings ──────────────────────────────────────────────────────

test('ceiling: at or under the ceiling passes', () => {
  assert.deepEqual(analyzeCeiling({ file: 'a.go', actual: 484, ceiling: 484 }), [])
  assert.deepEqual(analyzeCeiling({ file: 'a.go', actual: 100, ceiling: 484 }), [])
})

test('ceiling: going over fails with the delta', () => {
  const problems = analyzeCeiling({ file: 'a.go', actual: 490, ceiling: 484 })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /490 lines vs upstream \(ceiling 484\)/)
  assert.match(problems[0], /grew by 6/)
})

// ─── R2: product-file registration ─────────────────────────────────────────

test('product files: a registered product file passes', () => {
  const found = ['internal/server/product_login.go', 'internal/server/handlers.go']
  const registered = ['internal/server/product_login.go']
  assert.deepEqual(analyzeProductFiles({ found, registered }), [])
})

test('product files: an unregistered product file fails', () => {
  const found = ['internal/server/product_credits.go']
  const problems = analyzeProductFiles({ found, registered: [] })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /no registration/)
  assert.match(problems[0], /internal\/productruntime/)
})

test('product files: non-product upstream files are ignored', () => {
  const found = ['internal/server/handlers.go', 'internal/server/session_groups.go']
  assert.deepEqual(analyzeProductFiles({ found, registered: [] }), [])
})

test('product file pattern matches exactly the product-layer names', () => {
  for (const name of [
    'product_handlers.go',
    'product_login.go',
    'product_login_test.go',
    'privacy.go',
    'privacy_test.go',
    'chatmode_handlers.go',
    'chatmode_handlers_test.go',
    'sensitive_dict_handlers.go',
    'sensitive_dict_handlers_test.go',
  ]) {
    assert.ok(PRODUCT_FILE_PATTERN.test(name), `${name} should match`)
  }
  for (const name of [
    'handlers.go',
    'ws_handlers.go',
    'productruntime.go',
    'chatmode.go',
    'handler.go',
  ]) {
    assert.ok(!PRODUCT_FILE_PATTERN.test(name), `${name} should not match`)
  }
})

// ─── the pinned upstream baseline ───────────────────────────────────────────
//
// These are the tests for the 2026-09-16 incident: the ratchets resolved their
// upstream from a ref that CI kept fresh, so upstream advancing at 04:29Z turned
// every branch red — including three PRs that never touched internal/server —
// while the same job on the same base SHA had passed at 04:27Z. The pin is what
// makes the verdict a property of the branch, and these tests are what stop it
// from silently becoming a ref again.

const PIN = '6a9d040b317104df8f29c08c6b8657fde840ffc0'
const OTHER = 'ee1b7743deadbeefdeadbeefdeadbeefdeadbeef'

test('the baseline is the first commit id, and comments are ignored', () => {
  const { sha, problems } = parseUpstreamBaseline(
    ['# why this file exists', '', `   ${PIN}   `, `# ${OTHER} was a decoy`].join('\n'),
  )
  assert.deepEqual(problems, [])
  assert.equal(sha, PIN)
})

test('a baseline file with no commit id stops the guard', () => {
  const { sha, problems } = parseUpstreamBaseline('# only prose, no pin\n\n')
  assert.equal(sha, null)
  assert.equal(problems.length, 1)
  assert.match(problems[0], new RegExp(UPSTREAM_BASELINE_PATH))
})

test('a short or malformed id is rejected rather than used', () => {
  // Short ids are the tempting form (every log and PR body shows seven
  // characters), and a pin that can be ambiguous is not a pin.
  for (const bad of ['6a9d040', 'origin/main', 'main', `${PIN}^{commit}`]) {
    const { sha, problems } = parseUpstreamBaseline(`${bad}\n`)
    assert.equal(sha, null, `${bad} must not be accepted as a baseline`)
    assert.equal(problems.length, 1)
  }
})

test('the baseline is read from the file, and a missing file is a problem', () => {
  const root = '/repo'
  const read = (p) => {
    assert.equal(p, path.join(root, UPSTREAM_BASELINE_PATH))
    return `# comment\n${PIN}\n`
  }
  assert.deepEqual(readUpstreamBaseline(root, read), { sha: PIN, problems: [] })

  const missing = readUpstreamBaseline(root, () => {
    throw new Error('ENOENT')
  })
  assert.equal(missing.sha, null)
  assert.equal(missing.problems.length, 1)
  assert.match(missing.problems[0], new RegExp(UPSTREAM_BASELINE_PATH))
})

test('the pinned baseline is resolved, and nothing else is tried', () => {
  const tried = []
  const run = (_root, args) => {
    tried.push(args[args.length - 1])
    if (args.includes(`${PIN}^{commit}`)) return `${PIN}\n`
    throw new Error(`unexpected ref: ${args.join(' ')}`)
  }
  const read = () => `# c\n${PIN}\n`
  assert.equal(resolvePinnedUpstream('/repo', run, read), PIN)
  assert.deepEqual(tried, [`${PIN}^{commit}`], 'only the pin may be asked for')

  // With the pin absent from the checkout the resolver reports null; it must
  // not reach for `origin/main`, which is exactly how the drift got in.
  const absent = resolvePinnedUpstream('/repo', () => {
    throw new Error('no such ref')
  }, read)
  assert.equal(absent, null)
})

test('check fails closed on an unresolvable pin and does not touch a moving ref', async () => {
  const seen = []
  const run = (_root, args) => {
    seen.push(args.join(' '))
    throw new Error('git failed')
  }
  const read = () => `# c\n${PIN}\n`

  const { problems } = await check('/repo', run, read)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /pinned upstream baseline/)
  assert.match(problems[0], new RegExp(PIN))
  assert.ok(
    !seen.some((c) => c.includes('origin/main') || c.includes(' main^{commit}')),
    `the guard reached for a moving ref: ${seen.join(' | ')}`,
  )
})

test('missingBaselineProblem names the fetch command and the reason', () => {
  const message = missingBaselineProblem(PIN)
  assert.match(message, new RegExp(PIN))
  assert.match(message, /git fetch/)
  assert.match(message, new RegExp(UPSTREAM_BASELINE_PATH))
})

// ─── git-backed gathering, driven by a fake runner ─────────────────────────

test('resolveUpstream returns the first ref that exists', () => {
  const run = (_root, args) => {
    if (args.includes('origin/main^{commit}')) throw new Error('no such ref')
    if (args.includes('main^{commit}')) return 'abc123\n'
    throw new Error(`unexpected: ${args.join(' ')}`)
  }
  assert.equal(resolveUpstream('/repo', ['origin/main', 'main'], run), 'main')
})

test('resolveUpstream returns null when no candidate exists', () => {
  const run = () => {
    throw new Error('no such ref')
  }
  assert.equal(resolveUpstream('/repo', ['origin/main', 'main'], run), null)
})

test('countRouteLines treats git grep exit 1 as zero', () => {
  const run = () => {
    const error = new Error('no matches')
    error.status = 1
    throw error
  }
  assert.equal(countRouteLines('/repo', 'main', run), 0)
})

test('countRouteLines counts occurrences, not matching lines', () => {
  // `git grep -o` prints one line PER OCCURRENCE, each carrying the path prefix.
  const run = () =>
    [
      'internal/server/server.go:s.api(',
      'internal/server/server.go:s.api(',
    ].join('\n') + '\n'
  assert.equal(countRouteLines('/repo', 'HEAD', run), 2)
})

test('countAddedRouteLines counts only added lines that register a route', () => {
  const run = () =>
    [
      'diff --git a/x b/x',
      '--- a/internal/server/server.go',
      '+++ b/internal/server/server.go',
      '@@ -1,2 +1,3 @@',
      ' \ts.api("GET /a", h)          # context, not added',
      '+\ts.api("PATCH /api/sessions/{id}/chat_mode", h)',
      '+\t// s.api( mentioned in a comment does not count either',
      '-\ts.api("GET /old", h2)       # a removal is not an addition',
    ].join('\n')
  assert.equal(countAddedRouteLines('/repo', 'main', run), 1)
})

test('forkDiffLines counts added and removed lines, and skips the file header', () => {
  const run = () =>
    [
      'diff --git a/a.go b/a.go',
      '--- a/a.go',
      '+++ b/a.go',
      '@@ -1,2 +1,3 @@',
      ' upstream context is not present with -U0',
      '+added one',
      '+added two',
      '-removed one',
    ].join('\n')
  assert.equal(forkDiffLines('/r', 'main', 'a.go', run), 3)
})

test('forkDiffLines does not charge hard rule 3 markers as debt', () => {
  // A marker line is mandated, so counting it would make the marker guard and
  // this ratchet contradict: complying with one would fail the other.
  const run = () =>
    [
      'diff --git a/a.go b/a.go',
      '--- a/a.go',
      '+++ b/a.go',
      '@@ -1,1 +1,3 @@',
      '+// OCTO-FORK: the reason - see the design doc',
      '+\trealChange()',
      '+\t# OCTO-FORK: a non-Go introducer counts too',
    ].join('\n')
  assert.equal(forkDiffLines('/r', 'main', 'a.go', run), 1)
})

test('forkDiffLines counts an empty added line, which a "non-plus char" filter would drop', () => {
  const run = () =>
    ['diff --git a/a.go b/a.go', '--- a/a.go', '+++ b/a.go', '@@ -1,1 +1,3 @@', '+', '+x', '-'].join('\n')
  assert.equal(forkDiffLines('/r', 'main', 'a.go', run), 3)
})

test('forkDiffLines treats an unreadable diff as zero rather than throwing', () => {
  const run = () => {
    throw new Error('binary file')
  }
  assert.equal(forkDiffLines('/r', 'main', 'a.png', run), 0)
  assert.equal(forkDiffLines('/r', 'main', 'new.go', () => ''), 0)
})

test('listGovernedFiles keeps only the governed prefix', () => {
  const run = () => 'internal/server/a.go\ninternal/server/b_test.go\n'
  const files = listGovernedFiles('/repo', run)
  assert.deepEqual(files, ['internal/server/a.go', 'internal/server/b_test.go'])
  assert.ok(files.every((f) => f.startsWith(GOVERNED_PREFIX)))
})

test('listUpstreamFiles reads the tree at the ref, not the working tree', () => {
  const seen = []
  const run = (root, args) => {
    seen.push(args)
    return 'internal/server/upstream.go\n'
  }
  assert.deepEqual(listUpstreamFiles('/repo', 'abc123', run), ['internal/server/upstream.go'])
  assert.deepEqual(seen[0], ['ls-tree', '-r', '--name-only', 'abc123', GOVERNED_PREFIX])
})

// ─── R2 coverage: the ceilings are per-file, so most of the tree is uncapped ─

test('summarizeCoverage separates capped from uncapped fork-modified files', () => {
  const coverage = summarizeCoverage({
    modified: [
      { file: 'internal/server/server.go', lines: 576 },
      { file: 'internal/server/store_watch.go', lines: 58 },
      { file: 'internal/server/session_groups.go', lines: 49 },
    ],
    ceilings: [{ file: 'internal/server/server.go', ceiling: 576 }],
  })
  assert.equal(coverage.withCeilingFiles, 1)
  assert.equal(coverage.withCeilingLines, 576)
  assert.equal(coverage.withoutCeilingFiles, 2)
  assert.equal(coverage.withoutCeilingLines, 107)
  assert.deepEqual(coverage.withoutCeiling, ['internal/server/session_groups.go', 'internal/server/store_watch.go'])
})

test('summarizeCoverage counts nothing, rather than everything, when no file is modified', () => {
  const coverage = summarizeCoverage({
    modified: [],
    ceilings: [{ file: 'internal/server/server.go', ceiling: 576 }],
  })
  assert.equal(coverage.withCeilingFiles, 0)
  assert.equal(coverage.withoutCeilingFiles, 0)
  assert.equal(coverage.withoutCeilingLines, 0)
})

test('a ceiling on a file that is NOT fork-modified is not counted as capped', () => {
  // The ceiling and the measurement are different sets. A file listed in
  // DEBT_CEILINGS but unchanged (or renamed away) must not inflate the capped
  // count — the number the pass line prints is "files this run actually
  // compared", not "files the table mentions".
  const coverage = summarizeCoverage({
    modified: [{ file: 'internal/server/store_watch.go', lines: 58 }],
    ceilings: [
      { file: 'internal/server/server.go', ceiling: 576 },
      { file: 'internal/server/gone.go', ceiling: 4 },
    ],
  })
  assert.equal(coverage.withCeilingFiles, 0)
  assert.equal(coverage.withCeilingLines, 0)
  assert.equal(coverage.withoutCeilingFiles, 1)
  assert.equal(coverage.withoutCeilingLines, 58)
})

test('formatCoverage says the ceilings are not tree-wide', () => {
  const text = formatCoverage({
    withCeilingFiles: 5,
    withCeilingLines: 738,
    withoutCeilingFiles: 79,
    withoutCeilingLines: 884,
    withoutCeiling: [],
  })
  assert.match(text, /5 fork-modified upstream file\(s\) carry a ceiling \(738 lines\)/)
  assert.match(text, /79 more are fork-modified upstream files with no ceiling \(884 lines\)/)
  assert.match(text, /not tree-wide/)
})

test('the real repository reports coverage consistent with its own results', async () => {
  // fileURLToPath, not import.meta.dirname: the latter is undefined before Node
  // 20.11, and this repository's local Node is 20.0.0 while CI runs 22 — the two
  // revisions then disagree about whether the guard's tests pass, which is the
  // gap V-101 registered. A new test must not reintroduce it.
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
  const run = (r, args) => execFileSync('git', args, { cwd: r, encoding: 'utf8' })
  const { coverage } = await check(root, run)
  assert.ok(coverage, 'check must return the coverage it printed')
  // The guard examined nothing would be the failure mode this pins: a coverage
  // note reading "0 capped, 0 uncapped" is indistinguishable from a guard whose
  // gathering broke, and the two print the same sentence (V-49).
  assert.ok(
    coverage.withCeilingFiles > 0,
    'the ceilings must govern at least one fork-modified upstream file, or the guard measured nothing',
  )
  assert.ok(
    coverage.withoutCeilingFiles > 0,
    'the fork modifies far more of internal/server than the ceilings cover; 0 here means the gathering broke',
  )
})
