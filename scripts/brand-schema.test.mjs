import assert from 'node:assert/strict'
import test from 'node:test'

import {
  REQUIRED_LOCALES,
  SUPPORTED_SCHEMA_VERSION,
  loadBrand,
  repositoryRoot,
  validateBrand,
} from './brand-schema.mjs'

const root = repositoryRoot(import.meta.url)

function withBrand(mutate) {
  const brand = structuredClone(fixture)
  mutate(brand)
  return brand
}

// The committed configuration doubles as the fixture: a test that passes
// against a hand-written stub but not against the real file is worthless.
const fixture = await loadBrand(root)

test('the committed brand.json passes validation', () => {
  assert.deepEqual(validateBrand(fixture), [])
})

test('schemaVersion is pinned so a structural change cannot land silently', () => {
  assert.equal(fixture.schemaVersion, SUPPORTED_SCHEMA_VERSION)
  const errors = validateBrand(withBrand((brand) => { brand.schemaVersion = 1 }))
  assert.ok(errors.some((error) => error.startsWith('schemaVersion:')))
})

test('rule C rejects a localized identifier', () => {
  const errors = validateBrand(
    withBrand((brand) => {
      brand.identifiers.current.exeName = { 'zh-CN': '布丁盒子.exe', 'en-US': 'PuddingBox.exe' }
    }),
  )
  assert.ok(
    errors.some((error) => error.includes('identifiers.current.exeName') && error.includes('永不本地化')),
    `expected a localization error, got: ${errors.join(' | ')}`,
  )
})

test('rule C rejects a non-ASCII path', () => {
  const errors = validateBrand(
    withBrand((brand) => { brand.identifiers.current.workspaceDir = 'data/工作区/' }),
  )
  assert.ok(errors.some((error) => error.includes('identifiers.current.workspaceDir') && error.includes('ASCII')))
})

test('rule C rejects a path containing a space', () => {
  const errors = validateBrand(
    withBrand((brand) => { brand.identifiers.current.portableDirName = 'Pudding Box' }),
  )
  assert.ok(errors.some((error) => error.includes('portableDirName') && error.includes('ASCII')))
})

test('rule C covers links and visual, not just identifiers', () => {
  const errors = validateBrand(
    withBrand((brand) => {
      brand.links.inApp.terms = { 'zh-CN': '/legal/条款', 'en-US': '/legal/terms' }
      brand.visual.logo.mark = 'assets/布丁标志.svg'
    }),
  )
  assert.ok(errors.some((error) => error.includes('links.inApp.terms')))
  assert.ok(errors.some((error) => error.includes('visual.logo.mark')))
})

test('rule B rejects a localized Windows display value', () => {
  const errors = validateBrand(
    withBrand((brand) => {
      brand.display.windows.fileDescription = { 'zh-CN': '布丁盒子', 'en-US': 'Pudding Box Desktop' }
    }),
  )
  assert.ok(
    errors.some(
      (error) => error.includes('display.windows.fileDescription') && error.includes('不随界面语言切换'),
    ),
  )
})

test('rule B allows a Chinese fixed display value', () => {
  // 布丁盒子 is not ASCII, but it is a human-readable fixed display string
  // rather than an identifier, so rule C must not apply to it.
  assert.deepEqual(validateBrand(fixture), [])
  assert.equal(fixture.display.windows.fileDescription, '布丁盒子')
})

test('rule A requires every locale to be present', () => {
  for (const locale of REQUIRED_LOCALES) {
    const errors = validateBrand(
      withBrand((brand) => { delete brand.product.names[locale] }),
    )
    assert.ok(
      errors.some((error) => error.includes('product.names') && error.includes(locale)),
      `deleting ${locale} should be reported`,
    )
  }
})

test('rule A rejects an empty locale value', () => {
  const errors = validateBrand(withBrand((brand) => { brand.about.teamName['zh-CN'] = '   ' }))
  assert.ok(errors.some((error) => error.includes('about.teamName')))
})

test('rule A rejects a plain string where a locale map belongs', () => {
  const errors = validateBrand(withBrand((brand) => { brand.product.tagline = 'One workspace.' }))
  assert.ok(errors.some((error) => error.includes('product.tagline') && error.includes('语言 map')))
})

test('an unknown top-level section is reported instead of silently ignored', () => {
  const errors = validateBrand(withBrand((brand) => { brand.platform = { windows: {} } }))
  assert.ok(errors.some((error) => error.startsWith('platform:')))
})

test('a generator-injected underscore field is not treated as unknown', () => {
  const errors = validateBrand(withBrand((brand) => { brand._generated = 'note' }))
  assert.deepEqual(errors, [])
})

test('a missing required path fails the build rather than yielding a blank string', () => {
  const errors = validateBrand(withBrand((brand) => { delete brand.identifiers.current.singleInstanceId }))
  assert.ok(errors.some((error) => error.includes('identifiers.current.singleInstanceId')))
})

test('English short name stays short so the tray and notifications do not truncate', () => {
  assert.equal(fixture.product.shortName['en-US'], 'Pudding')
  assert.equal(fixture.product.names['en-US'], 'Pudding Box')
})

test('this phase ships exactly the two proofread locales', () => {
  assert.deepEqual(Object.keys(fixture.product.names).sort(), ['en-US', 'zh-CN'])
})

test('no external link carries an unresolved placeholder', () => {
  for (const [key, url] of Object.entries(fixture.links.external)) {
    assert.ok(!/[<>]/.test(url), `links.external.${key} still contains a placeholder: ${url}`)
  }
})
