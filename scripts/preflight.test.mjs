// Tests for scripts/preflight.mjs. The interesting behaviour is the two tiers:
// the advisory tier must never fail a build, because a packaging host can
// legitimately lack the upstream ref — if that blocked packaging, people would
// disable the check, which is worse than a warning.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import path from 'node:path'

import { runAdvisoryChecks, runHardChecks, runPreflight, repositoryRoot } from './preflight.mjs'

const ROOT = repositoryRoot(import.meta.url)

// ─── advisory tier ──────────────────────────────────────────────────────────

test('advisory: a missing upstream ref warns instead of failing', async () => {
  const { warnings, notes } = await runAdvisoryChecks(ROOT, { resolve: () => null })
  assert.deepEqual(notes, [])
  assert.equal(warnings.length, 1)
  assert.match(warnings[0], /no upstream ref/)
  assert.match(warnings[0], /NOT verified/)
})

test('advisory: with an upstream ref the drift result is reported', async () => {
  // The real repo has main, so this reports real numbers rather than a warning.
  const { warnings, notes } = await runAdvisoryChecks(ROOT)
  assert.ok(notes.length > 0, 'expected server-diff notes when the ref resolves')
  for (const w of warnings) assert.match(w, /^server-diff-guard: /)
})

// ─── hard tier ──────────────────────────────────────────────────────────────

test('hard: a clean tree yields no problems', async () => {
  // This is the repository's real state, and the guards below are the ones CI
  // runs green on. If this fails, the tree genuinely drifted — fix the tree,
  // do not relax the test.
  const problems = await runHardChecks(ROOT)
  assert.deepEqual(problems, [])
})

test('runPreflight: reports zero failures and says so', async () => {
  const logs = []
  const log = {
    log: (...a) => logs.push(['log', a.join(' ')]),
    warn: (...a) => logs.push(['warn', a.join(' ')]),
    error: (...a) => logs.push(['error', a.join(' ')]),
  }
  const result = await runPreflight(ROOT, { log })
  assert.equal(result.failureCount, 0)
  assert.ok(logs.some(([kind, line]) => kind === 'log' && line.includes('通过')))
  assert.ok(!logs.some(([kind]) => kind === 'error'))
})

test('runPreflight: a rejected tree produces errors and a non-zero failure count', async () => {
  const logs = []
  const log = {
    log: () => {},
    warn: () => {},
    error: (...a) => logs.push(a.join(' ')),
  }
  const hardChecks = async () => ['release-profile-guard: fake drift for the failure path']
  const result = await runPreflight(ROOT, { log, hardChecks })
  assert.equal(result.failureCount, 1)
  assert.ok(logs.some((line) => line.includes('拒绝构建')))
  assert.ok(logs.some((line) => line.includes('fake drift')))
})

test('advisory warnings do not make runPreflight fail', async () => {
  const logs = []
  const log = {
    log: () => {},
    warn: (...a) => logs.push(a.join(' ')),
    error: () => {},
  }
  // Advisory alone (upstream ref absent) must not flip failureCount.
  const hardChecks = async () => []
  const result = await runPreflight(ROOT, { log, hardChecks })
  assert.equal(result.failureCount, 0)
})
