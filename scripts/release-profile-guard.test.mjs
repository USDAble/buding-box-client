// Tests for scripts/release-profile-guard.mjs. The guard itself is a small
// text scan, so the tests drive checkSource() with synthetic file content:
// the positive case (tagged), the negative case (untagged) and the two
// documented escapes (CLI builds, explicit allow marker).

import assert from 'node:assert/strict'
import { test } from 'node:test'
import path from 'node:path'

import { checkSource, buildLines, SITES, PROFILE_TAG } from './release-profile-guard.mjs'

test('buildLines skips comments and non-build lines', () => {
  const content = [
    '# a comment mentioning go build',
    '// another comment about go build',
    'run: go build -o x .',
    'echo hello',
  ].join('\n')
  assert.deepEqual(
    buildLines(content).map((h) => h.line),
    [3],
  )
})

test('buildLines finds the Node execFileSync array form too', () => {
  const content = "execFileSync('go', ['build', '-tags', BUILD_TAGS, '-o', out, '.'], { cwd: modDir })"
  assert.deepEqual(
    buildLines(content).map((h) => h.line),
    [1],
  )
})

test('checkSource accepts a tagged shell build', () => {
  const problems = checkSource('scripts/x.sh', `go build -tags 'embedrg ${PROFILE_TAG}' -o out .`)
  assert.deepEqual(problems, [])
})

test('checkSource rejects an untagged desktop build', () => {
  const problems = checkSource('scripts/x.sh', 'go build -tags embedrg -o PuddingBox.exe .')
  assert.equal(problems.length, 1)
  assert.match(problems[0], /without product_production/)
})

test('checkSource accepts the BUILD_TAGS indirection only when defined with the tag', () => {
  const ok = [
    `export const BUILD_TAGS = 'embedrg ${PROFILE_TAG}'`,
    "execFileSync('go', ['build', '-tags', BUILD_TAGS, '-o', out, '.'], {})",
  ].join('\n')
  assert.deepEqual(checkSource('scripts/x.mjs', ok), [])

  const bad = [
    "export const BUILD_TAGS = 'embedrg'",
    "execFileSync('go', ['build', '-tags', BUILD_TAGS, '-o', out, '.'], {})",
  ].join('\n')
  const problems = checkSource('scripts/x.mjs', bad)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /definition does not include product_production/)
})

test('checkSource ignores CLI builds but not the desktop module', () => {
  assert.deepEqual(checkSource('w.yml', 'go build -o staging/octo ./cmd/octo'), [])
  const problems = checkSource('w.yml', 'go build -C cmd/octo-desktop -o PuddingBox.exe .')
  assert.equal(problems.length, 1)
})

test('checkSource honours an explicit allow marker', () => {
  const content = 'go build -o x . # release-profile-guard:allow — dev-only artifact'
  assert.deepEqual(checkSource('w.yml', content), [])
})

test('every declared site is a real repo-relative path', () => {
  for (const site of SITES) {
    assert.ok(site.file.length > 0)
    assert.ok(!path.isAbsolute(site.file), `${site.file} must be repo-relative`)
  }
})
