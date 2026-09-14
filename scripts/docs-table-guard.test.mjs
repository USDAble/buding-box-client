// Tests for scripts/docs-table-guard.mjs.
//
// The guard's whole value is that it reads cells instead of lines, so the tests
// are ordered around that: what counts as a cell boundary, then the two
// diagnosed defect shapes, then the shapes it must NOT flag (which is where a
// table parser usually goes wrong), then the refusal to pass vacuously.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'

import { checkDocument, check, splitRow, isDelimiterRow, SCAN_ROOTS } from './docs-table-guard.mjs'

// ─── what a cell boundary is ────────────────────────────────────────────────

test('splitRow splits on pipes and trims each cell', () => {
  assert.deepEqual(splitRow('| a | b | c |'), ['a', 'b', 'c'])
  assert.deepEqual(splitRow('  |  a  |  b  |  '), ['a', 'b'])
})

test('splitRow returns null for a line that is not a row', () => {
  assert.equal(splitRow('plain prose'), null)
  assert.equal(splitRow(''), null)
  assert.equal(splitRow('   '), null)
})

test('an escaped pipe is not a cell boundary', () => {
  // This is the fix the guard recommends, so it has to be the thing it accepts.
  assert.deepEqual(splitRow('| `terminal\\|read_file` | x |'), ['`terminal\\|read_file`', 'x'])
})

test('an even backslash run leaves the pipe a boundary', () => {
  // `\\|` is a literal backslash followed by a delimiter, not an escaped pipe.
  assert.deepEqual(splitRow('| a\\\\ | b |'), ['a\\\\', 'b'])
})

test('a row written without a closing pipe keeps its last cell', () => {
  assert.deepEqual(splitRow('| a | b'), ['a', 'b'])
})

test('isDelimiterRow recognises the delimiter and nothing else', () => {
  assert.equal(isDelimiterRow(splitRow('| --- | --- |')), true)
  assert.equal(isDelimiterRow(splitRow('| :--- | ---: |')), true)
  assert.equal(isDelimiterRow(splitRow('| --- | cell |')), false)
  assert.equal(isDelimiterRow(splitRow('| a | b |')), false)
  assert.equal(isDelimiterRow(null), false)
})

// ─── the defect shapes that were actually observed ──────────────────────────

test('a row that is a cell short is reported with both counts', () => {
  const doc = ['| 文档 | 是什么 | 谁看 |', '| --- | --- | --- |', '| R | 索引 |'].join('\n')
  const { problems, tables } = checkDocument('doc.md', doc)
  assert.equal(tables, 1)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /doc\.md:3/)
  assert.match(problems[0], /2 cell\(s\), header has 3/)
})

test('two records joined into one line is reported as an over-long row', () => {
  // This is 需求基线.md §6's defect: a missing newline while appending turned
  // two two-cell revision records into one four-cell row, which is why V-46 was
  // invisible for days.
  const doc = [
    '| 日期 | 说明 |',
    '| --- | --- |',
    '| 2026-09-12 | 第一条记录 | 2026-09-12 | 第二条记录 |',
  ].join('\n')
  const { problems } = checkDocument('doc.md', doc)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /4 cell\(s\), header has 2/)
})

test('an unescaped pipe inside a code span is reported', () => {
  // 开发计划.md had a `-tool=terminal|read_file` flag in a code span. GFM splits
  // on it, so the row really does have an extra column.
  const doc = ['| 开关 | 说明 |', '| --- | --- |', '| `-tool=terminal|read_file` | 走查开关 |'].join('\n')
  const { problems } = checkDocument('doc.md', doc)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /\\\| /)
})

// ─── shapes that must NOT be flagged ────────────────────────────────────────

test('a delimiter row that disagrees with the header is reported', () => {
  // GFM takes the column count from the delimiter row, so a three-cell header
  // above a two-cell delimiter silently loses its third column.
  const doc = ['| a | b | c |', '| --- | --- |', '| 1 | 2 | 3 |'].join('\n')
  const { problems } = checkDocument('doc.md', doc)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /delimiter row has 2 cell\(s\), header has 3/)
})

test('a well-formed table is clean', () => {
  const doc = [
    '| a | b | c |',
    '| --- | --- | --- |',
    '| 1 | 2 | 3 |',
    '| 4 | 5 | 6 |',
  ].join('\n')
  assert.deepEqual(checkDocument('doc.md', doc).problems, [])
})

test('a table inside a fenced code block is not a table', () => {
  // The guard's own header comment and this repo's docs quote malformed tables
  // as examples; a fence is how a document says "this is not a table".
  const doc = ['```', '| a | b |', '| --- | --- |', '| only-one |', '```'].join('\n')
  assert.equal(checkDocument('doc.md', doc).tables, 0)
  assert.deepEqual(checkDocument('doc.md', doc).problems, [])
})

test('a second table after prose keeps its own header count', () => {
  // Regression guard for the header state: a two-column table following a
  // three-column one must not inherit the first one's count.
  const doc = [
    '| a | b | c |',
    '| --- | --- | --- |',
    '| 1 | 2 | 3 |',
    '',
    'prose',
    '',
    '| x | y |',
    '| --- | --- |',
    '| 1 | 2 |',
  ].join('\n')
  const { problems, tables } = checkDocument('doc.md', doc)
  assert.equal(tables, 2)
  assert.deepEqual(problems, [])
})

test('a row with no table above it is reported, not ignored', () => {
  // This test used to assert the opposite ("not a table, and no problem"). That
  // expectation is what let V-70 through: a blank line inside a table ends it,
  // so every row below lost its header and was never compared against anything —
  // fifty rows in 需求基线 §5.6 were rendering as pipe text while this guard
  // reported a clean tree. Silence was the bug, so the assertion changed.
  const doc = ['| a | b |', '', '| c | d |'].join('\n')
  const { problems, tables } = checkDocument('doc.md', doc)
  assert.equal(tables, 0)
  assert.equal(problems.length, 2)
  for (const p of problems) assert.match(p, /row with no table above it/)
})

test('a blank line that cuts a table in two names every stranded row', () => {
  const doc = ['| a | b |', '| --- | --- |', '| 1 | 2 |', '', '| 3 | 4 |', '| 5 | 6 |'].join('\n')
  const { problems, tables } = checkDocument('doc.md', doc)
  assert.equal(tables, 1)
  assert.equal(problems.length, 2)
  assert.match(problems[0], /doc\.md:5:/)
  assert.match(problems[1], /doc\.md:6:/)
})

test('a blank line before a new header is a separator, not a cut', () => {
  // The distinction the rule needs: both are "a blank line between two rows", but
  // the row below this one is the header of its own table.
  const doc = [
    '| a | b |',
    '| --- | --- |',
    '| 1 | 2 |',
    '',
    '| c | d |',
    '| --- | --- |',
    '| 3 | 4 |',
  ].join('\n')
  const { problems, tables } = checkDocument('doc.md', doc)
  assert.equal(tables, 2)
  assert.deepEqual(problems, [])
})

test('prose containing a pipe is not a row', () => {
  // Only a line that starts with `|` is a row; otherwise every sentence with a
  // vertical bar in it would look like a one-cell table.
  const doc = ['The regex is `Gateway|Ordinary|Refusal`.', '', '| a |', '| --- |', '| 1 |'].join('\n')
  const { problems, tables } = checkDocument('doc.md', doc)
  assert.equal(tables, 1)
  assert.deepEqual(problems, [])
})

// ─── the scan must not pass vacuously ───────────────────────────────────────

test('the scan roots cover this fork own docs and not upstream', () => {
  assert.deepEqual(SCAN_ROOTS, ['dev-docs-usdable'])
})

test('check reports a defect found in a real file on disk', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'docs-table-guard-'))
  try {
    const dir = path.join(root, 'dev-docs-usdable')
    await fs.mkdir(dir, { recursive: true })
    await fs.writeFile(path.join(dir, 'a.md'), '| a | b |\n| --- | --- |\n| 1 |\n')
    const { problems, notes } = await check(root)
    assert.equal(problems.length, 1)
    assert.match(problems[0], /dev-docs-usdable\/a\.md:3/)
    assert.ok(notes.some((n) => n.includes('1 file(s), 1 table(s)')))
  } finally {
    await fs.rm(root, { recursive: true, force: true })
  }
})

test('examining zero files is a failure, not a pass', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'docs-table-guard-'))
  try {
    const { problems } = await check(root)
    assert.equal(problems.length, 1)
    assert.match(problems[0], /examined nothing/)
  } finally {
    await fs.rm(root, { recursive: true, force: true })
  }
})

test('files but no recognised table header is a parser failure, not a clean tree', async () => {
  // The failure mode this pins: a parser that stops recognising tables reports
  // zero problems for every input, forever, while looking green.
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'docs-table-guard-'))
  try {
    const dir = path.join(root, 'dev-docs-usdable')
    await fs.mkdir(dir, { recursive: true })
    await fs.writeFile(path.join(dir, 'a.md'), '# prose only\n')
    const { problems } = await check(root)
    assert.equal(problems.length, 1)
    assert.match(problems[0], /no table headers recognised/)
  } finally {
    await fs.rm(root, { recursive: true, force: true })
  }
})
