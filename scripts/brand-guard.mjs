// scripts/brand-guard.mjs
//
// Guards the brand-string rule (开发规范.md §3.2, 硬规则 2): user-facing product
// copy is interpolated from branding/brand.json — `{brand}` for the full name,
// `{brandShort}` for the short one — and is never typed into source. Before
// this script existed the rule had no mechanical enforcement at all, only four
// files claiming in prose that a "brand-guard" enforced it (see V-79): `make
// brand-check` validated brand.json's schema and the generated copies, which
// says nothing about a literal in a Go string or a .svelte template.
//
// WHAT IS FORBIDDEN
//
// Every string value under brand.json's `product`, `about`, and `copy` keys —
// that is the whole user-facing copy surface, collected recursively so a new
// copy key is covered the day it is added. `identifiers`, `links`, and `visual`
// are deliberately NOT in the set: they are technical conventions (`octo.exe`,
// `~/.octo`, `PuddingBox` as a directory name), not copy, and the guard must
// not confuse "the executable is called PuddingBox.exe" with "the product is
// called Pudding Box". `display.windows.*` duplicates `product.names` /
// `about.*` verbatim, so leaving it out loses no coverage.
//
// ASCII values carry word boundaries: `Pudding` matches "Pudding Box" but not
// "PuddingBox" or "PuddingBox.exe". CJK values need none (`[A-Za-z0-9]` never
// matches a Han character), and a value that is a prefix of another value in
// the set — `布丁盒子` inside `布丁盒子工作室` — is a real hit either way.
//
// SCOPE
//
// Product trees only (internal, cmd, web, mobile, landing, packaging, scripts,
// shared). NOT scanned, on purpose:
//   - `*.md` — documents are allowed to name the product; the rule governs
//     code that ships, not prose that describes it.
//   - `_test.go` / `*.test.*` / `*.spec.*` — a test's job is to pin the value
//     it asserts, so the literal there is the fixture, not a copy defect.
//   - `branding/` — the value's origin.
//   - the six files scripts/sync-branding.mjs generates from brand.json (five
//     JSON copies + packaging/windows/brand.iss). Those legitimately contain
//     the names; `make brand-check`'s sync check owns them.
//   - any line that STARTS with a comment opener (`//`, `#`, `;`, `*`, `<!--`,
//     `/*`). A comment naming the brand is documentation — `internal/brand`'s
//     own doc comment reads `e.g. "布丁盒子" or "Pudding Box"`. Only a line
//     whose leading non-space text is code is scanned, so a trailing
//     `foo := "布丁盒子" // ...` is still caught.
//
// Licences: whole-file exceptions live in scripts/brand-allowlist.txt, one
// written reason per entry (same shape as scripts/homedir-allowlist.txt).
//
// Usage:
//   node scripts/brand-guard.mjs
//
// No npm dependencies. Run it in CI (the `brand-guard` job in
// .github/workflows/go.yml) and locally via `make brand-check`.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { isAllowed, loadAllowlist } from './datapath-guard.mjs'

export const BRAND_SOURCE_REL = 'branding/brand.json'
export const ALLOWLIST_REL = 'scripts/brand-allowlist.txt'

// The three brand.json keys whose string leaves are user-facing copy.
const COPY_KEYS = ['product', 'about', 'copy']

// Trees that ship to a user. Explicit rather than "everything", so a new
// top-level directory is a deliberate decision rather than silent coverage.
const SCAN_ROOTS = ['internal', 'cmd', 'web', 'mobile', 'landing', 'packaging', 'scripts', 'shared']

// Directories never worth walking into. `webdist` is Vite's output directory
// (internal/server/webdist is go:embed'd and only .gitkeep is tracked): every
// file in it is derived from web/, which IS scanned, so scanning the artifact
// would only report the same literal twice — and would report it against a
// path that no edit can fix.
const SKIP_DIRS = new Set(['node_modules', '.git', 'dist', 'webdist', '.svelte-kit', '.next', 'vendor', 'branding'])

// Binary/archive extensions — brand.json's visual assets live in these.
const SKIP_EXT = new Set([
  '.png', '.jpg', '.jpeg', '.gif', '.ico', '.webp', '.icns', '.svg',
  '.zip', '.tar', '.gz', '.exe', '.dll', '.so', '.dylib', '.pkg', '.dmg',
  '.pdf', '.woff', '.woff2', '.ttf', '.otf', '.wasm', '.bin', '.syso', '.sum',
])

// The six generated copies of brand.json. scripts/sync-branding.mjs writes
// them and its --check mode asserts they are current, so a literal here is
// expected — scanning them would only duplicate that check.
export const GENERATED_RELS = [
  'internal/brand/brand.json',
  'web/src/lib/brand.config.json',
  'mobile/src/brand.config.json',
  'landing/brand.config.json',
  'cmd/octo-relay/internal/push/brand.json',
  'packaging/windows/brand.iss',
]

// A leading comment opener. Deliberately anchored: only a line whose first
// non-space characters open a comment is skipped.
export const LEADING_COMMENT = /^\s*(\/\/|#|;|\*|\/\*|<!--)/

// ── leg 2: the upstream product name in the copy tables ──────────────────────
//
// The rebranding design specified this as check #1 (品牌升级方案 §4.5) and it was
// never implemented — the same gap as the literal scan itself (V-79). It is a
// different failure from a typed brand copy: the tables below are where a stale
// product name survives a rebrand, because an entry like
// `"welcome": "Welcome to Octo"` renders the *old* name to a user while
// brand.json and every accessor say the new one. Nothing else catches it —
// brand.json is correct, the generated copies are in sync, and `Octo` is not
// one of the values brand-guard leg 1 forbids.
//
// Scoped to the two user-facing copy tables on purpose. "octo" is the CLI's
// real name (`cliCommand`, `cliExeName`), so a repo-wide scan for it would be
// noise; these two tables are the surfaces where it must never appear as the
// product's name. The design's own formulation was `rg '\bOcto\b'` — matched
// here without the shell's comment blindness, since both files legitimately
// carry an `OCTO-FORK` or history comment naming the old path.
//
// One line may name it on purpose, and that line has to say so. 需求20260906
// §5.1.2 第 6 条 *mandates* the sentence 「若本机正在运行 Octo 或其它程序，请先
// 退出后再打开布丁盒子」 — there the upstream name is the whole point, because
// the situation it describes is the user having the upstream Octo installed, so
// interpolating our brand would tell them to quit the wrong program. That is an
// exception 硬规则 2 grants ("两个例外须显式注释" — .cursor/rules/dev-norms.mdc),
// and this is where the annotation is cashed in: the marker must sit on the
// comment line *immediately above* the copy, so an exemption is visible in the
// diff next to the string it licenses and cannot be sprinkled somewhere else in
// the file. Without the marker the line is a defect like any other.
export const UPSTREAM_NAME = /(?<![A-Za-z])Octo(?![A-Za-z])/

// The annotation. Anchored to a leading comment opener like LEADING_COMMENT, so
// it is a comment marker and not a way to smuggle the word into a string.
export const EXCEPTION_MARKER = /^\s*(\/\/|#|;|\*|\/\*)[\s\S]*brand-exception/

export const COPY_TABLES = ['web/src/lib/i18n.ts', 'cmd/octo-desktop/lang.go']

function isTestFile(name) {
  return name.endsWith('_test.go') ||
    /\.(test|spec)\.(ts|tsx|js|jsx|mjs|cjs)$/.test(name)
}

function isMarkdown(name) {
  return name.endsWith('.md') || name.endsWith('.mdx')
}

// copyStrings returns every string leaf under brand.json's copy keys.
export function copyStrings(brand) {
  const out = []
  const walk = (node) => {
    if (typeof node === 'string') {
      out.push(node)
      return
    }
    if (node && typeof node === 'object' && !Array.isArray(node)) {
      for (const value of Object.values(node)) walk(value)
    }
  }
  for (const key of COPY_KEYS) walk(brand?.[key])
  return out
}

function escapeRegExp(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// brandPattern builds the matcher for one copy value. ASCII-alphanumeric edges
// get word boundaries so `Pudding` does not fire on `PuddingBox`; a CJK value
// needs none, because [A-Za-z0-9] cannot match a Han character.
export function brandPattern(value) {
  const first = value[0] ?? ''
  const last = value[value.length - 1] ?? ''
  const lead = /[A-Za-z0-9]/.test(first) ? '(?<![A-Za-z0-9])' : ''
  const trail = /[A-Za-z0-9]/.test(last) ? '(?![A-Za-z0-9])' : ''
  return new RegExp(lead + escapeRegExp(value) + trail)
}

// buildMatchers pairs each copy value with its pattern, longest first so the
// reported hit names the most specific value in the set.
export function buildMatchers(brand) {
  const seen = new Set()
  const matchers = []
  for (const value of copyStrings(brand)) {
    if (value === '' || seen.has(value)) continue
    seen.add(value)
    matchers.push({ value, pattern: brandPattern(value) })
  }
  matchers.sort((a, b) => b.value.length - a.value.length)
  return matchers
}

// literalHits returns the 1-based line numbers of unlicensed hits, plus the
// copy value each one matched. Comment-led lines are skipped (see SCOPE).
export function literalHits(content, matchers) {
  const hits = []
  const lines = content.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (line.trim() === '' || LEADING_COMMENT.test(line)) continue
    for (const { value, pattern } of matchers) {
      if (pattern.test(line)) {
        hits.push({ line: i + 1, value })
        break
      }
    }
  }
  return hits
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

export async function loadBrand(root) {
  return JSON.parse(await fs.readFile(path.join(root, BRAND_SOURCE_REL), 'utf8'))
}

// collectFiles returns repo-relative paths of every scannable file under the
// product trees. A missing tree is tolerated so the guard also runs in a
// partial checkout.
export async function collectFiles(root) {
  const files = []
  const generated = new Set(GENERATED_RELS)
  for (const dir of SCAN_ROOTS) {
    await walk(path.join(root, dir))
  }
  return files

  async function walk(dir) {
    let entries
    try {
      entries = await fs.readdir(dir, { withFileTypes: true })
    } catch (error) {
      if (error?.code === 'ENOENT') return
      throw error
    }
    for (const entry of entries) {
      const abs = path.join(dir, entry.name)
      if (entry.isDirectory()) {
        if (SKIP_DIRS.has(entry.name)) continue
        await walk(abs)
        continue
      }
      if (!entry.isFile()) continue
      if (isMarkdown(entry.name) || isTestFile(entry.name)) continue
      if (SKIP_EXT.has(path.extname(entry.name).toLowerCase())) continue
      const rel = path.relative(root, abs)
      if (generated.has(rel)) continue
      files.push(rel)
    }
  }
}

// upstreamNameHits returns the 1-based line numbers of copy-table lines naming
// the upstream product. Comment-led lines are skipped, same as leg 1: both
// tables carry a comment recording where the old name used to be.
//
// A code line is exempt when the comment immediately above it carries the
// annotation (见上面 EXCEPTION_MARKER 的说明). "Immediately" is the whole
// mechanism: it keeps the licence next to the string it covers, so a reviewer
// reads both at once and the marker cannot be dropped at the top of the file to
// cover every line below it.
export function upstreamNameHits(content) {
  const hits = []
  const lines = content.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (line.trim() === '' || LEADING_COMMENT.test(line)) continue
    if (!UPSTREAM_NAME.test(line)) continue
    if (i > 0 && EXCEPTION_MARKER.test(lines[i - 1])) continue
    hits.push(i + 1)
  }
  return hits
}

// check returns a list of human-readable problems; empty means clean.
export async function check(root) {
  const brand = await loadBrand(root)
  const matchers = buildMatchers(brand)
  const entries = await loadAllowlist(root, ALLOWLIST_REL)
  const problems = []
  const files = await collectFiles(root)

  for (const rel of files) {
    const buffer = await fs.readFile(path.join(root, rel))
    // A NUL byte means binary; the extension list catches the common cases
    // and this catches the rest (icons committed without an extension).
    if (buffer.includes(0)) continue
    const content = buffer.toString('utf8')
    if (!matchers.some(({ pattern }) => pattern.test(content))) continue
    if (isAllowed(rel, entries)) continue
    for (const { line, value } of literalHits(content, matchers)) {
      problems.push(
        `${rel}:${line}: contains the brand copy ${JSON.stringify(value)} — ` +
          'interpolate {brand}/{brandShort} (internal/brand, web/src/lib/brand.ts) ' +
          `or add a reason to ${ALLOWLIST_REL}`,
      )
    }
  }

  for (const rel of COPY_TABLES) {
    const content = await fs.readFile(path.join(root, rel), 'utf8')
    for (const line of upstreamNameHits(content)) {
      problems.push(
        `${rel}:${line}: the copy table names the upstream product "Octo" — ` +
          'interpolate the brand instead (硬规则 2; 品牌升级方案 §4.5 check 1)',
      )
    }
  }

  return problems
}

async function main() {
  const root = repositoryRoot(import.meta.url)
  const problems = await check(root)
  if (problems.length > 0) {
    console.error('brand-guard failed:')
    for (const problem of problems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }
  console.log(
    `brand-guard passed: no unlicensed brand literals outside ${ALLOWLIST_REL}, ` +
      `and no upstream name in the copy tables (${COPY_TABLES.join(', ')}).`,
  )
}

// Only run the CLI when invoked directly, so importing check for a test is
// free of side effects.
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
