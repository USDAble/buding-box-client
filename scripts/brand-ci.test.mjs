import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

async function workflow(name) {
  return fs.readFile(path.join(root, '.github', 'workflows', name), 'utf8')
}

test('Go workflow has a standalone brand synchronization gate', async () => {
  const content = await workflow('go.yml')
  assert.match(content, /^  brand-sync:/m)
  assert.match(content, /node scripts\/sync-branding\.mjs --check/)
})

test('brand source and synchronizer changes trigger affected platform workflows', async () => {
  for (const name of ['desktop.yml', 'mobile.yml', 'relay.yml', 'windows-installer-check.yml', 'macos-installer-check.yml', 'pages.yml']) {
    const content = await workflow(name)
    assert.match(content, /'branding\/\*\*'/, name + ' must watch branding assets')
    assert.match(content, /'scripts\/sync-branding\.mjs'/, name + ' must watch synchronizer changes')
  }
})

test('landing page binds its visible name and tagline to synced brand configuration', async () => {
  const content = await fs.readFile(path.join(root, 'landing', 'index.html'), 'utf8')
  assert.match(content, /data-brand-name/)
  assert.match(content, /data-brand-tagline/)
  assert.match(content, /data-brand-logo/)
  assert.match(content, /el\.dataset\.en\s*=\s*landingLocalized\(landingBrand\.product\?\.tagline, 'en'\)/)
  assert.match(content, /el\.dataset\.zh\s*=\s*landingLocalized\(landingBrand\.product\?\.tagline, 'zh'\)/)
})

test('Windows installer consumes generated brand macros instead of hard-coded product metadata', async () => {
  const installer = await fs.readFile(path.join(root, 'packaging', 'windows', 'octo.iss'), 'utf8')
  assert.match(installer, /#include "brand\.iss"/)
  assert.match(installer, /AppName=\{#BrandAppName\}/)
  assert.match(installer, /AppPublisher=\{#BrandAppPublisher\}/)
  assert.match(installer, /AppPublisherURL=\{#BrandAppPublisherURL\}/)
  assert.match(installer, /UninstallDisplayName=\{#BrandAppName\} \{#AppVersion\}/)
})


test('Windows installer source uses the nondeprecated file-copy API', async () => {
  const installer = await fs.readFile(path.join(root, 'packaging', 'windows', 'octo.iss'), 'utf8')
  assert.doesNotMatch(installer, /\bFileCopy\s*\(/)
  assert.match(installer, /\bCopyFile\s*\(/)
})
