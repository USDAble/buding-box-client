// Assembles the portable Windows deliverable — a self-contained <portableDirName>/
// directory (product exe + bundled tools + pre-filled data/ + bilingual usage
// note) and zips it for the CI artifact. This is the product's primary
// deliverable (需求 §5.1.1): the installer still builds but is no longer the
// main output (see the portable packaging design).
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
import { runPreflight } from './preflight.mjs'

const VERSION_PKG = 'github.com/open-octo/octo-agent/internal/version'
const DEFAULT_VERSION = '0.0.0-dev'
// Build tags for the packaged desktop binary. product_production selects the
// immutable production profile; see the buildExe comment and
// the runtime Profile boundary.
export const BUILD_TAGS = 'embedrg product_production'
export const TEST_BUILD_TAGS = 'embedrg product_test'
// The pre-filled data/ files. The portable package ships an empty template so
// the user can see and edit it (P12 §3.1).
//
// `chat-modes.json` is deliberately NOT here: its factory content was the four
// `buding-*` fake models, and the model list now comes from the signed central
// catalog (P0-04). A missing file is already equivalent to the factory grouping
// (chatmode.Load returns Builtin() without writing), so pre-filling it would
// only re-ship deleted fake data — see P12 §3.1 and 开发规范 §3.9.1.
export const DATA_FILES = ['sensitive-words.txt', 'config.yml']
export const WORKSPACE_DIR = 'workspace'

// ── target resolution ─────────────────────────────────────────────────────

export function resolveTarget(env = process.env) {
  const goos = env.GOOS || 'windows'
  const goarch = env.GOARCH || 'amd64'
  const version = (env.VERSION || DEFAULT_VERSION).replace(/^v/, '').trim()
  return { goos, goarch, version }
}

export function resolvePackageProfile(env = process.env) {
  const profile = env.PACKAGE_PROFILE || 'production'
  if (profile === 'production') return { name: profile, buildTags: BUILD_TAGS, suffix: '' }
  if (profile === 'test') return { name: profile, buildTags: TEST_BUILD_TAGS, suffix: '-test' }
  throw new Error(`unknown PACKAGE_PROFILE=${JSON.stringify(profile)}; expected production or test`)
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

// Marker strings that must not exist in a `product_production` binary.
//
// Every marker is canned content from internal/provider/local (the P11 fake
// channel) or the path of its P10 PII log. The package is tagged
// !product_production, so a production build excludes all of them.
//
// Why check the artifact when release-profile-guard already checks the source:
// the two catch different failures. That guard proves the tag is *written* in
// the build command; this proves it *reached the compiler and changed the
// output*. A BUILD_TAGS constant edited to drop product_production, a `go build`
// whose -tags is shadowed by an env GOFLAGS, or a reply.go that loses its build
// tag would all pass the source check and still ship the demo channel.
//
// Keep sorted by prominence: the first is the user-visible reply text, the
// second is the PII log path.
export const FORBIDDEN_MARKERS = [
  '本地演示模型的固定回复',
  'local-provider.jsonl',
]

// checkProductionBinary returns the problems found in a built binary. Pure, so
// a test can drive it with synthetic bytes.
export function checkProductionBinary(buf) {
  const problems = []
  for (const marker of FORBIDDEN_MARKERS) {
    if (buf.includes(Buffer.from(marker, 'utf8'))) {
      problems.push(
        `binary contains ${JSON.stringify(marker)} — the P11 local fake channel was not ` +
          `compiled out; check that BUILD_TAGS still lists product_production and that ` +
          `internal/provider/local/reply.go kept its !product_production tag`,
      )
    }
  }
  return problems
}

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

// Vendored third-party binaries, and the reason the check below can tell whose
// build-machine path a hit is (V-107).
//
// Every one of these is byte-identical to a file on disk at package time, which
// is what makes a hit attributable by exact bytes rather than by filename or by
// "the two home directories differ, so it must be theirs". The coincidence that
// broke this check was `os.homedir()` on the Windows runner being the *same
// string* as a path inside a vendored binary — a property no check can reason
// from, and the reason the verdict must not depend on which machine built the
// package.
//
// The list is **discovered, not written down**. A hand-written list of two went
// stale on the first run: `mruby.wasm` (a wasi-sdk build, embedded by
// `internal/workflow/runtime.go`) carries `/Users/runner/work/wasi-sdk/...` too,
// and 14 of the 143 hits were outside ripgrep's payload. Payloads are per-file
// and the roots are per-convention, so adding a platform or a version needs no
// edit here.
const VENDORED_ROOTS = [
  // Third-party release staged for the target platform before the build.
  ['internal/tools/rgembed/binaries', () => true],
  // Every wasm module the build compiles in verbatim (mruby.wasm today).
  ['internal', (p) => p.endsWith('.wasm')],
  // Tools copied into the package rather than compiled into it (uv.exe).
  ['dist/bundled-tools', () => true],
]

export function bundledUvPath(root, target) {
  return path.join(root, 'dist', 'bundled-tools', `windows-${target.goarch}`, 'uv.exe')
}

// A vendored payload is staged for one platform and then embedded verbatim, so
// a payload for the wrong platform ships a binary the target cannot run — and
// nothing else notices, because the bytes are a perfectly valid file (V-108).
// Reproduced before this check existed: `make desktop-portable` on a host whose
// `binaries/rg` happened to be a Mach-O arm64 build produced a windows/amd64 exe
// with that macOS binary inside it, and the self-check passed.
//
// Checked by magic rather than by hash because the question is "can the target
// run this", which the header answers for every platform and needs nothing
// pinned. Provenance — "is it the release we pinned" — is a different question,
// answered where the release is fetched.
const PAYLOAD_MAGICS = {
  windows: ['4d5a'], // MZ
  linux: ['7f454c46'], // ELF
  // Mach-O, either width or endianness, plus the fat/universal wrapper.
  darwin: ['cffaedfe', 'cefaedfe', 'feedfacf', 'feedface', 'cafebabe', 'cafebabf'],
}

export function payloadPlatformMismatch(buf, goos) {
  const wanted = PAYLOAD_MAGICS[goos]
  if (!wanted) return null // nothing to assert for a platform we have no rule for
  if (buf.length < 4) return `too short to be a ${goos} binary`
  const head = buf.subarray(0, 4).toString('hex')
  return wanted.some((m) => head.startsWith(m)) ? null : `its header is ${head}, which is not a ${goos} binary`
}

export async function checkVendoredPayloadPlatforms(paths, goos, { root = null } = {}) {
  const failures = []
  for (const p of paths) {
    // WebAssembly is executed by a runtime we ship, not by the OS, so it is
    // platform-neutral by construction and has no header to match.
    if (p.endsWith('.wasm')) continue
    let buf
    try {
      buf = await fs.readFile(p)
    } catch {
      continue // absence is not this check's business
    }
    const why = payloadPlatformMismatch(buf, goos)
    if (why) {
      const label = root ? path.relative(root, p) : p
      failures.push(`vendored payload ${label} cannot run on ${goos}: ${why}`)
    }
  }
  return failures
}

export async function vendoredBinarySources(root) {
  const out = []
  for (const [rel, keep] of VENDORED_ROOTS) {
    const dir = path.join(root, ...rel.split('/'))
    // OCTO-FORK: Node 20.0 accepts recursive readdir but neither recurses nor
    // exposes Dirent.path; walk explicitly so the release guard is portable.
    const pending = [dir]
    while (pending.length > 0) {
      const current = pending.pop()
      let entries
      try {
        entries = await fs.readdir(current, { withFileTypes: true })
      } catch {
        continue // not staged in this checkout — nothing to attribute
      }
      for (const e of entries) {
        const abs = path.join(current, e.name)
        if (e.isDirectory()) {
          pending.push(abs)
          continue
        }
        if (!e.isFile()) continue
        // Repo metadata that lives beside a payload but is never embedded: keep
        // the logged set honest so a human can see what was attributed.
        if (e.name.startsWith('.') || e.name.endsWith('.md')) continue
        if (keep(abs)) out.push(abs)
      }
    }
  }
  return out.sort()
}

export async function checkNoAbsolutePaths(files, forbiddenPaths = [], { vendoredPaths = [] } = {}) {
  const needles = forbiddenPaths.filter((p) => typeof p === 'string' && p !== '').map((p) => Buffer.from(p, 'utf8'))
  if (needles.length === 0) return []

  // Read the originals once; a payload that is not staged cannot be in the
  // artefact either, so its absence leaves every hit attributed to us. That is
  // the fail-closed direction.
  const vendors = []
  for (const p of vendoredPaths) {
    try {
      const buf = await fs.readFile(p)
      if (buf.length > 0) vendors.push(buf)
    } catch {
      // not staged — e.g. a build without the embedrg tag
    }
  }

  const failures = []
  for (const f of files) {
    if (f.dir) continue
    const data = await fs.readFile(f.abs)
    const spans = []
    for (const v of vendors) {
      for (let i = data.indexOf(v); i !== -1; i = data.indexOf(v, i + 1)) spans.push([i, i + v.length])
    }
    for (const needle of needles) {
      for (let i = data.indexOf(needle); i !== -1; i = data.indexOf(needle, i + 1)) {
        // One report per needle per file, as before; but every hit is examined,
        // so a needle that appears both inside and outside a payload still fails.
        if (spans.some(([a, b]) => a <= i && i < b)) continue
        failures.push(`${f.rel} contains build-machine path ${needle.toString('utf8')}`)
        break
      }
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
export async function selfCheck(dir, { brand, target = null, root = null, forbiddenPaths = [], vendoredPaths = [], baselinePath = null } = {}) {
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
  // Say what was attributed, so a reader can see the verdict's basis without
  // re-deriving it (V-107).
  if (vendoredPaths.length > 0) {
    console.log(`  按字节归属 ${vendoredPaths.length} 个随包原件：${vendoredPaths.map((p) => path.basename(p)).join(", ")}`)
  }
  failures.push(...(await checkNoAbsolutePaths(files, forbiddenPaths, { vendoredPaths })))

  // Every payload that goes into the artefact has to be a binary the target can
  // actually run (V-108).
  if (target) {
    failures.push(...(await checkVendoredPayloadPlatforms(vendoredPaths, target.goos, { root })))
  }

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

async function buildExe({ root, brand, target, dest, buildTags }) {
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
  // A package selects either product_production or product_test. Neither is a
  // developer build: both profiles reject developer webviews and ambient model
  // sources. Production remains the default and is guarded separately.
  execFileSync('go', ['build', '-trimpath', '-tags', buildTags, '-ldflags', ldflags, '-o', out, '.'], { // release-profile-guard:allow — resolvePackageProfile defaults to production and explicitly selects either sealed package profile
    cwd: modDir,
    stdio: 'inherit',
    env,
  })

  // Artifact-level counterpart of release-profile-guard: that script proves the
  // tag is *written down*, this proves it *took effect*. See
  // checkProductionBinary.
  const problems = checkProductionBinary(await fs.readFile(out))
  if (problems.length > 0) {
    throw new Error(`refusing to package ${exeName}:\n- ${problems.join('\n- ')}`)
  }
  return out
}

async function assemble({ root, brand, target, dest }) {
  // bin/uv.exe — optional, seeded into data/bin on first launch (P1). Fetched
  // by `make bundle-tools-windows` locally / PowerShell in CI (release.yml).
  const uvSrc = bundledUvPath(root, target)
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

  // `make rg-embed` needs the platform rule before the build rather than in the
  // self-check, because the failure it prevents is a *stale* payload: the target
  // is up to date by mtime while holding another platform's binary. It asks this
  // file instead of repeating the magic numbers, so there is one definition of
  // "this payload can run there" (V-108).
  const probe = process.argv.indexOf('--check-payload-platform')
  if (probe !== -1) {
    const [payload, goos] = process.argv.slice(probe + 1)
    if (!payload || !goos) {
      console.error('用法: package-portable.mjs --check-payload-platform <文件> <goos>')
      process.exitCode = 2
      return
    }
    const buf = await fs.readFile(payload)
    const why = payloadPlatformMismatch(buf, goos)
    if (why) {
      console.error(`${payload} 不能作为 ${goos} 的随包原件：${why}`)
      process.exitCode = 1
      return
    }
    console.log(`ok: ${payload} 是 ${goos} 二进制`)
    return
  }

  const brand = await loadBrand(root)
  const target = resolveTarget()
  let packageProfile
  try {
    packageProfile = resolvePackageProfile()
  } catch (error) {
    console.error(error.message)
    process.exitCode = 2
    return
  }
  if (target.goos !== 'windows') {
    console.error(`便携交付只支持 windows，收到 GOOS=${target.goos}`)
    process.exitCode = 2
    return
  }

  // Refuse to build a shipped artifact from a tree the fork guards reject —
  // CI runs them on the commit, but packaging can start from a dirty or stale
  // checkout and would otherwise silently produce a developer package.
  const preflight = await runPreflight(root, { profile: packageProfile.name })
  if (preflight.failureCount > 0) {
    process.exitCode = 1
    return
  }

  if (packageProfile.name === 'test') {
    execFileSync('make', ['test-profile-check'], {
      cwd: root,
      stdio: 'inherit',
    })
  }

  const dirName = `${brand.identifiers.current.portableDirName}${packageProfile.suffix}`
  const exeName = brand.identifiers.current.exeName
  const dest = path.join(root, 'dist', dirName)

  console.log(`==> 构建 ${exeName} (${target.goos}/${target.goarch}, ${target.version})`)
  await fs.rm(dest, { recursive: true, force: true })
  await fs.mkdir(dest, { recursive: true })
  await buildExe({ root, brand, target, dest, buildTags: packageProfile.buildTags })
  await assemble({ root, brand, target, dest })

  console.log(`==> 产物自检 ${dirName}/`)
  const { failures, warnings } = await selfCheck(dest, {
    brand,
    // V-107: the needle list is what upstream had — `root` plus the build
    // machine's home — and it is restored because the false alarm is fixed
    // rather than tolerated. On GitHub's windows-latest runner `os.homedir()`
    // IS `C:\Users\runneradmin`, which is also the path baked into the vendored
    // third-party binaries (ripgrep's official Windows release, astral's uv.exe)
    // that `embedrg` compiles into the exe, so substrings alone could not tell
    // our leak from theirs.
    //
    // `vendoredPaths` is what tells them apart: a hit inside one of those
    // payloads is attributable to the vendor by exact bytes. So the check stays
    // needle-based and fails on any hit *outside* a payload — including a hit
    // that is inside one place and outside another.
    //
    // The third fact this rests on is `-trimpath`, which is what keeps OUR
    // build paths out of the artefact at all; if that ever regresses, the hits
    // land outside the payloads and this check reports them.
    forbiddenPaths: [root, os.homedir()].filter(Boolean),
    vendoredPaths: await vendoredBinarySources(root),
    target,
    root,
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
