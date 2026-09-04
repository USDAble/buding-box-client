import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const scriptPath = path.join(root, 'scripts', 'package-desktop-windows.ps1')

async function readPackageScript() {
  return fs.readFile(scriptPath, 'utf8')
}

test('Windows package script exposes explicit target architecture and separate ARM64 output', async () => {
  const script = await readPackageScript()

  assert.match(script, /\[ValidateSet\('amd64', 'arm64'\)\]/)
  assert.match(script, /\$Arch\s*=\s*'amd64'/)
  assert.match(script, /staging-arm64/)
  assert.match(script, /octo-setup-arm64/)
})

test('Windows package script validates branding and embeds ripgrep for the target architecture', async () => {
  const script = await readPackageScript()

  assert.match(script, /& \$node \$syncScript --check/)
  assert.match(script, /embed-rg-windows\.ps1/)
  assert.match(script, /-Arch \$Arch/)
})

test('Windows package script keeps Go target settings process-local and builds both installer binaries', async () => {
  const script = await readPackageScript()

  assert.match(script, /GOARCH/)
  assert.match(script, /CGO_ENABLED/)
  assert.match(script, /finally/)
  assert.match(script, /octo-desktop\.exe/)
  assert.match(script, /octo\.exe/)
  assert.match(script, /generate-syso/)
  assert.match(script, /ISCC\.exe/)
  assert.match(script, /Get-FileHash/)
  assert.match(script, /\[string\]\$UvExecutable/)
  assert.match(script, /\[string\]\$OutputDirectory/)
  assert.match(script, /Invoke-WebRequest[\s\S]*-TimeoutSec 120/)
})



test('Windows package script tolerates unrelated uninstall registry entries under strict mode', async () => {
  const script = await readPackageScript()

  assert.match(script, /PSObject\.Properties\['DisplayName'\]/)
  assert.match(script, /PSObject\.Properties\['InstallLocation'\]/)
})

test('Windows package script refuses SkipWebBuild unless a built web entrypoint exists', async () => {
  const script = await readPackageScript()

  assert.match(script, /internal\\server\\webdist\\index\.html/)
})
