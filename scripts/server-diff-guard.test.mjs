// Tests for scripts/server-diff-guard.mjs. The guard's value is the ratchet
// semantics, so the tests pin exactly those: growth fails, standing still
// passes, shrinking passes, and reaching zero is announced. The git-backed
// gathering is exercised separately against the real repository.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import path from 'node:path'

import {
  analyzeCeiling,
  analyzeProductFiles,
  analyzeRouteTable,
  countAddedRouteLines,
  countRouteLines,
  forkDiffLines,
  resolveUpstream,
  listGovernedFiles,
  PRODUCT_FILE_PATTERN,
  GOVERNED_PREFIX,
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
