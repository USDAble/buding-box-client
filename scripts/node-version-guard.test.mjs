import assert from 'node:assert/strict'
import test from 'node:test'

import { MIN_NODE, parseVersion, problem, supported } from './node-version-guard.mjs'

test('parses a normal Node version', () => {
  assert.deepEqual(parseVersion('v22.22.2'), MIN_NODE)
  assert.equal(parseVersion('node 22'), null)
})

test('accepts the floor and later major versions', () => {
  assert.equal(supported([22, 22, 2]), true)
  assert.equal(supported([24, 0, 0]), true)
  assert.equal(problem('v22.22.2'), null)
})

test('rejects an earlier Node runtime with an actionable message', () => {
  assert.equal(supported([22, 22, 1]), false)
  assert.equal(supported([20, 19, 0]), false)
  assert.match(problem('v20.0.0'), /22\.22\.2/)
})
