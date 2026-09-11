// Tests for scripts/server-diff-guard.mjs. The guard's value is the ratchet
// semantics, so the tests pin exactly those: growth fails, standing still
// passes, shrinking passes, and reaching zero is announced. The git-backed
// gathering is exercised separately against the real repository.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import path from 'node:path'

import {
  analyzeApiProduct,
  analyzeCeiling,
  analyzeProductFiles,
  countApiProduct,
  forkDiffLines,
  resolveUpstream,
  listGovernedFiles,
  PRODUCT_FILE_PATTERN,
  GOVERNED_PREFIX,
} from './server-diff-guard.mjs'

// ─── R1: apiProduct ratchet ─────────────────────────────────────────────────

test('apiProduct: standing still is not a failure (the fold has not landed yet)', () => {
  const { problems, notes } = analyzeApiProduct({ current: 157, baseline: 157, upstream: 0 })
  assert.deepEqual(problems, [])
  assert.equal(notes.length, 1)
  assert.match(notes[0], /157 call sites remaining/)
})

test('apiProduct: growth fails and names the middleware as the fix', () => {
  const { problems } = analyzeApiProduct({ current: 160, baseline: 157, upstream: 0 })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /grew to 160/)
  assert.match(problems[0], /middleware/)
})

test('apiProduct: shrinking passes and reports progress', () => {
  const { problems, notes } = analyzeApiProduct({ current: 4, baseline: 157, upstream: 0 })
  assert.deepEqual(problems, [])
  assert.match(notes[0], /4 call sites remaining/)
})

test('apiProduct: reaching zero is announced as the target', () => {
  const { problems, notes } = analyzeApiProduct({ current: 0, baseline: 157, upstream: 0 })
  assert.deepEqual(problems, [])
  assert.match(notes[0], /fold complete/)
})

test('apiProduct: an upstream occurrence invalidates the fork-only premise', () => {
  const { problems } = analyzeApiProduct({ current: 5, baseline: 157, upstream: 3 })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /no longer a fork-only symbol/)
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

test('countApiProduct treats git grep exit 1 as zero', () => {
  const run = () => {
    const error = new Error('no matches')
    error.status = 1
    throw error
  }
  assert.equal(countApiProduct('/repo', 'main', run), 0)
})

test('countApiProduct counts occurrences, not matching lines', () => {
  // `git grep -o` prints one line PER OCCURRENCE, each carrying the path prefix.
  const run = () =>
    [
      'internal/server/server.go:s.apiProduct',
      'internal/server/server.go:s.apiProduct',
      'internal/server/product_nickname.go:apiProduct',
    ].join('\n') + '\n'
  assert.equal(countApiProduct('/repo', 'HEAD', run), 3)
})

test('forkDiffLines sums added and removed, and tolerates binary output', () => {
  assert.equal(forkDiffLines('/r', 'main', 'a.go', () => '317\t167\ta.go\n'), 484)
  assert.equal(forkDiffLines('/r', 'main', 'a.png', () => '-\t-\ta.png\n'), 0)
  assert.equal(forkDiffLines('/r', 'main', 'new.go', () => ''), 0)
})

test('listGovernedFiles keeps only the governed prefix', () => {
  const run = () => 'internal/server/a.go\ninternal/server/b_test.go\n'
  const files = listGovernedFiles('/repo', run)
  assert.deepEqual(files, ['internal/server/a.go', 'internal/server/b_test.go'])
  assert.ok(files.every((f) => f.startsWith(GOVERNED_PREFIX)))
})
