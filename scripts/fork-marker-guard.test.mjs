// Tests for scripts/fork-marker-guard.mjs.
//
// The guard's value is entirely in what it refuses, so the tests pin the
// refusals: a marker line is not a mention, a zero-file check is not success,
// and a growing allowlist is not free. Each test that pins a refusal was also
// mutation-checked (break the guard, watch this test go red) — a guard test
// that passes against a no-op guard is the bug this file exists to prevent.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import fs from 'node:fs'
import { execFileSync } from 'node:child_process'
import os from 'node:os'
import path from 'node:path'

import {
  ALLOWLIST_CEILING,
  analyzeMarkers,
  listModifiedUpstreamFiles,
  markerInFrontmatter,
  parseAllowlist,
  readMarkerState,
} from './fork-marker-guard.mjs'
import { MARKER_PATTERN, isMarkerLine } from './fork-marker.mjs'

// ─── the marker line vs a mention ───────────────────────────────────────────
//
// This pair is the whole point. The loose form (`/OCTO-FORK/`) accepts both,
// which is how a 21%-compliant tree reported 70 "marked" files: CLAUDE.md and
// .octorules define the rule, so they contain the token.

test('a marker line is accepted', () => {
  assert.ok(MARKER_PATTERN.test('// OCTO-FORK: the data root moved — see P1'))
})

test('prose about the rule is NOT a marker (the loose-census false positive)', () => {
  const claudeMd = [
    '| **Mark every change to an upstream file** with `// OCTO-FORK: <why> — see <design doc>` |',
    '`grep -rn "OCTO-FORK" .` is this fork’s complete diff-from-upstream inventory.',
  ].join('\n')
  assert.ok(/OCTO-FORK/.test(claudeMd), 'the fixture must contain the token, as the real file does')
  assert.equal(MARKER_PATTERN.test(claudeMd), false)
})

test('every comment introducer the fork touches is accepted', () => {
  for (const line of [
    '// OCTO-FORK: go, ts',
    '# OCTO-FORK: yaml, shell',
    '<!-- OCTO-FORK: markdown, xml -->',
    ';; OCTO-FORK: inno setup',
    '-- OCTO-FORK: lua',
  ]) {
    assert.ok(MARKER_PATTERN.test(line), `expected ${JSON.stringify(line)} to be a marker`)
  }
})

test('a block-comment continuation and indented markers are accepted', () => {
  assert.ok(MARKER_PATTERN.test('   * OCTO-FORK: inside a /* */ block'))
  assert.ok(MARKER_PATTERN.test('\t// OCTO-FORK: indented with a tab'))
})

test('isMarkerLine is the shared predicate both guards use', () => {
  // The marker guard detects with it and server-diff-guard excludes with it, so
  // a marker cannot be required by one and charged as debt by the other. It is
  // line-anchored: a line that merely contains the token mid-sentence is not a
  // marker, which is what makes `^` meaningful rather than decorative.
  assert.equal(isMarkerLine('// OCTO-FORK: why — see the design doc'), true)
  assert.equal(isMarkerLine('\t# OCTO-FORK: another introducer'), true)
  assert.equal(isMarkerLine('see // OCTO-FORK: mid-line, not at the start'), false)
  assert.equal(isMarkerLine('// mentions OCTO-FORK: in prose'), false)
  // Not global, so repeated calls must not drift via lastIndex.
  for (let i = 0; i < 3; i += 1) assert.equal(isMarkerLine('// OCTO-FORK: x'), true)
})

test('the colon is load-bearing', () => {
  // Without it, a sentence that merely names the token in a comment passes.
  assert.equal(MARKER_PATTERN.test('// see OCTO-FORK for the rule'), false)
  assert.equal(MARKER_PATTERN.test('// OCTO-FORK without a colon'), false)
})

test('the token mid-line is not a marker', () => {
  assert.equal(MARKER_PATTERN.test('const tag = "OCTO-FORK: x"'), false)
})

// ─── ratchet semantics ──────────────────────────────────────────────────────

test('at the ceiling passes and reports the split', () => {
  const { problems, notes } = analyzeMarkers({ examined: 324, missing: ['a', 'b'], ceiling: 2 })
  assert.deepEqual(problems, [])
  assert.match(notes[0], /324 modified upstream file\(s\); 322 marked, 2 missing/)
})

test('exceeding the ceiling fails and names the files', () => {
  const { problems } = analyzeMarkers({ examined: 324, missing: ['a.go', 'b.md'], ceiling: 1 })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /2 modified upstream file\(s\) carry no "OCTO-FORK:" marker \(ceiling 1\)/)
  assert.match(problems[0], /a\.go, b\.md/)
  assert.match(problems[0], /fork-marker-allowlist\.txt/)
})

test('clean is clean', () => {
  const { problems, notes } = analyzeMarkers({ examined: 324, missing: [], ceiling: 0 })
  assert.deepEqual(problems, [])
  assert.match(notes[0], /324 marked, 0 missing/)
})

// ─── the anti-no-op property (the V-49 lesson) ──────────────────────────────

test('examining zero files is a failure, not a pass', () => {
  const { problems, notes } = analyzeMarkers({ examined: 0, missing: [], ceiling: 0 })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /examined 0 modified upstream files/)
  assert.match(problems[0], /must not report success/)
  assert.deepEqual(notes, [], 'a no-op run must not also print a reassuring note')
})

// ─── the allowlist ──────────────────────────────────────────────────────────

test('an allowlist entry needs a written reason', () => {
  const { entries, problems } = parseAllowlist(
    ['b.png  Binary, no comment syntax.', 'a.ico', 'web/x.json    Generated by npm.'].join('\n'),
  )
  assert.equal(problems.length, 1)
  assert.match(problems[0], /fork-marker-allowlist\.txt:2: expected "<path>  <reason>"/)
  assert.deepEqual(
    entries.map((e) => e.path),
    ['b.png', 'web/x.json'],
  )
})

test('comments and blank lines in the allowlist are ignored', () => {
  const { entries, problems } = parseAllowlist('# a comment\n\n  \n# another\np  r')
  assert.deepEqual(problems, [])
  assert.deepEqual(entries, [{ path: 'p', reason: 'r' }])
})

test('a growing allowlist fails even when every marker is present', () => {
  const allowlisted = Array.from({ length: ALLOWLIST_CEILING + 1 }, (_, i) => ({
    path: `f${i}`,
    reason: 'r',
  }))
  const { problems } = analyzeMarkers({ examined: 10, missing: [], ceiling: 0, allowlisted })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /entries \(ceiling 6\)/)
  assert.match(problems[0], /§3\.7 stop-and-ask/)
})

test('allowlist parse problems are surfaced, not swallowed', () => {
  const { problems } = analyzeMarkers({
    examined: 10,
    missing: [],
    ceiling: 0,
    allowlisted: [],
    allowlistProblems: ['bad line'],
  })
  assert.deepEqual(problems, ['bad line'])
})

// ─── reading files ──────────────────────────────────────────────────────────

test('a file that cannot be read counts as missing, not as skipped', () => {
  const root = '/nonexistent-root'
  const read = (p) => {
    if (p.endsWith('ok.go')) return Buffer.from('// OCTO-FORK: x')
    throw new Error('EACCES')
  }
  const { marked, missing } = readMarkerState(root, ['ok.go', 'binary.icns'], read)
  assert.deepEqual(marked, ['ok.go'])
  assert.deepEqual(missing, ['binary.icns'], 'an unreadable file must not silently leave the check')
})

test('a binary file with no marker is reported missing', () => {
  const read = () => Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x00, 0xff])
  const { missing } = readMarkerState('/x', ['icon.png'], read)
  assert.deepEqual(missing, ['icon.png'])
})

// ─── git-backed gathering, against a throwaway repository ───────────────────
//
// Exercises the real `git diff` invocation: renames must be included (a rename
// is a fork change with a merge consequence) and the destination path is the
// one reported.

test('listModifiedUpstreamFiles includes renames and reports the destination path', (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'fork-marker-guard-'))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  // The injected runner takes (cwd, args), like server-diff-guard's git().
  const run = (_root, args) => execFileSync('git', args, { cwd: dir, encoding: 'utf8' })
  const sh = (args) => run(dir, args)

  sh(['init', '-q', '-b', 'main'])
  sh(['config', 'user.email', 'test@example.com'])
  sh(['config', 'user.name', 'test'])
  fs.writeFileSync(path.join(dir, 'plain.go'), 'package a\n')
  fs.writeFileSync(path.join(dir, 'moved.go'), 'package a\n')
  fs.writeFileSync(path.join(dir, 'untouched.go'), 'package a\n')
  sh(['add', '-A'])
  sh(['commit', '-q', '-m', 'base'])
  const upstream = sh(['rev-parse', 'HEAD']).trim()

  fs.writeFileSync(path.join(dir, 'plain.go'), 'package a\n// edit\n')
  sh(['mv', 'moved.go', 'renamed.go'])
  sh(['commit', '-q', '-am', 'fork work'])

  const files = listModifiedUpstreamFiles(dir, upstream, run)
  assert.deepEqual(files.sort(), ['plain.go', 'renamed.go'])
  assert.ok(!files.includes('untouched.go'), 'an untouched upstream file must not be listed')
})

// The census reads the working tree, so `make marker-check` just before a commit
// checks the change being committed. A HEAD-based diff would report success on
// the previous commit instead — a guard validating something other than what it
// was run to check (V-49, one layer down).
//
// The fixture has to separate the two revisions, so the file that discriminates
// is edited ONLY in the working tree: untouched at HEAD, modified on disk.
test('an uncommitted edit to an upstream file is examined, not just HEAD', (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'fork-marker-guard-dirty-'))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  const run = (_root, args) => execFileSync('git', args, { cwd: dir, encoding: 'utf8' })
  const sh = (args) => run(dir, args)

  sh(['init', '-q', '-b', 'main'])
  sh(['config', 'user.email', 'test@example.com'])
  sh(['config', 'user.name', 'test'])
  fs.writeFileSync(path.join(dir, 'at-head.go'), 'package a\n')
  fs.writeFileSync(path.join(dir, 'only-on-disk.go'), 'package a\n')
  sh(['add', '-A'])
  sh(['commit', '-q', '-m', 'base'])
  const upstream = sh(['rev-parse', 'HEAD']).trim()

  // A committed change, so the branch is not identical to upstream...
  fs.writeFileSync(path.join(dir, 'at-head.go'), 'package a\n// OCTO-FORK: committed\n')
  sh(['commit', '-q', '-am', 'committed marker'])
  // ...and an edit that exists ONLY in the working tree.
  fs.writeFileSync(path.join(dir, 'only-on-disk.go'), 'package a\n// uncommitted edit\n')

  // The premise: HEAD alone would not see this file, so the two diff revisions
  // really do disagree and the assertion below is not vacuous.
  const atHead = sh(['diff', '--diff-filter=MR', '-M', '--name-only', upstream, 'HEAD'])
  assert.ok(
    !atHead.split('\n').includes('only-on-disk.go'),
    'fixture is wrong: HEAD already contains the discriminating edit',
  )

  const files = listModifiedUpstreamFiles(dir, upstream, run)
  assert.ok(files.includes('at-head.go'), 'the committed edit is still in range')
  assert.ok(files.includes('only-on-disk.go'), 'a working-tree-only edit must be examined')
})

// The same gathering against this repository, asserting properties rather than
// a fixed file list so the test does not go stale on the next upstream merge.
test('the gathering is self-consistent against the real repository', () => {
  const root = path.resolve(import.meta.dirname, '..')
  const upstream = execFileSync('git', ['rev-parse', '--verify', '--quiet', 'origin/main^{commit}'], {
    cwd: root,
    encoding: 'utf8',
  })
    .trim()
    .slice(0, 7) || 'origin/main'
  const files = listModifiedUpstreamFiles(root, 'origin/main', (r, args) =>
    execFileSync('git', args, { cwd: r, encoding: 'utf8' }),
  )
  assert.ok(files.length > 0, 'the fork must show a diff against upstream')
  for (const f of files) {
    assert.ok(fs.existsSync(path.join(root, f)), `${f} was reported modified but does not exist`)
  }
  assert.ok(upstream.length > 0)
})

// ─── marker placement: not inside YAML frontmatter ──────────────────────────
//
// Proved the hard way: an HTML comment in a SKILL.md frontmatter parses as a
// mapping key `<!-- OCTO-FORK` rather than failing, so the skill still loads and
// four shipped default skills carried a junk key with every test green.

test('a marker inside frontmatter is located', () => {
  const file = ['---', 'name: x', '<!-- OCTO-FORK: why -->', 'description: d', '---', 'body'].join('\n')
  assert.equal(markerInFrontmatter(file), 3)
})

test('a marker after the frontmatter is fine', () => {
  const file = ['---', 'name: x', 'description: d', '---', '<!-- OCTO-FORK: why -->', 'body'].join('\n')
  assert.equal(markerInFrontmatter(file), null)
})

test('no frontmatter at all is fine', () => {
  assert.equal(markerInFrontmatter('// OCTO-FORK: why\npackage a\n'), null)
})

test('an unterminated frontmatter is not used to invent a placement error', () => {
  assert.equal(markerInFrontmatter(['---', 'name: x'].join('\n')), null)
})

test('analyzeMarkers reports a misplaced marker', () => {
  const { problems } = analyzeMarkers({
    examined: 10,
    missing: [],
    ceiling: 0,
    misplaced: [{ file: 'internal/skills/x/SKILL.md', line: 3 }],
  })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /internal\/skills\/x\/SKILL\.md:3/)
  assert.match(problems[0], /YAML frontmatter block/)
})

test('readMarkerState surfaces misplacement for a marked file', () => {
  const read = () => Buffer.from(['---', 'name: x', '<!-- OCTO-FORK: why -->', '---'].join('\n'))
  const { marked, missing, misplaced } = readMarkerState('/x', ['s/SKILL.md'], read)
  assert.deepEqual(marked, ['s/SKILL.md'])
  assert.deepEqual(missing, [])
  assert.deepEqual(misplaced, [{ file: 's/SKILL.md', line: 3 }])
})
