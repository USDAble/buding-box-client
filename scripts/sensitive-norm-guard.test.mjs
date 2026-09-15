// Tests for scripts/sensitive-norm-guard.mjs.
//
// Three properties are load-bearing and all are tested here, because a guard
// that cannot fail is worse than no guard:
//   - drift in either direction is reported, with the characters named;
//   - a renamed constant is a failure, not a quiet pass (the V-49 shape);
//   - reflowing a concatenation (or joining it onto one line) is NOT drift.

import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'

import {
  GO_CONST,
  GO_REL,
  TS_CONST,
  TS_REL,
  check,
  extractConcatenation,
  readTable,
  repositoryRoot,
  scanLiteral,
} from './sensitive-norm-guard.mjs'

const root = repositoryRoot(import.meta.url)

// The two tables as the repository actually ships them, abbreviated to the
// cases these tests need.
const GO_TABLE = `const ${GO_CONST} = "!@#" +\n\t"（）"\n`
const TS_TABLE = `const ${TS_CONST} = '!@#' +\n  '（）'\n`

// makeTree writes a minimal repo layout — the two files the guard reads and
// nothing else — and returns its root.
async function makeTree(goSource, tsSource) {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'sensitive-norm-guard-'))
  await fs.mkdir(path.join(dir, path.dirname(GO_REL)), { recursive: true })
  await fs.mkdir(path.join(dir, path.dirname(TS_REL)), { recursive: true })
  await fs.writeFile(path.join(dir, GO_REL), goSource)
  await fs.writeFile(path.join(dir, TS_REL), tsSource)
  return dir
}

test('the committed tree passes the guard', async () => {
  assert.deepEqual(await check(root), [])
})

test('the committed tables are equal as character sets', async () => {
  const go = await readTable(root, GO_REL, GO_CONST)
  const ts = await readTable(root, TS_REL, TS_CONST)
  // Sorted comparison: order is deliberately not asserted (see the header).
  assert.deepEqual([...new Set([...ts.value])].sort(), [...new Set([...go.value])].sort())
})

test('extractConcatenation joins a multi-line literal list', () => {
  const found = extractConcatenation(GO_TABLE, GO_CONST)
  assert.equal(found.value, '!@#（）')
  assert.deepEqual(found.parts, ['!@#', '（）'])
  assert.equal(found.line, 1)
})

test('extractConcatenation accepts a single literal', () => {
  assert.equal(extractConcatenation(`const ${GO_CONST} = "!@#"\n`, GO_CONST).value, '!@#')
})

test('extractConcatenation accepts a one-line concatenation', () => {
  assert.equal(extractConcatenation(`const ${GO_CONST} = "!@" + "#"\n`, GO_CONST).value, '!@#')
})

test('extractConcatenation accepts the operator at the start of the next line', () => {
  const source = `const ${GO_CONST} = "!@"\n\t+ "#"\n`
  assert.equal(extractConcatenation(source, GO_CONST).value, '!@#')
})

test('extractConcatenation accepts an export const', () => {
  assert.equal(extractConcatenation(`export const ${TS_CONST} = 'ab'\n`, TS_CONST).value, 'ab')
})

test('extractConcatenation reports the declaration line, not the file start', () => {
  assert.equal(extractConcatenation(`// lead-in\n\n${GO_TABLE}`, GO_CONST).line, 3)
})

test('extractConcatenation returns null when the declaration is absent', () => {
  assert.equal(extractConcatenation(GO_TABLE, 'someOtherName'), null)
})

test('extractConcatenation throws rather than silently narrowing the expression', () => {
  // A computed table would make "the same characters" unanswerable.
  assert.throws(() => extractConcatenation(`const ${GO_CONST} = someArray.join('')\n`, GO_CONST), /is not a list of string literals/)
  // A trailing "+" means the expression continues; reading only the first part
  // would compare a truncated table and report success.
  assert.throws(() => extractConcatenation(`const ${GO_CONST} = "a" +\n`, GO_CONST), /no literal after it/)
})

test('extractConcatenation stops at the end of the expression', () => {
  const source = `const ${GO_CONST} = "!@#"\n\nconst other = "should not be read"\n`
  assert.equal(extractConcatenation(source, GO_CONST).value, '!@#')
})

test('scanLiteral evaluates both quote styles and their escapes', () => {
  assert.equal(scanLiteral(`'a\\'b'`, 0).value, "a'b")
  assert.equal(scanLiteral(`"c\\"d"`, 0).value, 'c"d')
  assert.equal(scanLiteral(`'e\\\\f'`, 0).value, 'e\\f')
  assert.equal(scanLiteral(`"\\n\\t"`, 0).value, '\n\t')
})

test('scanLiteral refuses what it cannot evaluate', () => {
  assert.throws(() => scanLiteral(`'unterminated`, 0), /unterminated string literal/)
  assert.throws(() => scanLiteral(`'a\nb'`, 0), /unterminated string literal/)
  assert.throws(() => scanLiteral(`'\\u00e9'`, 0), /unsupported escape/)
})

test('a character the frontend fails to drop is reported with both sides', async () => {
  const dir = await makeTree(GO_TABLE, `const ${TS_CONST} = '!@#' +\n  '（'\n`)
  const problems = await check(dir)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /^web\/src\/lib\/sensitiveDict\.ts:1 \(STRIP_SYMBOLS\) does not drop/)
  // The missing character is named, and the owner is pointed at.
  assert.match(problems[0], /"）"/)
  assert.match(problems[0], /internal\/sensitive\/normalize\.go:1 \(stripSymbols\)/)
})

test('a character only the frontend drops is reported too', async () => {
  const dir = await makeTree(GO_TABLE, `const ${TS_CONST} = '!@#' +\n  '（）' +\n  '~'\n`)
  const problems = await check(dir)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /drops "~"/)
})

test('a re-split of the same characters is not drift', async () => {
  // Guards the guard: an editor may reflow the tables. Reflowing must not be
  // reported as drift.
  const dir = await makeTree(`const ${GO_CONST} = "!@#" + "（）"\n`, `const ${TS_CONST} = '!@#（）'\n`)
  assert.deepEqual(await check(dir), [])
})

test('a missing comparison target is a problem, not a silent pass', async () => {
  const dir = await makeTree(GO_TABLE, `const SOMETHING_ELSE = '!@#' + '（）'\n`)
  const problems = await check(dir)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /no `const STRIP_SYMBOLS =/)
})

test('both renames are reported, one line each', async () => {
  const dir = await makeTree(`const a = "!@#" + "（）"\n`, `const b = '!@#' + '（）'\n`)
  assert.equal((await check(dir)).length, 2)
})

test('an unreadable-destination file is reported, not thrown', async () => {
  const dir = await makeTree(GO_TABLE, `const ${TS_CONST} = '!@#' + '（）'\n`)
  await fs.rm(path.join(dir, TS_REL))
  const problems = await check(dir)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /sensitiveDict\.ts: file not found/)
})

test('a non-literal table is reported, not thrown', async () => {
  const dir = await makeTree(GO_TABLE, `const ${TS_CONST} = SYMBOLS.split('').join('')\n`)
  const problems = await check(dir)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /is not a list of string literals/)
})
