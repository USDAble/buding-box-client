// Assembles the portable Windows deliverable — a self-contained <portableDirName>/
// directory (product exe + bundled tools + pre-filled data/ + bilingual usage
// note) and zips it for the CI artifact. This is the product's primary
// deliverable (需求 §5.1.1): the installer still builds but is no longer the
// main output (see dev-docs-usdable/需求/2260906/技术方案/P12-便携打包.md).
//
// Usage:
//   node scripts/package-portable.mjs
//
// Inputs (env): GOOS/GOARCH (default windows/amd64), VERSION, COMMIT.
// Outputs:
//   dist/<portableDirName>/                         the copy-to-USB directory
//   dist/<portableDirName>-<goos>-<goarch>.zip      the CI artifact
// Exit code: 0 on success; non-zero with the failed self-check items printed.
//
// Zero npm dependencies, matching the other scripts/ tools (sync-branding,
// brand-schema, datapath-guard). The PE self-check reads the exe through
// pe-info.mjs (hand-rolled, no dependency). WebView2Loader.dll is NOT bundled:
// Wails v3 embeds the WebView2 loader at build time (jchv/go-winloader), so the
// exe is self-contained — the existing CI already ships just the bare exe.
//
// The exe is built with -trimpath so no build-machine source path is baked into
// the binary; the "no absolute paths" self-check (P12 §3.3) depends on it.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import os from 'node:os'

import { repositoryRoot, loadBrand } from './brand-schema.mjs'
import { inspectFile } from './pe-info.mjs'

const VERSION_PKG = 'github.com/open-octo/octo-agent/internal/version'
const DEFAULT_VERSION = '0.0.0-dev'
// The pre-filled data/ files P12 §3.1 mandates. chat-modes.json (P9) and
// config.yml (P11 buding endpoint) are placeholders until those PRs land —
// see packaging/portable/README.txt.
export const DATA_FILES = ['sensitive-words.txt', 'chat-modes.json', 'config.yml']
export const WORKSPACE_DIR = 'workspace'

// ── target resolution ─────────────────────────────────────────────────────

export function resolveTarget(env = process.env) {
  const goos = env.GOOS || 'windows'
  const goarch = env.GOARCH || 'amd64'
  const version = (env.VERSION || DEFAULT_VERSION).replace(/^v/, '').trim()
  return { goos, goarch, version }
}

function gitShortHead(root) {
  try {
    return execFileSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: root, stdio: 'pipe' })
      .toString()
      .trim()
  } catch {
    return 'unknown'
  }
}

// ── 使用说明.txt rendering ─────────────────────────────────────────────────
// The template carries {nameZh} / {nameEn} / {exeName} placeholders rather than
// hardcoded brand strings (fork rule 2: never hardcode a brand string).

export function renderUsageNote(template, brand) {
  const names = brand.product?.names ?? {}
  const exeName = brand.identifiers?.current?.exeName ?? ''
  return template
    .replaceAll('{nameZh}', names['zh-CN'] ?? '')
    .replaceAll('{nameEn}', names['en-US'] ?? '')
    .replaceAll('{exeName}', exeName)
}

// ── filesystem walk (pure enough to unit-test) ────────────────────────────

export async function listFiles(dir, rel = '', acc = []) {
  const entries = await fs.readdir(dir, { withFileTypes: true })
  for (const e of entries) {
    const relPath = rel ? `${rel}/${e.name}` : e.name
    const abs = path.join(dir, e.name)
    if (e.isDirectory()) {
      acc.push({ rel: relPath, abs, dir: true })
      await listFiles(abs, relPath, acc)
    } else {
      acc.push({ rel: relPath, abs, dir: false })
    }
  }
  return acc
}

// ── self-check (P12 §3.3) ─────────────────────────────────────────────────

export function checkDevResiduals(files) {
  const failures = []
  for (const f of files) {
    const name = path.basename(f.rel)
    if (f.dir && (name === 'node_modules' || name === '.git')) {
      failures.push(`dev residual dir: ${f.rel}`)
    }
    if (!f.dir && (name.endsWith('.map') || name.endsWith('.pdb'))) {
      failures.push(`dev residual file: ${f.rel}`)
    }
  }
  return failures
}

export function checkDataFiles(files) {
  const failures = []
  if (!files.some((f) => f.rel === 'data' && f.dir)) {
    failures.push('missing data/ directory')
    return failures
  }
  for (const name of DATA_FILES) {
    if (!files.some((f) => f.rel === `data/${name}` && !f.dir)) {
      failures.push(`data/ missing ${name}`)
    }
  }
  if (!files.some((f) => f.rel === `data/${WORKSPACE_DIR}` && f.dir)) {
    failures.push(`data/ missing ${WORKSPACE_DIR}/ directory`)
  }
  return failures
}

export async function checkNoAbsolutePaths(files, forbiddenPaths = []) {
  const needles = forbiddenPaths.filter((p) => typeof p === 'string' && p !== '').map((p) => Buffer.from(p, 'utf8'))
  const failures = []
  for (const f of files) {
    if (f.dir) continue
    const data = await fs.readFile(f.abs)
    for (const needle of needles) {
      if (data.includes(needle)) failures.push(`${f.rel} contains build-machine path ${needle.toString('utf8')}`)
    }
  }
  return failures
}

export async function totalBytes(files) {
  let total = 0
  for (const f of files) {
    if (!f.dir) total += (await fs.stat(f.abs)).size
  }
  return total
}

// checkSize records the package size baseline and warns (does not fail) when a
// build exceeds it by 20% — P12 §3.3 sizes this as "先记基线，超 20% 告警".
export async function checkSize(baselinePath, files) {
  const total = await totalBytes(files)
  let warning = null
  try {
    const prev = parseInt(await fs.readFile(baselinePath, 'utf8'), 10)
    if (prev > 0 && total > prev * 1.2) {
      warning = `package size ${total} bytes exceeds 120% of baseline ${prev} bytes`
    }
  } catch {
    // no baseline yet — first build just records it
  }
  await fs.mkdir(path.dirname(baselinePath), { recursive: true })
  await fs.writeFile(baselinePath, String(total))
  return { total, warning }
}

// selfCheck returns { failures, warnings, files }. A non-empty failures means
// the package must not ship. warnings are advisory (e.g. the size budget).
export async function selfCheck(dir, { brand, forbiddenPaths = [], baselinePath = null } = {}) {
  const files = await listFiles(dir)
  const failures = []
  const warnings = []

  const exeName = brand.identifiers.current.exeName

  // exe name: exactly one top-level exe, and it is the brand exe name.
  for (const f of files) {
    if (!f.dir && !f.rel.includes('/') && f.rel.toLowerCase().endsWith('.exe') && f.rel !== exeName) {
      failures.push(`unexpected top-level exe: ${f.rel}`)
    }
  }
  if (!files.some((f) => f.rel === exeName && !f.dir)) {
    failures.push(`missing ${exeName} at package root`)
  }

  // exe metadata: GUI subsystem + VERSIONINFO product name / description.
  try {
    const info = await inspectFile(path.join(dir, exeName))
    if (info.subsystem !== 2) {
      failures.push(`${exeName} subsystem=${info.subsystem}, want 2 (GUI)`)
    }
    const wantProduct = brand.display.windows.productName
    const wantDesc = brand.display.windows.fileDescription
    if (info.versionStrings.ProductName !== wantProduct) {
      failures.push(`VERSIONINFO ProductName=${JSON.stringify(info.versionStrings.ProductName)}, want ${JSON.stringify(wantProduct)}`)
    }
    if (info.versionStrings.FileDescription !== wantDesc) {
      failures.push(`VERSIONINFO FileDescription=${JSON.stringify(info.versionStrings.FileDescription)}, want ${JSON.stringify(wantDesc)}`)
    }
  } catch (err) {
    failures.push(`${exeName} PE inspect failed: ${err.message}`)
  }

  failures.push(...checkDevResiduals(files))
  failures.push(...checkDataFiles(files))
  failures.push(...(await checkNoAbsolutePaths(files, forbiddenPaths)))

  if (baselinePath) {
    const { warning } = await checkSize(baselinePath, files)
    if (warning) warnings.push(warning)
  }

  return { failures, warnings, files }
}

// ── ZIP (store, no compression) ───────────────────────────────────────────
// Zero-dependency so the packaging script runs anywhere Node does. STORE is
// fast and the payload (already-compressed exe + embedded web UI) gains little
// from deflate; the zip exists only for the CI artifact.

const CRC32_TABLE = (() => {
  const t = new Uint32Array(256)
  for (let n = 0; n < 256; n++) {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    t[n] = c >>> 0
  }
  return t
})()

export function crc32(buf) {
  let c = 0xffffffff
  for (let i = 0; i < buf.length; i++) c = CRC32_TABLE[(c ^ buf[i]) & 0xff] ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}

function dosDateTime(date = new Date()) {
  const time = (date.getHours() << 11) | (date.getMinutes() << 5) | (date.getSeconds() >> 1)
  const day = ((date.getFullYear() - 1980) << 9) | ((date.getMonth() + 1) << 5) | date.getDate()
  return { date: day, time }
}

// buildZipEntries names every file "<baseName>/<relpath>" so extracting yields
// the whole <portableDirName>/ folder, matching "拷到 U 盘的就是这一层".
export async function buildZipEntries(rootDir, baseName) {
  const files = await listFiles(rootDir)
  const entries = []
  for (const f of files) {
    if (f.dir) continue
    entries.push({ name: `${baseName}/${f.rel}`, data: await fs.readFile(f.abs) })
  }
  return entries
}

export function buildZipBuffer(entries) {
  const { date: dosDate, time: dosTime } = dosDateTime()
  const chunks = []
  const central = []
  let offset = 0
  for (const e of entries) {
    const nameBuf = Buffer.from(e.name, 'utf8')
    const crc = crc32(e.data)
    const size = e.data.length

    const lh = Buffer.alloc(30)
    lh.writeUInt32LE(0x04034b50, 0)
    lh.writeUInt16LE(20, 4) // version needed
    lh.writeUInt16LE(0x0800, 6) // general purpose: UTF-8 names
    lh.writeUInt16LE(0, 8) // method: store
    lh.writeUInt16LE(dosTime, 10)
    lh.writeUInt16LE(dosDate, 12)
    lh.writeUInt32LE(crc, 14)
    lh.writeUInt32LE(size, 18) // compressed size
    lh.writeUInt32LE(size, 22) // uncompressed size
    lh.writeUInt16LE(nameBuf.length, 26)
    lh.writeUInt16LE(0, 28) // extra field length
    chunks.push(lh, nameBuf, e.data)

    const cd = Buffer.alloc(46)
    cd.writeUInt32LE(0x02014b50, 0)
    cd.writeUInt16LE(20, 4) // version made by
    cd.writeUInt16LE(20, 6) // version needed
    cd.writeUInt16LE(0x0800, 8) // UTF-8 names
    cd.writeUInt16LE(0, 10) // method: store
    cd.writeUInt16LE(dosTime, 12)
    cd.writeUInt16LE(dosDate, 14)
    cd.writeUInt32LE(crc, 16)
    cd.writeUInt32LE(size, 20) // compressed size
    cd.writeUInt32LE(size, 24) // uncompressed size
    cd.writeUInt16LE(nameBuf.length, 28)
    cd.writeUInt16LE(0, 30) // extra field length
    cd.writeUInt16LE(0, 32) // file comment length
    cd.writeUInt16LE(0, 34) // disk number start
    cd.writeUInt16LE(0, 36) // internal attrs
    cd.writeUInt32LE(0, 38) // external attrs
    cd.writeUInt32LE(offset, 42) // local header offset
    central.push(Buffer.concat([cd, nameBuf]))

    offset += 30 + nameBuf.length + size
  }
  const centralBuf = Buffer.concat(central)
  const eocd = Buffer.alloc(22)
  eocd.writeUInt32LE(0x06054b50, 0)
  eocd.writeUInt16LE(0, 4)
  eocd.writeUInt16LE(0, 6)
  eocd.writeUInt16LE(entries.length, 8)
  eocd.writeUInt16LE(entries.length, 10)
  eocd.writeUInt32LE(centralBuf.length, 12)
  eocd.writeUInt32LE(offset, 16)
  eocd.writeUInt16LE(0, 20)
  return Buffer.concat([...chunks, centralBuf, eocd])
}

export async function writeZip(entries, outPath) {
  await fs.writeFile(outPath, buildZipBuffer(entries))
}

// ── build / assemble ──────────────────────────────────────────────────────

async function exists(p) {
  try {
    await fs.access(p)
    return true
  } catch {
    return false
  }
}

async function copyDir(src, dest) {
  await fs.mkdir(dest, { recursive: true })
  const entries = await fs.readdir(src, { withFileTypes: true })
  for (const e of entries) {
    const s = path.join(src, e.name)
    const d = path.join(dest, e.name)
    if (e.isDirectory()) await copyDir(s, d)
    else await fs.copyFile(s, d)
  }
}

function buildExe({ root, brand, target, dest }) {
  const modDir = path.join(root, 'cmd', 'octo-desktop')
  const exeName = brand.identifiers.current.exeName
  const out = path.join(dest, exeName)
  // generate-syso is a host-side codegen tool: it writes the rsrc_windows_*.syso
  // resource object that the later go build embeds. It must run with the host's
  // GOOS/GOARCH (go run executes the freshly built binary), so strip the target
  // override from its env. Only the go build below targets windows.
  const hostEnv = { ...process.env }
  delete hostEnv.GOOS
  delete hostEnv.GOARCH
  const env = { ...hostEnv, GOOS: target.goos, GOARCH: target.goarch, CGO_ENABLED: '0' }

  execFileSync(
    'go',
    ['run', './build/windows/generate-syso', target.goarch, 'build/windows/icon.ico', 'build/windows/wails.exe.manifest', target.version],
    { cwd: modDir, stdio: 'inherit', env: hostEnv },
  )

  const commit = process.env.COMMIT || gitShortHead(root)
  const ldflags = `-H windowsgui -X ${VERSION_PKG}.Version=${target.version} -X ${VERSION_PKG}.Commit=${commit}`
  execFileSync('go', ['build', '-trimpath', '-tags', 'embedrg', '-ldflags', ldflags, '-o', out, '.'], {
    cwd: modDir,
    stdio: 'inherit',
    env,
  })
  return out
}

async function assemble({ root, brand, target, dest }) {
  // bin/uv.exe — optional, seeded into data/bin on first launch (P1). Fetched
  // by `make bundle-tools-windows` locally / PowerShell in CI (release.yml).
  const uvSrc = path.join(root, 'dist', 'bundled-tools', `windows-${target.goarch}`, 'uv.exe')
  if (await exists(uvSrc)) {
    await fs.mkdir(path.join(dest, 'bin'), { recursive: true })
    await fs.copyFile(uvSrc, path.join(dest, 'bin', 'uv.exe'))
  } else {
    console.warn(`  (跳过 bin/uv.exe：未找到 ${uvSrc}；可运行 make bundle-tools-windows 补齐)`)
  }

  // Pre-filled data/ from the template.
  await copyDir(path.join(root, 'packaging', 'portable', 'data'), path.join(dest, 'data'))

  // Rendered bilingual usage note.
  const tpl = path.join(root, 'packaging', 'portable', '使用说明.txt')
  const rendered = renderUsageNote(await fs.readFile(tpl, 'utf8'), brand)
  await fs.writeFile(path.join(dest, '使用说明.txt'), rendered)

  return dest
}

// ── main ──────────────────────────────────────────────────────────────────

async function main() {
  const root = repositoryRoot(import.meta.url)
  const brand = await loadBrand(root)
  const target = resolveTarget()
  if (target.goos !== 'windows') {
    console.error(`便携交付只支持 windows，收到 GOOS=${target.goos}`)
    process.exitCode = 2
    return
  }

  const dirName = brand.identifiers.current.portableDirName
  const exeName = brand.identifiers.current.exeName
  const dest = path.join(root, 'dist', dirName)

  console.log(`==> 构建 ${exeName} (${target.goos}/${target.goarch}, ${target.version})`)
  await fs.rm(dest, { recursive: true, force: true })
  await fs.mkdir(dest, { recursive: true })
  buildExe({ root, brand, target, dest })
  await assemble({ root, brand, target, dest })

  console.log(`==> 产物自检 ${dirName}/`)
  const { failures, warnings } = await selfCheck(dest, {
    brand,
    forbiddenPaths: [root, os.homedir()].filter(Boolean),
    baselinePath: path.join(root, 'dist', '.portable-size-baseline'),
  })
  for (const w of warnings) console.warn(`  警告: ${w}`)
  if (failures.length > 0) {
    console.error('产物自检失败：')
    for (const f of failures) console.error(`  - ${f}`)
    process.exitCode = 1
    return
  }
  console.log('  自检通过。')

  const zipName = `${dirName}-${target.goos}-${target.goarch}.zip`
  const zipPath = path.join(root, 'dist', zipName)
  console.log(`==> 打包 ${zipName}`)
  const entries = await buildZipEntries(dest, dirName)
  await writeZip(entries, zipPath)
  console.log(`==> 完成: dist/${dirName}/ + dist/${zipName}`)
}

// Only run the CLI when invoked directly, so importing the module for tests is
// free of side effects (same pattern as sync-branding.mjs / brand-schema.mjs).
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
