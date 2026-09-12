// Tests for scripts/webdist-clean.mjs (V-34).
//
// The defect this guards is silent in both directions: a stale fixed-name asset
// ships inside the binary (the upstream favicon did), and deleting the tracked
// .gitkeep leaves the git tree dirty, which blocks a release. Neither shows up
// as a failing build, so both are asserted here.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { SURVIVORS, DEFAULT_DIR, staleEntries, clean } from './webdist-clean.mjs'

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

async function tmpdir(t) {
  const d = await fs.mkdtemp(path.join(os.tmpdir(), 'webdist-'))
  t.after(() => fs.rm(d, { recursive: true, force: true }))
  return d
}

// The regression, stated as a listing: a favicon.svg that a previous build left
// behind must be classified stale. Vite would have kept it (emptyOutDir: false).
test('staleEntries: a fixed-name asset left by an earlier build is stale', () => {
  const stale = staleEntries(['.gitkeep', 'favicon.svg', 'index.html', 'assets'])
  assert.ok(stale.includes('favicon.svg'), 'the upstream favicon deletion must actually take effect')
  assert.ok(stale.includes('index.html'))
  assert.ok(stale.includes('assets'))
})

test('staleEntries: .gitkeep survives, because the tracked sentinel is what keeps the tree clean', () => {
  assert.deepEqual(SURVIVORS, ['.gitkeep'])
  assert.ok(!staleEntries(['.gitkeep']).includes('.gitkeep'))
  assert.deepEqual(staleEntries(['.gitkeep']), [])
})

test('clean: removes a stray asset and its directory, keeps .gitkeep', async (t) => {
  const dir = await tmpdir(t)
  await fs.writeFile(path.join(dir, '.gitkeep'), '')
  await fs.writeFile(path.join(dir, 'favicon.svg'), '<svg/>')
  await fs.mkdir(path.join(dir, 'assets'))
  await fs.writeFile(path.join(dir, 'assets', 'index-abc123.js'), 'x')

  const removed = await clean(dir)

  assert.deepEqual(removed.sort(), ['assets', 'favicon.svg'])
  assert.deepEqual((await fs.readdir(dir)).sort(), ['.gitkeep'])
})

test('clean: a missing directory is not an error', async (t) => {
  const dir = path.join(await tmpdir(t), 'never-created')
  assert.deepEqual(await clean(dir), [])
})

// The failure this catches is the one V-29 recorded for a different feature: a
// helper exists, is tested, and nothing calls it. `make web-build` must depend on
// the clean, or the binary keeps its stale assets while every test above passes.
test('the web build depends on the clean step', async () => {
  const mk = await fs.readFile(path.join(repoRoot, 'Makefile'), 'utf8')
  const recipe = mk.split('\nweb-build:')[1]
  assert.ok(recipe, 'Makefile has no web-build target')

  const prerequisites = recipe.split('\n')[0]
  assert.match(
    prerequisites,
    /web-dist-clean/,
    'web-build must depend on web-dist-clean, or V-34 comes back silently',
  )
  assert.ok(
    mk.includes(`node scripts/webdist-clean.mjs ${DEFAULT_DIR}`) || mk.includes('node scripts/webdist-clean.mjs'),
    'the web-dist-clean target must run this script',
  )
})
