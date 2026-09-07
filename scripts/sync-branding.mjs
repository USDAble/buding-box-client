// Distributes branding/brand.json to every consumer that cannot import it
// directly: Go packages need a file next to their go:embed directive, the
// nested octo-relay module cannot reach the root module's internal/, and Inno
// Setup needs #define lines rather than JSON.
//
// Usage:
//   node scripts/sync-branding.mjs           write the generated targets
//   node scripts/sync-branding.mjs --check   fail if any target is stale (CI)
//
// The source is validated first — an invalid configuration is never
// distributed, so a bad value cannot reach six consumers before anyone notices.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import {
  BRAND_SOURCE_RELATIVE_PATH,
  loadBrand,
  repositoryRoot,
  validateBrand,
} from './brand-schema.mjs'

const GENERATED_NOTICE = `Generated from ${BRAND_SOURCE_RELATIVE_PATH} by scripts/sync-branding.mjs. Do not edit directly.`

// JSON has no comments, so provenance rides in a leading underscore field.
// Consumers ignore unknown keys; the schema validator skips underscore keys.
export function renderBrandJSON(brand) {
  return `${JSON.stringify({ _generated: GENERATED_NOTICE, ...brand }, null, 2)}\n`
}

function requireText(value, dottedPath) {
  if (typeof value !== 'string' || value.trim() === '') {
    throw new Error(`${BRAND_SOURCE_RELATIVE_PATH} 需要非空的 ${dottedPath}`)
  }
  if (/[\r\n]/.test(value)) {
    throw new Error(`${BRAND_SOURCE_RELATIVE_PATH} 的 ${dottedPath} 不能包含换行`)
  }
  return value
}

// Inno Setup's preprocessor doubles quotes to escape them inside a string.
function escapeInnoString(value) {
  return value.replaceAll('"', '""')
}

// The installer shows the fixed Windows display name (B class) rather than a
// localized one: MSI-style system UI is not re-rendered on UI language change.
export function renderWindowsInstallerBrand(brand) {
  const appName = requireText(brand.display?.windows?.productName, 'display.windows.productName')
  const publisher = requireText(brand.about?.teamName?.['en-US'], "about.teamName['en-US']")
  const publisherURL = requireText(brand.links?.external?.website, 'links.external.website')

  return [
    `; ${GENERATED_NOTICE}`,
    `#define BrandAppName "${escapeInnoString(appName)}"`,
    `#define BrandAppPublisher "${escapeInnoString(publisher)}"`,
    `#define BrandAppPublisherURL "${escapeInnoString(publisherURL)}"`,
    '',
  ].join('\n')
}

// buildTargets returns the full generated-file set. Adding a consumer means
// adding one row here — nothing else in the pipeline changes.
export function buildTargets(brand) {
  const brandJSON = Buffer.from(renderBrandJSON(brand), 'utf8')
  const jsonDestinations = [
    // go:embed for the desktop shell and the in-process server.
    ['internal', 'brand', 'brand.json'],
    // Vite import for the web UI.
    ['web', 'src', 'lib', 'brand.config.json'],
    ['mobile', 'src', 'brand.config.json'],
    ['landing', 'brand.config.json'],
    // Nested Go module: cannot import the root module's internal/brand.
    ['cmd', 'octo-relay', 'internal', 'push', 'brand.json'],
  ]

  return [
    ...jsonDestinations.map((segments) => ({
      destination: path.join(...segments),
      content: brandJSON,
    })),
    {
      destination: path.join('packaging', 'windows', 'brand.iss'),
      content: Buffer.from(renderWindowsInstallerBrand(brand), 'utf8'),
    },
  ]
}

async function readIfPresent(absolutePath) {
  try {
    return await fs.readFile(absolutePath)
  } catch (error) {
    if (error?.code === 'ENOENT') return null
    throw error
  }
}

async function main(argv) {
  const flags = argv.slice(2)
  const checkOnly = flags.includes('--check')
  const unknown = flags.filter((flag) => flag !== '--check')
  if (unknown.length > 0) {
    console.error(`未知参数：${unknown.join(' ')}`)
    console.error('用法：node scripts/sync-branding.mjs [--check]')
    process.exitCode = 2
    return
  }

  const root = repositoryRoot(import.meta.url)
  const brand = await loadBrand(root)

  const schemaErrors = validateBrand(brand)
  if (schemaErrors.length > 0) {
    console.error(`${BRAND_SOURCE_RELATIVE_PATH} 校验未通过，已终止同步：`)
    for (const error of schemaErrors) console.error(`- ${error}`)
    process.exitCode = 1
    return
  }

  const targets = buildTargets(brand)

  if (checkOnly) {
    const stale = []
    for (const target of targets) {
      const actual = await readIfPresent(path.join(root, target.destination))
      if (actual === null || !actual.equals(target.content)) stale.push(target.destination)
    }
    if (stale.length > 0) {
      console.error('以下生成目标与 brand.json 不一致：')
      for (const destination of stale) console.error(`- ${destination}`)
      console.error('运行：node scripts/sync-branding.mjs')
      process.exitCode = 1
      return
    }
    console.log(`品牌生成目标全部同步（已检查 ${targets.length} 个）。`)
    return
  }

  for (const target of targets) {
    const absolutePath = path.join(root, target.destination)
    await fs.mkdir(path.dirname(absolutePath), { recursive: true })
    await fs.writeFile(absolutePath, target.content)
  }
  console.log(`已同步品牌配置到 ${targets.length} 个生成目标。`)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main(process.argv)
}
