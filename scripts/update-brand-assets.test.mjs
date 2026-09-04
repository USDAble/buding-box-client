import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const scriptPath = path.join(root, 'scripts', 'update-brand-assets.ps1')

async function readAssetScript() {
  return fs.readFile(scriptPath, 'utf8')
}

test('brand asset update script accepts a canonical PNG and runs the full sync chain', async () => {
  const script = await readAssetScript()

  assert.match(script, /\[Parameter\(Mandatory\)\]/)
  assert.match(script, /InputPath/)
  assert.match(script, /generate-brand-assets/)
  assert.match(script, /sync-branding\.mjs/)
  assert.match(script, /--check/)
  assert.match(script, /node --test/)
})

test('brand asset update script resolves tools without hard-coding a target GOARCH', async () => {
  const script = await readAssetScript()

  assert.doesNotMatch(script, /GOARCH\s*=/)
  assert.match(script, /ProgramFiles.*Go\\bin\\go\.exe/)
  assert.match(script, /Get-Command -Name \$CommandName/)
})

test('brand asset update script documents the generated platform asset set', async () => {
  const script = await readAssetScript()

  for (const fragment of [
    'icon.ico',
    'icon.icns',
    'tray-icon.png',
    'pudding-box-icon-1024.png',
  ]) {
    assert.match(script, new RegExp(fragment.replaceAll('.', '\\.') ))
  }
})
