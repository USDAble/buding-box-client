import assert from 'node:assert/strict'
import test from 'node:test'

import {
  ALLOWLIST_REL,
  COPY_TABLES,
  GENERATED_RELS,
  LEADING_COMMENT,
  brandPattern,
  buildMatchers,
  check,
  collectFiles,
  copyStrings,
  literalHits,
  loadBrand,
  repositoryRoot,
  upstreamNameHits,
} from './brand-guard.mjs'

const root = repositoryRoot(import.meta.url)

test('the committed tree passes the guard', async () => {
  assert.deepEqual(await check(root), [])
})

test('the forbidden set is the copy surface, not the technical one', async () => {
  const brand = await loadBrand(root)
  const values = copyStrings(brand)

  // Interpolated copy — the strings the rule governs.
  assert.ok(values.includes('布丁盒子'))
  assert.ok(values.includes('Pudding Box'))
  assert.ok(values.includes('Pudding'))

  // Identifiers are conventions, not copy: `octo.exe`, `~/.octo`, and
  // PuddingBox-as-a-directory-name must never be treated as brand literals, or
  // the guard would forbid the product's own executable name.
  assert.equal(values.includes('PuddingBox'), false)
  assert.equal(values.includes('PuddingBox.exe'), false)
  assert.equal(values.includes('octo'), false)
})

test('an ASCII name matches at word boundaries only', async () => {
  const brand = await loadBrand(root)
  const pattern = brandPattern(brand.product.shortName['en-US'])

  // The whole reason the boundary exists: `{brandShort}` is "Pudding", and
  // without the guard it would fire on the executable and directory names that
  // legitimately contain it.
  assert.equal(pattern.test('Pudding Box'), true)
  assert.equal(pattern.test('Pudding'), true)
  assert.equal(pattern.test('PuddingBox'), false)
  assert.equal(pattern.test('PuddingBox.exe'), false)
  assert.equal(pattern.test('PuddingBox-windows-amd64.exe'), false)
})

test('a CJK name matches without a boundary', () => {
  // No boundary is possible or needed: [A-Za-z0-9] cannot match a Han
  // character, so the prefix rule would only create false negatives. A value
  // that extends another value is itself in the set, so both are hits.
  const pattern = brandPattern('布丁盒子')
  assert.equal(pattern.test('布丁盒子'), true)
  assert.equal(pattern.test('欢迎使用布丁盒子'), true)
  assert.equal(pattern.test('布丁盒子工作室'), true)
  assert.equal(pattern.test('布丁'), false)
})

test('a comment-led line is skipped, a trailing comment is not', () => {
  const matchers = buildMatchers({ product: { names: { 'zh-CN': '布丁盒子' } } })

  // Documentation naming the brand is legitimate — internal/brand's own doc
  // comment reads `e.g. "布丁盒子"`. Every opener in LEADING_COMMENT is covered.
  for (const opener of ['//', '#', ';', '*', '/*', '<!--']) {
    assert.deepEqual(literalHits(`${opener} the product is 布丁盒子`, matchers), [])
  }
  assert.equal(LEADING_COMMENT.test('   // indented comment'), true)

  // But code is code however it is commented afterwards.
  assert.deepEqual(literalHits('title := "布丁盒子" // fallback', matchers), [
    { line: 1, value: '布丁盒子' },
  ])
})

test('a hit is reported with its line number and the most specific value', () => {
  const matchers = buildMatchers({
    product: { names: { 'en-US': 'Pudding Box', 'zh-CN': '布丁盒子工作室' }, shortName: { 'en-US': 'Pudding' } },
  })
  const src = ['package p', '', '\ttitle := "Pudding Box"'].join('\n')
  // Longest-first, so the message names "Pudding Box" rather than "Pudding".
  assert.deepEqual(literalHits(src, matchers), [{ line: 3, value: 'Pudding Box' }])
})

test('an empty or unset brand value never matches', () => {
  // `copy` values can be blank; a blank pattern would match every line.
  const matchers = buildMatchers({ copy: { termsBody: { 'zh-CN': '' } } })
  assert.deepEqual(matchers, [])
  assert.deepEqual(literalHits('anything at all', matchers), [])
})

test('the scanned trees exclude tests, prose, and generated copies', async () => {
  const files = await collectFiles(root)

  // Prose describes the product; tests pin the value they assert.
  assert.ok(!files.some((f) => f.endsWith('.md')))
  assert.ok(!files.some((f) => f.endsWith('_test.go')))
  assert.ok(!files.some((f) => /\.(test|spec)\.(ts|tsx|mjs)$/.test(f)))

  // The origin and its generated copies are someone else's check.
  assert.ok(!files.some((f) => f.startsWith('branding/')))
  for (const rel of GENERATED_RELS) assert.ok(!files.includes(rel), `${rel} should not be scanned`)

  // Vite's output directory is derived from web/, which is scanned instead.
  assert.ok(!files.some((f) => f.startsWith('internal/server/webdist/')))

  // The trees that do ship are covered, including the loose shell metadata
  // files that carry no extension.
  assert.ok(files.includes('web/index.html'))
  assert.ok(files.includes('mobile/capacitor.config.ts'))
  assert.ok(files.includes('cmd/octo-desktop/build/linux/AppRun'))
})

test('a copy-table line naming the upstream product is a hit, a comment is not', () => {
  // The rebranding design's check 1 (品牌升级方案 §4.5): a stale product name
  // renders the OLD name to a user while brand.json says the new one.
  assert.deepEqual(upstreamNameHits('  "welcome": "Welcome to Octo",'), [1])
  assert.deepEqual(upstreamNameHits('  takeTitle: "Octo is not responding",'), [1])

  // Both tables legitimately record where the old name used to be.
  assert.deepEqual(upstreamNameHits('  // named the default workspace directory (~/Octo, before P1)'), [])
  assert.deepEqual(upstreamNameHits('\t// OCTO-FORK: 内置默认工作区从 `~/Octo` 改为数据根'), [])

  // "octo" is the CLI's real name, so only the product-name spelling counts.
  assert.deepEqual(upstreamNameHits('\tcliCommand: "octo",'), [])
  assert.deepEqual(upstreamNameHits('  运行 octo serve 启动服务'), [])

  // The tables really are scanned: this is the third leg of `make brand-check`,
  // and it is only useful if it runs.
  assert.deepEqual(COPY_TABLES, ['web/src/lib/i18n.ts', 'cmd/octo-desktop/lang.go'])
})

test('every allowlist entry carries a written reason', async () => {
  const { readFile } = await import('node:fs/promises')
  const { join } = await import('node:path')
  const raw = await readFile(join(root, ALLOWLIST_REL), 'utf8')

  const entries = raw
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '' && !line.startsWith('#'))

  assert.ok(entries.length > 0)
  for (const line of entries) {
    const [, ...reason] = line.split(/\s+/)
    // A path with no reason is an unlicensed exemption — the reason is the
    // whole difference between "we decided" and "we forgot".
    assert.ok(reason.length > 0, `no reason on: ${line}`)
    assert.ok(reason.join(' ').length > 20, `reason too thin on: ${line}`)
  }
})
