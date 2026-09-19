// Rejects stale design-document paths in source and broken relative links in
// this fork's maintained documentation. Source comments must explain why; they
// must not preserve retired document paths as a second, unverified index.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const SOURCE_ROOTS = [
  'AGENTS.md', 'CLAUDE.md', '.octorules', 'CONTRIBUTING.md', 'Makefile',
  '.github', 'scripts', 'cmd', 'internal', 'web', 'mobile', 'packaging',
]
const DOC_ROOTS = ['dev-docs-puddingbox', 'CONTRIBUTING.md']
const SKIPPED_DIRECTORIES = new Set(['.git', 'node_modules', 'dist', 'vendor', 'webdist'])
const TEXT_EXTENSIONS = new Set(['', '.go', '.md', '.mjs', '.js', '.ts', '.svelte', '.html', '.yml', '.yaml', '.sh', '.ps1', '.json'])
const RETIRED_ROOT = 'dev-docs-' + 'usdable/'
const DOCUMENT_PATH = new RegExp(`(?:dev-docs-puddingbox|${RETIRED_ROOT.slice(0, -1)}|dev-docs)/[\\p{L}\\p{N}_./-]+\\.md`, 'gu')
const RETIRED_DOCUMENT_PATH = new RegExp(`${RETIRED_ROOT}[^\\s\`"'<>()[\\]，。；：,;]*`, 'gu')
const MARKDOWN_LINK = /\[[^\]]+\]\(([^)#]+)(?:#[^)]+)?\)/g

async function exists(file) {
  try {
    await fs.access(file)
    return true
  } catch {
    return false
  }
}

async function collect(root, rel, output) {
  const absolute = path.join(root, rel)
  let stat
  try {
    stat = await fs.stat(absolute)
  } catch {
    return
  }
  if (stat.isFile()) {
    if (TEXT_EXTENSIONS.has(path.extname(rel)) || path.basename(rel) === 'Makefile') output.push(rel)
    return
  }
  for (const entry of await fs.readdir(absolute, { withFileTypes: true })) {
    if (SKIPPED_DIRECTORIES.has(entry.name)) continue
    await collect(root, path.join(rel, entry.name), output)
  }
}

export async function check(root) {
  const problems = []
  const sourceFiles = []
  for (const sourceRoot of SOURCE_ROOTS) await collect(root, sourceRoot, sourceFiles)

  for (const rel of sourceFiles) {
    const content = await fs.readFile(path.join(root, rel), 'utf8')
    if (content.includes('\0')) continue
    for (const match of content.matchAll(RETIRED_DOCUMENT_PATH)) {
      problems.push(`${rel}: references retired document ${match[0]}`)
    }
    for (const match of content.matchAll(DOCUMENT_PATH)) {
      const reference = match[0]
      if (reference.startsWith(RETIRED_ROOT)) continue
      if (!(await exists(path.join(root, reference)))) {
        problems.push(`${rel}: references missing document ${reference}`)
      }
    }
  }

  const docFiles = []
  for (const docRoot of DOC_ROOTS) await collect(root, docRoot, docFiles)
  for (const rel of docFiles.filter((file) => file.endsWith('.md'))) {
    const content = await fs.readFile(path.join(root, rel), 'utf8')
    for (const match of content.matchAll(MARKDOWN_LINK)) {
      const target = match[1]
      if (/^(https?:|mailto:|#)/.test(target)) continue
      if (!(await exists(path.resolve(root, path.dirname(rel), target)))) {
        problems.push(`${rel}: relative link ${target} does not resolve`)
      }
    }
  }
  return { problems, scanned: { sourceFiles: sourceFiles.length, docFiles: docFiles.length } }
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const { problems, scanned } = await check(repositoryRoot(import.meta.url))
  if (problems.length) {
    console.error('docs-ref-guard failed:')
    for (const item of problems) console.error(`- ${item}`)
    process.exitCode = 1
  } else {
    console.log(`docs-ref-guard passed: ${scanned.sourceFiles} source files and ${scanned.docFiles} maintained documents checked.`)
  }
}
