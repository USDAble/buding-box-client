import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import path from 'node:path'
import test from 'node:test'

import { loadBrand, repositoryRoot } from './brand-schema.mjs'
import { buildTargets, renderBrandJSON, renderWindowsInstallerBrand } from './sync-branding.mjs'

const root = repositoryRoot(import.meta.url)
const brand = await loadBrand(root)

test('every generated target on disk matches what the script would write', async () => {
  for (const target of buildTargets(brand)) {
    const actual = await fs.readFile(path.join(root, target.destination))
    assert.ok(
      actual.equals(target.content),
      `${target.destination} is stale — run: node scripts/sync-branding.mjs`,
    )
  }
})

test('the Go embed target and the web import target receive identical bytes', () => {
  const targets = buildTargets(brand)
  const goTarget = targets.find((t) => t.destination === path.join('internal', 'brand', 'brand.json'))
  const webTarget = targets.find(
    (t) => t.destination === path.join('web', 'src', 'lib', 'brand.config.json'),
  )
  assert.ok(goTarget && webTarget)
  assert.ok(goTarget.content.equals(webTarget.content))
})

test('generated JSON is marked so nobody edits a copy by mistake', () => {
  const parsed = JSON.parse(renderBrandJSON(brand))
  assert.match(parsed._generated, /scripts\/sync-branding\.mjs/)
  assert.match(parsed._generated, /Do not edit directly/)
})

test('the provenance marker does not disturb the real values', () => {
  const parsed = JSON.parse(renderBrandJSON(brand))
  delete parsed._generated
  assert.deepEqual(parsed, brand)
})

test('generated JSON ends with a newline so it is a well-formed text file', () => {
  assert.ok(renderBrandJSON(brand).endsWith('}\n'))
})

test('the installer takes the fixed Windows display name, not a localized one', () => {
  const rendered = renderWindowsInstallerBrand(brand)
  assert.match(rendered, /^; Generated from branding\/brand\.json/m)
  assert.match(rendered, /#define BrandAppName "Pudding Box"/)
  assert.match(rendered, /#define BrandAppPublisher "Pudding Box Studio"/)
  assert.match(rendered, /#define BrandAppPublisherURL "https:\/\/[^"]+"/)
})

test('a quote in a brand value cannot break out of the Inno string literal', () => {
  const hostile = structuredClone(brand)
  hostile.display.windows.productName = 'Pudding "Box"'
  assert.match(renderWindowsInstallerBrand(hostile), /#define BrandAppName "Pudding ""Box"""/)
})

test('a newline in a brand value is rejected rather than producing a broken .iss', () => {
  const hostile = structuredClone(brand)
  hostile.about.teamName['en-US'] = 'Pudding Box\nStudio'
  assert.throws(() => renderWindowsInstallerBrand(hostile), /不能包含换行/)
})

test('a missing installer input is reported with its config path', () => {
  const incomplete = structuredClone(brand)
  delete incomplete.links.external.website
  assert.throws(() => renderWindowsInstallerBrand(incomplete), /links\.external\.website/)
})

test('the target list covers every consumer the plan enumerates', () => {
  const destinations = buildTargets(brand).map((target) => target.destination.split(path.sep).join('/'))
  assert.deepEqual(destinations.sort(), [
    'cmd/octo-relay/internal/push/brand.json',
    'internal/brand/brand.json',
    'landing/brand.config.json',
    'mobile/src/brand.config.json',
    'packaging/windows/brand.iss',
    'web/src/lib/brand.config.json',
  ])
})
