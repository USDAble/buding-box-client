import assert from 'node:assert/strict'
import test from 'node:test'

import {
  OCTO_HOME_PATH,
  OCTO_LITERAL,
  USER_HOME_DIR,
  check,
  isAllowed,
  loadAllowlist,
  octoLiteralViolations,
  repositoryRoot,
} from './datapath-guard.mjs'

const root = repositoryRoot(import.meta.url)

test('the committed tree passes the guard', async () => {
  assert.deepEqual(await check(root), [])
})

test('the ".octo" literal matches only the exact double-quoted string', () => {
  assert.equal(OCTO_LITERAL.test(`filepath.Join(home, ".octo", "sessions")`), true)
  assert.equal(OCTO_LITERAL.test(`".octo"`), true)
  // .octorules must never trip it, and prose mentioning the path name is not a
  // Go string literal.
  assert.equal(OCTO_LITERAL.test(`".octorules"`), false)
  assert.equal(OCTO_LITERAL.test(`// the ~/.octo data root is gone`), false)
})

test('an unlicensed ".octo" literal is reported with its line number', () => {
  const src = ['package p', '', '\thome, _ := os.UserHomeDir()', '\tp := filepath.Join(home, ".octo", "sessions")'].join('\n')
  assert.deepEqual(octoLiteralViolations(src), [4])
})

test('a marker licenses the line it is written on and nothing else', () => {
  const licensed = '\treturn filepath.Join(cwd, ".octo", "hooks.yml") // octo-literal-allow: project dir, not the data root'
  assert.deepEqual(octoLiteralViolations(licensed), [])

  // A second, unmarked occurrence in the same file still fails — the marker is
  // per-line, not per-file, which is the whole point of it.
  const twoSites = [licensed, '\tp := filepath.Join(home, ".octo", "sessions")'].join('\n')
  assert.deepEqual(octoLiteralViolations(twoSites), [2])
})

test('a marker on a neighbouring line licenses nothing', () => {
  // Adjacency is not special: were it, a new occurrence inserted right after a
  // marked line would be silently licensed — exactly the hole the marker exists
  // to close.
  const above = [
    '// octo-literal-allow: project-level directory in the user repo',
    '\treturn filepath.Join(cwd, ".octo", "hooks.yml")',
  ].join('\n')
  assert.deepEqual(octoLiteralViolations(above), [2])

  const below = [
    '\treturn filepath.Join(cwd, ".octo", "hooks.yml")',
    '// octo-literal-allow: project-level directory in the user repo',
  ].join('\n')
  assert.deepEqual(octoLiteralViolations(below), [1])
})

test('a marker with no reason is not a marker', () => {
  const src = '\treturn filepath.Join(cwd, ".octo", "hooks.yml") // octo-literal-allow:'
  assert.deepEqual(octoLiteralViolations(src), [1])
})

test('the "~/.octo" path regex matches a host-home reference', () => {
  assert.equal(OCTO_HOME_PATH.test(`~/.octo/light-apps/<slug>/`), true)
  assert.equal(OCTO_HOME_PATH.test(`write_file ~/.octo/config.yml`), true)
  // The data-root placeholder and bare directory name must not trip it.
  assert.equal(OCTO_HOME_PATH.test(`<data root>/light-apps/<slug>/`), false)
  assert.equal(OCTO_HOME_PATH.test(`.octorules`), false)
})

test('the os.UserHomeDir() regex matches a call', () => {
  assert.equal(USER_HOME_DIR.test(`home, _ := os.UserHomeDir()`), true)
  assert.equal(USER_HOME_DIR.test(`// os.UserHomeDir is banned`), false)
})

test('isAllowed matches an exact file entry', () => {
  const entries = [
    { prefix: 'internal/tools/read_file.go', dir: false },
    { prefix: 'internal/datapath', dir: true },
  ]
  assert.equal(isAllowed('internal/tools/read_file.go', entries), true)
  assert.equal(isAllowed('internal/tools/other.go', entries), false)
})

test('isAllowed matches a recursive directory entry at path-segment boundaries', () => {
  const entries = [{ prefix: 'internal/datapath', dir: true }]
  assert.equal(isAllowed('internal/datapath', entries), true)
  assert.equal(isAllowed('internal/datapath/sub/file.go', entries), true)
  assert.equal(isAllowed('internal/datapathX/file.go', entries), false)
})

test('loadAllowlist parses comments, blank lines, exact files, and dir recursion', async () => {
  const entries = await loadAllowlist(root)
  assert.ok(entries.length > 0)
  assert.ok(entries.some((e) => e.prefix === 'internal/datapath' && e.dir))
  assert.ok(entries.some((e) => e.prefix === 'internal/tools/read_file.go' && !e.dir))
})
