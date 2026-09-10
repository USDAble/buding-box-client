// Tests for scripts/package-portable.mjs. The build/assemble steps shell out
// to `go` and are exercised in CI, not here; the unit tests cover the pure
// pieces — target resolution, 使用说明.txt rendering, the self-check
// predicates (positive and negative), and the zero-dependency ZIP writer
// (round-tripped through a hand-rolled reader so no unzip tool is needed).

import assert from 'node:assert/strict'
import { test } from 'node:test'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'

import {
  resolveTarget,
  renderUsageNote,
  checkDataFiles,
  checkDevResiduals,
  checkNoAbsolutePaths,
  checkProductionBinary,
  listFiles,
  crc32,
  buildZipBuffer,
  buildZipEntries,
  DATA_FILES,
} from './package-portable.mjs'

async function tmpdir(t) {
  const d = await fs.mkdtemp(path.join(os.tmpdir(), 'p12-'))
  t.after(() => fs.rm(d, { recursive: true, force: true }))
  return d
}

const brand = {
  product: { names: { 'zh-CN': '布丁盒子', 'en-US': 'Pudding Box' } },
  identifiers: { current: { exeName: 'PuddingBox.exe', portableDirName: 'PuddingBox' } },
}

test('resolveTarget: defaults to windows/amd64 and -dev version', () => {
  assert.deepEqual(resolveTarget({}), { goos: 'windows', goarch: 'amd64', version: '0.0.0-dev' })
  assert.deepEqual(resolveTarget({ GOOS: 'windows', GOARCH: 'arm64', VERSION: 'v0.5.0' }), {
    goos: 'windows',
    goarch: 'arm64',
    version: '0.5.0',
  })
})

test('renderUsageNote: replaces brand placeholders', () => {
  const out = renderUsageNote('双击 {exeName} 启动 {nameZh} / {nameEn}。', brand)
  assert.equal(out, '双击 PuddingBox.exe 启动 布丁盒子 / Pudding Box。')
})

test('checkDataFiles: passes on the pre-filled items', () => {
  const files = [
    { rel: 'data', dir: true },
    { rel: 'data/sensitive-words.txt', dir: false },
    { rel: 'data/config.yml', dir: false },
    { rel: 'data/workspace', dir: true },
  ]
  assert.deepEqual(checkDataFiles(files), [])
})

test('checkDataFiles: fails on a missing data/ file', () => {
  const files = [
    { rel: 'data', dir: true },
    { rel: 'data/sensitive-words.txt', dir: false },
    { rel: 'data/workspace', dir: true },
  ]
  assert.deepEqual(checkDataFiles(files), ['data/ missing config.yml'])
})

// chat-modes.json must NOT be pre-filled: its factory content was the four
// deleted buding-* fake models (P12 §3.1). A missing file already means the
// factory grouping, so shipping it would only re-ship fake data.
test('checkDataFiles: does not require chat-modes.json', () => {
  const files = [
    { rel: 'data', dir: true },
    { rel: 'data/sensitive-words.txt', dir: false },
    { rel: 'data/config.yml', dir: false },
    { rel: 'data/workspace', dir: true },
  ]
  assert.ok(!DATA_FILES.includes('chat-modes.json'))
  assert.deepEqual(checkDataFiles(files), [])
})

// The shipped binary must not contain the P11 fake-channel reply text or its
// PII log path — the build tag is what removes them, so their presence means
// the production tag did not take effect.
test('checkProductionBinary: passes on a binary without the fake channel', () => {
  const buf = Buffer.from('a production binary with no demo content', 'utf8')
  assert.deepEqual(checkProductionBinary(buf), [])
})

test('checkProductionBinary: flags the demo reply text', () => {
  const buf = Buffer.from('…本地演示模型的固定回复…', 'utf8')
  const problems = checkProductionBinary(buf)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /product_production/)
})

test('checkProductionBinary: flags the PII log path', () => {
  const problems = checkProductionBinary(Buffer.from('logs/local-provider.jsonl', 'utf8'))
  assert.equal(problems.length, 1)
})

test('checkProductionBinary: flags both when the tag is missing entirely', () => {
  const buf = Buffer.from('本地演示模型的固定回复 logs/local-provider.jsonl', 'utf8')
  assert.equal(checkProductionBinary(buf).length, 2)
})

test('checkDataFiles: fails when data/ is absent', () => {
  assert.deepEqual(checkDataFiles([]), ['missing data/ directory'])
})

test('checkDevResiduals: flags .pdb, .map, node_modules and .git', () => {
  const files = [
    { rel: 'PuddingBox.exe', dir: false },
    { rel: 'a.pdb', dir: false },
    { rel: 'b.map', dir: false },
    { rel: 'node_modules', dir: true },
    { rel: '.git', dir: true },
  ]
  assert.deepEqual(checkDevResiduals(files), [
    'dev residual file: a.pdb',
    'dev residual file: b.map',
    'dev residual dir: node_modules',
    'dev residual dir: .git',
  ])
})

test('checkNoAbsolutePaths: flags a file containing the build-machine path', async (t) => {
  const dir = await tmpdir(t)
  await fs.writeFile(path.join(dir, 'clean.txt'), 'no paths here')
  await fs.writeFile(path.join(dir, 'leaky.txt'), `built at ${os.homedir()}/repo`)

  const files = await listFiles(dir)
  assert.deepEqual(await checkNoAbsolutePaths(files, [os.homedir()]), [
    `leaky.txt contains build-machine path ${os.homedir()}`,
  ])
})

test('crc32: matches the standard check value', () => {
  assert.equal(crc32(Buffer.from('123456789')), 0xcbf43926)
})

// readZip is a minimal central-directory reader used only to round-trip what
// buildZipBuffer writes, so the test asserts real structure, not just length.
function readZip(buf) {
  let eocd = -1
  for (let i = buf.length - 22; i >= 0; i--) {
    if (buf.readUInt32LE(i) === 0x06054b50) {
      eocd = i
      break
    }
  }
  assert.notEqual(eocd, -1, 'missing end-of-central-directory')
  const count = buf.readUInt16LE(eocd + 10)
  const cdOffset = buf.readUInt32LE(eocd + 16)

  const entries = []
  let p = cdOffset
  for (let i = 0; i < count; i++) {
    assert.equal(buf.readUInt32LE(p), 0x02014b50, 'central directory signature')
    const nameLen = buf.readUInt16LE(p + 28)
    const extraLen = buf.readUInt16LE(p + 30)
    const commentLen = buf.readUInt16LE(p + 32)
    const localOff = buf.readUInt32LE(p + 42)
    const name = buf.toString('utf8', p + 46, p + 46 + nameLen)

    assert.equal(buf.readUInt32LE(localOff), 0x04034b50, 'local header signature')
    const lNameLen = buf.readUInt16LE(localOff + 26)
    const lExtraLen = buf.readUInt16LE(localOff + 28)
    const cSize = buf.readUInt32LE(localOff + 18)
    const dataStart = localOff + 30 + lNameLen + lExtraLen
    entries.push({ name, data: Buffer.from(buf.subarray(dataStart, dataStart + cSize)) })

    p += 46 + nameLen + extraLen + commentLen
  }
  return entries
}

test('buildZipBuffer: round-trips names (incl. UTF-8) and data', () => {
  const entries = [
    { name: 'PuddingBox/使用说明.txt', data: Buffer.from('你好') },
    { name: 'PuddingBox/PuddingBox.exe', data: Buffer.from([0, 1, 2, 3]) },
  ]
  const back = readZip(buildZipBuffer(entries))
  assert.equal(back.length, 2)
  assert.equal(back[0].name, 'PuddingBox/使用说明.txt')
  assert.equal(back[0].data.toString('utf8'), '你好')
  assert.equal(back[1].name, 'PuddingBox/PuddingBox.exe')
  assert.deepEqual(back[1].data, Buffer.from([0, 1, 2, 3]))
})

test('buildZipEntries: prefixes every entry with the base dir name', async (t) => {
  const dir = await tmpdir(t)
  await fs.mkdir(path.join(dir, 'data', 'workspace'), { recursive: true })
  await fs.writeFile(path.join(dir, 'PuddingBox.exe'), 'exe')
  await fs.writeFile(path.join(dir, 'data', 'config.yml'), '# c')

  const entries = await buildZipEntries(dir, 'PuddingBox')
  assert.deepEqual(
    entries.map((e) => e.name).sort(),
    ['PuddingBox/PuddingBox.exe', 'PuddingBox/data/config.yml'].sort(),
  )
})
