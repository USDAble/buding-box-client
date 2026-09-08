// scripts/datapath-guard.mjs
//
// Guards the portable-data-root rule (开发规范.md §3.1): product code must not
// write to the host home. It scans internal/, cmd/, shared/ for non-_test.go
// Go files and fails on two classes of hit:
//
//   - the ".octo" string literal — zero exceptions. It was the pre-fork data
//     root and no longer exists in the product (CLI included), so any
//     occurrence is a regression.
//   - os.UserHomeDir() — fails unless the file is in scripts/homedir-allowlist.txt
//     (the real-host-home access list; one written reason per entry).
//
// It ALSO scans the runtime prompt/skill text under internal/prompt and
// internal/skills (every .md and non-_test.go file) for the "~/.octo" host-home
// path reference. Unlike ".octo" (which only matches the bare directory-name
// literal), "~/.octo/…" spells out a write to the host home and slipped through
// the original Go-literal guard — the agent prompt once told the agent to
// `write_file` Light Apps to `~/.octo/light-apps/`, polluting the host home.
// Those strings are the runtime instructions the agent reads, so they must
// reference the data root (via the `<data root>` placeholder) instead. Zero
// exceptions — LICENSE.txt attribution and _test.go files are excluded.
//
// Usage:
//   node scripts/datapath-guard.mjs
//
// No npm dependencies. Run it in CI (see .github/workflows/go.yml, the
// datapath-guard job) and locally via `make datapath-check`. The same
// ".octo"-literal assertion also runs as a Go test (internal/datapath) so
// `make test` catches a regression without Node.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const SCAN_DIRS = ['internal', 'cmd', 'shared']
// Runtime prompt/skill text — the strings the agent actually reads and acts on.
// Only these two trees are checked for "~/.octo"; the rest of the repo's
// developer-facing comments and CLI help text are a separate, non-runtime
// cleanup (and would otherwise trip a zero-exception guard with historical
// prose).
const PROMPT_DIRS = ['internal/prompt', 'internal/skills']
const ALLOWLIST_REL = 'scripts/homedir-allowlist.txt'

// The pre-fork data-root segment as a Go string literal. Match the exact
// double-quoted ".octo" so ".octo-hooks.yml" (the renamed project-level hooks
// file), ".octorules", and the ".octo" path name in prose never trip it.
export const OCTO_LITERAL = /"\.octo"/

// "~/.octo/…" — an explicit host-home data path, not the bare directory-name
// literal. Banned in runtime prompt/skill text (see check's second pass).
export const OCTO_HOME_PATH = /~\/\.octo/

// os.UserHomeDir() call — the function that, used outside internal/datapath,
// reaches the host home.
export const USER_HOME_DIR = /os\.UserHomeDir\(\)/

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

// loadAllowlist parses scripts/homedir-allowlist.txt. Each entry becomes
// { prefix, dir } where dir=true means the prefix matches recursively (a
// trailing "/" in the file). Blank lines and "#" comments are ignored.
export async function loadAllowlist(root) {
  const raw = await fs.readFile(path.join(root, ALLOWLIST_REL), 'utf8')
  const entries = []
  for (const line of raw.split('\n')) {
    const trimmed = line.trim()
    if (trimmed === '' || trimmed.startsWith('#')) continue
    const entry = trimmed.split(/\s+/)[0]
    if (entry.endsWith('/')) {
      entries.push({ prefix: entry.slice(0, -1), dir: true })
    } else {
      entries.push({ prefix: entry, dir: false })
    }
  }
  return entries
}

export function isAllowed(rel, entries) {
  for (const { prefix, dir } of entries) {
    if (dir) {
      if (rel === prefix || rel.startsWith(prefix + path.sep)) return true
    } else if (rel === prefix) {
      return true
    }
  }
  return false
}

// collectGoFiles returns repo-relative paths of every non-_test.go Go file
// under the scan dirs. A missing scan dir (e.g. shared/ on some layouts) is
// tolerated, not an error.
export async function collectGoFiles(root) {
  const files = []
  for (const dir of SCAN_DIRS) {
    await walk(path.join(root, dir), files)
  }
  return files

  async function walk(dir, out) {
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
        await walk(abs, out)
      } else if (entry.isFile() && entry.name.endsWith('.go') && !entry.name.endsWith('_test.go')) {
        out.push(path.relative(root, abs))
      }
    }
  }
}

// collectPromptFiles returns repo-relative paths of every .md file and
// non-_test.go Go file under internal/prompt and internal/skills — the runtime
// text the agent reads and acts on. LICENSE.txt and other attribution files
// are .txt, so they fall outside the .md/.go filter and are left alone.
export async function collectPromptFiles(root) {
  const files = []
  for (const dir of PROMPT_DIRS) {
    await walk(path.join(root, dir), files)
  }
  return files

  async function walk(dir, out) {
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
        await walk(abs, out)
      } else if (entry.isFile() &&
        (entry.name.endsWith('.md') || (entry.name.endsWith('.go') && !entry.name.endsWith('_test.go')))) {
        out.push(path.relative(root, abs))
      }
    }
  }
}

// check returns a list of human-readable problems; an empty list means clean.
export async function check(root) {
  const entries = await loadAllowlist(root)
  const problems = []
  const files = await collectGoFiles(root)

  for (const rel of files) {
    const content = await fs.readFile(path.join(root, rel), 'utf8')
    if (OCTO_LITERAL.test(content)) {
      problems.push(`${rel}: contains the forbidden ".octo" string literal (zero exceptions)`)
    }
    if (USER_HOME_DIR.test(content) && !isAllowed(rel, entries)) {
      problems.push(`${rel}: calls os.UserHomeDir() and is not in ${ALLOWLIST_REL}`)
    }
  }

  // Runtime prompt/skill text must not reference the host-home "~/.octo" path:
  // those are the instructions the agent reads, so a reference there teaches
  // the agent to write outside the data root.
  const promptFiles = await collectPromptFiles(root)
  for (const rel of promptFiles) {
    const content = await fs.readFile(path.join(root, rel), 'utf8')
    if (OCTO_HOME_PATH.test(content)) {
      problems.push(`${rel}: runtime prompt/skill text references "~/.octo" (zero exceptions — use "<data root>")`)
    }
  }
  return problems
}

async function main() {
  const root = repositoryRoot(import.meta.url)
  const problems = await check(root)
  if (problems.length > 0) {
    console.error('datapath-guard failed:')
    for (const problem of problems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }
  console.log('datapath-guard passed: no ".octo" literals, no "~/.octo" in prompt/skill text, all os.UserHomeDir() calls allowlisted.')
}

// Only run the CLI when invoked directly, so importing check/loadAllowlist for
// a test is free of side effects.
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
