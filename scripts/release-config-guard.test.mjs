// Tests for scripts/release-config-guard.mjs. The guard's whole job is to tell
// a *structurally valid* production profile apart from a *release-ready* one,
// so the interesting cases are "Go would accept this, but nobody should ship
// it" — a `.invalid` placeholder host, an empty trust store.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { check, checkContent, PRODUCTION_ASSET } from './release-config-guard.mjs'

// The repository root, derived from this file's own location (scripts/…).
const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

const ready = {
  schemaVersion: 1,
  name: 'production',
  allowDevWebview: false,
  allowEnvironmentModelSource: false,
  allowDataRootOverride: false,
  apiHost: 'https://api.example.com/v1',
  gatewayHost: 'https://gateway.example.com/v1',
  trustedKeyIDs: { 'policy-2026-a': 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=' },
  startup: { channels: true, tools: true, mcp: true, backgroundTasks: true },
}

const json = (overrides = {}) => JSON.stringify({ ...ready, ...overrides })

test('a release-ready profile produces no problems', () => {
  assert.deepEqual(checkContent(PRODUCTION_ASSET, json()), [])
})

test('reports a placeholder apiHost that Go would accept', () => {
  // internal/productprofile.Validate only requires a non-empty https URL, so
  // this asset passes every unit test and fails every request at runtime.
  const problems = checkContent(PRODUCTION_ASSET, json({ apiHost: 'https://api.invalid/v1' }))
  assert.equal(problems.length, 1)
  assert.match(problems[0], /apiHost/)
  assert.match(problems[0], /placeholder/)
  assert.match(problems[0], /\.invalid/)
})

test('reports a placeholder gatewayHost independently', () => {
  const problems = checkContent(PRODUCTION_ASSET, json({ gatewayHost: 'https://gateway.invalid/v1' }))
  assert.equal(problems.length, 1)
  assert.match(problems[0], /gatewayHost/)
})

test('reports an empty trust store', () => {
  const problems = checkContent(PRODUCTION_ASSET, json({ trustedKeyIDs: {} }))
  assert.equal(problems.length, 1)
  assert.match(problems[0], /trustedKeyIDs is empty/)
})

test('reports an empty host as missing rather than as a placeholder', () => {
  const problems = checkContent(PRODUCTION_ASSET, json({ apiHost: '' }))
  assert.equal(problems.length, 1)
  assert.match(problems[0], /apiHost is empty/)
})

test('reports plaintext and unversioned hosts', () => {
  const problems = checkContent(PRODUCTION_ASSET, json({ apiHost: 'http://api.example.com' }))
  assert.equal(problems.length, 2)
  assert.ok(problems.some((p) => /must use https/.test(p)))
  assert.ok(problems.some((p) => /versioned/.test(p)))
})

test('reports a developer flag left on', () => {
  const problems = checkContent(PRODUCTION_ASSET, json({ allowDataRootOverride: true }))
  assert.equal(problems.length, 1)
  assert.match(problems[0], /allowDataRootOverride/)
})

test('reports a non-production asset', () => {
  const problems = checkContent(PRODUCTION_ASSET, json({ name: 'developer' }))
  assert.equal(problems.length, 1)
  assert.match(problems[0], /expected "production"/)
})

test('reports malformed JSON instead of throwing', () => {
  const problems = checkContent(PRODUCTION_ASSET, '{ not json')
  assert.equal(problems.length, 1)
  assert.match(problems[0], /not valid JSON/)
})

test('reports a non-object trustedKeyIDs', () => {
  const problems = checkContent(PRODUCTION_ASSET, json({ trustedKeyIDs: [] }))
  assert.equal(problems.length, 1)
  assert.match(problems[0], /trustedKeyIDs must be an object/)
})

test('the repository asset is currently not release-ready', async () => {
  // Pins today's state: the checked-in production profile still carries the
  // `.invalid` placeholders. When the deployment host lands, this test should
  // be deleted rather than relaxed — it exists so nobody assumes the release
  // asset is finished.
  const problems = await check(ROOT)
  assert.ok(problems.length > 0, 'expected the placeholder hosts to be reported')
  assert.ok(problems.some((p) => /placeholder/.test(p)))
  assert.ok(problems.some((p) => /trustedKeyIDs is empty/.test(p)))
})
