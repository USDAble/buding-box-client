import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { promisify } from 'node:util'

const execFileAsync = promisify(execFile)
const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const syncSource = path.join(repositoryRoot, 'scripts', 'sync-branding.mjs')

async function makeFixture() {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'pudding-brand-sync-'))
  await fs.mkdir(path.join(root, 'scripts'), { recursive: true })
  await fs.mkdir(path.join(root, 'branding', 'assets'), { recursive: true })
  await fs.copyFile(syncSource, path.join(root, 'scripts', 'sync-branding.mjs'))
  await fs.writeFile(path.join(root, 'branding', 'brand.json'), JSON.stringify({
    visual: { logo: { mark: 'assets/mark.svg' } },
    product: { names: { 'en-US': 'Pudding Box' } },
    about: { teamName: { 'en-US': 'Pudding Box Studio' } },
    links: { website: 'https://puddingbox.example' },
  }, null, 2))
  await fs.writeFile(path.join(root, 'branding', 'assets', 'mark.svg'), '<svg xmlns="http://www.w3.org/2000/svg"/>')
  return root
}

async function runSync(root, ...args) {
  return execFileAsync(process.execPath, [path.join(root, 'scripts', 'sync-branding.mjs'), ...args], {
    cwd: root,
    encoding: 'utf8',
  })
}

test('check fails without writing when generated brand targets are missing', async (t) => {
  const root = await makeFixture()
  t.after(() => fs.rm(root, { recursive: true, force: true }))

  await assert.rejects(runSync(root, '--check'), /out of sync|missing|generated/i)
  await assert.rejects(fs.access(path.join(root, 'internal', 'brand', 'brand.json')))
})

test('sync creates targets and check passes only while they match the source', async (t) => {
  const root = await makeFixture()
  t.after(() => fs.rm(root, { recursive: true, force: true }))

  await runSync(root)
  await runSync(root, '--check')

  const generated = path.join(root, 'web', 'src', 'lib', 'brand.config.json')
  await fs.writeFile(generated, '{"stale":true}')
  await assert.rejects(runSync(root, '--check'), /out of sync/i)
})

test('sync generates Windows installer brand macros from the canonical configuration', async (t) => {
  const root = await makeFixture()
  t.after(() => fs.rm(root, { recursive: true, force: true }))

  await runSync(root)

  const installerBrand = await fs.readFile(path.join(root, 'packaging', 'windows', 'brand.iss'), 'utf8')
  assert.match(installerBrand, /#define BrandAppName "Pudding Box"/)
  assert.match(installerBrand, /#define BrandAppPublisher "Pudding Box Studio"/)
  assert.match(installerBrand, /#define BrandAppPublisherURL "https:\/\/puddingbox\.example"/)
})
