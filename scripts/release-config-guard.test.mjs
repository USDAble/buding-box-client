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
  // The gateway host is written bare by convention (it matches the provider's
  // DefaultBaseURL shape), but a trailing /v1 is equally valid — see the nail
  // that asserts both are accepted. This fixture used to carry /v1 and the
  // guard used to require it on both hosts (V-37, retracted).
  gatewayHost: 'https://gateway.example.com',
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
  const problems = checkContent(PRODUCTION_ASSET, json({ gatewayHost: 'https://gateway.invalid' }))
  assert.equal(problems.length, 1)
  assert.match(problems[0], /gatewayHost/)
})

// ─── the gateway host's suffix is NOT a rule (V-37, retracted) ──────────────
//
// One nail per direction was added here while V-37 was believed, and both were
// wrong in the same way: they encoded a preference as a contract. What replaced
// them is one nail asserting the property that is actually true and actually
// matters — either gateway host shape is accepted, because
// internal/provider/openai normalises a trailing /v1 before appending its path.
//
// The apiHost direction stays, because that one is a real requirement (V-13).

test('reports an apiHost that lost its /v1', () => {
  const problems = checkContent(PRODUCTION_ASSET, json({ apiHost: 'https://api.example.com' }))
  assert.equal(problems.length, 1)
  assert.match(problems[0], /apiHost should be versioned/)
})

test('accepts a gatewayHost with or without /v1', () => {
  // Both are correct: the provider trims a trailing /v1 and appends
  // "/v1/chat/completions" either way. A guard that demanded one of them would
  // be telling whoever cuts the release to make a change the runtime does not
  // need — and in the /v1-carrying case, the change this guard used to demand
  // was in the *opposite* direction from the one it demanded first.
  for (const host of ['https://gateway.example.com', 'https://gateway.example.com/v1']) {
    assert.deepEqual(
      checkContent(PRODUCTION_ASSET, json({ gatewayHost: host })),
      [],
      `gatewayHost ${host} should be accepted`,
    )
  }
})

test('still requires the gatewayHost to be a real https host', () => {
  // Dropping the suffix rule must not drop the checks this guard exists for.
  const placeholder = checkContent(PRODUCTION_ASSET, json({ gatewayHost: 'https://gateway.invalid/v1' }))
  assert.equal(placeholder.length, 1)
  assert.match(placeholder[0], /placeholder/)

  const plaintext = checkContent(PRODUCTION_ASSET, json({ gatewayHost: 'http://gateway.example.com' }))
  assert.equal(plaintext.length, 1)
  assert.match(plaintext[0], /must use https/)
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
