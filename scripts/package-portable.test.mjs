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
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

import {
  resolveTarget,
  resolvePackageProfile,
  renderUsageNote,
  checkDataFiles,
  checkDevResiduals,
  checkNoAbsolutePaths,
  checkProductionBinary,
  checkVendoredPayloadPlatforms,
  payloadPlatformMismatch,
  vendoredBinarySources,
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

test('resolvePackageProfile: separates sealed production and test artifacts', () => {
  assert.deepEqual(resolvePackageProfile({}), {
    name: 'production',
    buildTags: 'embedrg product_production',
    suffix: '',
  })
  assert.deepEqual(resolvePackageProfile({ PACKAGE_PROFILE: 'test' }), {
    name: 'test',
    buildTags: 'embedrg product_test',
    suffix: '-test',
  })
  assert.throws(() => resolvePackageProfile({ PACKAGE_PROFILE: 'developer' }), /unknown PACKAGE_PROFILE/)
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

// The V-107 shape, reproduced without a Windows runner. The coincidence that
// broke the check is `os.homedir()` on the runner being the same string as the
// path inside a vendored binary, so these tests pin the property that makes the
// verdict independent of that: a hit is exempt only if it lies inside a
// vendored payload's exact bytes.
const FOREIGN = 'C:\\Users\\runneradmin'

test('checkNoAbsolutePaths: a hit inside a vendored payload is not ours (V-107)', async (t) => {
  const dir = await tmpdir(t)
  // A stand-in for ripgrep: a third-party artifact that carries its author's
  // build path, exactly as the real one carries `C:\Users\runneradmin`.
  const payload = Buffer.concat([Buffer.from('rustc\x00\x00'), Buffer.from(FOREIGN), Buffer.from('\\.cargo\\registry')])
  const vendored = path.join(dir, 'vendored-rg')
  await fs.writeFile(vendored, payload)
  // An artefact that embeds it verbatim, as go:embed does.
  await fs.writeFile(path.join(dir, 'app.exe'), Buffer.concat([Buffer.from('PE\x00\x00'), payload, Buffer.from('tail')]))

  const files = await listFiles(dir)
  assert.deepEqual(await checkNoAbsolutePaths(files, [FOREIGN], { vendoredPaths: [vendored] }), [])
})

test('checkNoAbsolutePaths: the exemption does not extend past the payload', async (t) => {
  const dir = await tmpdir(t)
  const payload = Buffer.from(`prefix${FOREIGN}suffix`)
  const vendored = path.join(dir, 'vendored-rg')
  await fs.writeFile(vendored, payload)
  // Once inside the payload, once outside it: the outside hit is still ours.
  await fs.writeFile(
    path.join(dir, 'app.exe'),
    Buffer.concat([payload, Buffer.from(' ... and again '), Buffer.from(FOREIGN)]),
  )

  const files = await listFiles(dir)
  assert.deepEqual(await checkNoAbsolutePaths(files, [FOREIGN], { vendoredPaths: [vendored] }), [
    `app.exe contains build-machine path ${FOREIGN}`,
  ])
})

test('checkNoAbsolutePaths: a payload that is not staged exempts nothing', async (t) => {
  const dir = await tmpdir(t)
  await fs.writeFile(path.join(dir, 'app.exe'), `built at ${FOREIGN}/repo`)

  const files = await listFiles(dir)
  const missing = path.join(dir, 'never-staged')
  // Fail-closed: an unreadable payload must not turn the check into a pass.
  assert.deepEqual(await checkNoAbsolutePaths(files, [FOREIGN], { vendoredPaths: [missing] }), [
    `app.exe contains build-machine path ${FOREIGN}`,
  ])
})

test('vendoredBinarySources: discovers payloads by convention, not by list', async (t) => {
  const root = await tmpdir(t)
  // The three conventions: an embed dir, any embedded wasm, and the tools
  // copied into the package. A hand-written list of two went stale on
  // `mruby.wasm`, which carries `wasi-sdk` build paths of its own.
  const put = async (rel, body) => {
    const abs = path.join(root, rel)
    await fs.mkdir(path.dirname(abs), { recursive: true })
    await fs.writeFile(abs, body)
  }
  await put('internal/tools/rgembed/binaries/rg', 'rg-bytes')
  await put('internal/tools/rgembed/binaries/README.md', 'not embedded')
  await put('internal/tools/rgembed/binaries/.gitignore', 'rg')
  await put('internal/workflow/mruby.wasm', '\0asm')
  await put('dist/bundled-tools/windows-amd64/uv.exe', 'uv-bytes')

  const found = (await vendoredBinarySources(root)).map((p) => path.relative(root, p))
  assert.deepEqual(found, [
    path.join('dist', 'bundled-tools', 'windows-amd64', 'uv.exe'),
    path.join('internal', 'tools', 'rgembed', 'binaries', 'rg'),
    path.join('internal', 'workflow', 'mruby.wasm'),
  ])
})

test('vendoredBinarySources: a checkout with nothing staged yields an empty set', async (t) => {
  const root = await tmpdir(t)
  // Not an error: nothing staged means nothing embedded, so every hit stays
  // attributed to us.
  assert.deepEqual(await vendoredBinarySources(root), [])
})

// ── V-108: the staged payload has to be the target's binary ────────────────
//
// The bug these cover: `binaries/rg` is one path for every platform, so a
// checkout that had staged a macOS rg and then built for windows embedded the
// macOS binary and reported success. Nothing downstream noticed — the bytes are
// a valid file, just not one windows can exec.

const MZ = Buffer.concat([Buffer.from('MZ'), Buffer.alloc(60)])
const ELF = Buffer.concat([Buffer.from([0x7f, 0x45, 0x4c, 0x46]), Buffer.alloc(60)])
const MACHO = Buffer.concat([Buffer.from([0xcf, 0xfa, 0xed, 0xfe]), Buffer.alloc(60)])

test('payloadPlatformMismatch: accepts each platform its own header', () => {
  assert.equal(payloadPlatformMismatch(MZ, 'windows'), null)
  assert.equal(payloadPlatformMismatch(ELF, 'linux'), null)
  assert.equal(payloadPlatformMismatch(MACHO, 'darwin'), null)
})

test('payloadPlatformMismatch: names the header when the platform is wrong', () => {
  // The shape of the failure matters: it has to say what was found, because
  // "wrong platform" alone does not tell you which build staged it.
  assert.match(payloadPlatformMismatch(MACHO, 'windows'), /cffaedfe/)
  assert.match(payloadPlatformMismatch(MZ, 'linux'), /4d5a/)
  assert.match(payloadPlatformMismatch(ELF, 'darwin'), /7f454c46/)
})

test('payloadPlatformMismatch: a truncated file is a failure, not a pass', () => {
  assert.match(payloadPlatformMismatch(Buffer.from('MZ'), 'windows'), /too short/)
})

test('payloadPlatformMismatch: an unknown platform asserts nothing', () => {
  // Fail-open here is deliberate and bounded: this check exists to catch a
  // payload staged for a platform we *do* know about. Guessing a rule for a
  // platform whose headers we have not verified would produce false failures.
  assert.equal(payloadPlatformMismatch(MZ, 'plan9'), null)
})

test('checkVendoredPayloadPlatforms: reports the mismatch against the repo root', async (t) => {
  const root = await tmpdir(t)
  const staged = path.join(root, 'internal', 'tools', 'rgembed', 'binaries', 'rg')
  await fs.mkdir(path.dirname(staged), { recursive: true })
  await fs.writeFile(staged, MACHO)

  assert.deepEqual(await checkVendoredPayloadPlatforms([staged], 'windows', { root }), [
    `vendored payload ${path.join('internal', 'tools', 'rgembed', 'binaries', 'rg')} cannot run on windows: its header is cffaedfe, which is not a windows binary`,
  ])
  assert.deepEqual(await checkVendoredPayloadPlatforms([staged], 'darwin', { root }), [])
})

test('checkVendoredPayloadPlatforms: wasm is platform-neutral', async (t) => {
  const root = await tmpdir(t)
  const wasm = path.join(root, 'internal', 'workflow', 'mruby.wasm')
  await fs.mkdir(path.dirname(wasm), { recursive: true })
  // The wasm magic is not any host's magic, and must not be read as one.
  await fs.writeFile(wasm, Buffer.from('\0asm\x01\0\0\0'))

  assert.deepEqual(await checkVendoredPayloadPlatforms([wasm], 'windows', { root }), [])
})

test('checkVendoredPayloadPlatforms: an absent payload is not this check\u2019s business', async (t) => {
  const root = await tmpdir(t)
  // `selfCheck` runs over the same discovered set that may legitimately be
  // empty (a CLI-only checkout stages no uv.exe); missing must not fail here.
  assert.deepEqual(
    await checkVendoredPayloadPlatforms([path.join(root, 'dist', 'bundled-tools', 'windows-amd64', 'uv.exe')], 'windows', { root }),
    [],
  )
})

// The Makefile asks this file rather than restating the magic numbers, so the
// CLI contract is load-bearing: `make rg-embed` fails the build on a non-zero
// exit and writes no stamp.
test('CLI --check-payload-platform: exit code is the contract the Makefile reads', async (t) => {
  const dir = await tmpdir(t)
  const ok = path.join(dir, 'rg.exe')
  const bad = path.join(dir, 'rg')
  await fs.writeFile(ok, MZ)
  await fs.writeFile(bad, MACHO)

  const run = (args) => spawnSync(process.execPath, [fileURLToPath(new URL('./package-portable.mjs', import.meta.url)), ...args], { encoding: 'utf8' })

  const pass = run(['--check-payload-platform', ok, 'windows'])
  assert.equal(pass.status, 0)
  assert.match(pass.stdout, /是 windows 二进制/)

  const fail = run(['--check-payload-platform', bad, 'windows'])
  assert.equal(fail.status, 1)
  assert.match(fail.stderr, /不能作为 windows 的随包原件/)

  // A missing argument is a usage error (2), not a silent pass.
  assert.equal(run(['--check-payload-platform', ok]).status, 2)
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
