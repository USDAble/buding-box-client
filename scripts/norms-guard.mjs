// scripts/norms-guard.mjs
//
// Guards the rule that every AI coding tool touching this repo is told to read
// and obey dev-docs-usdable/开发规范.md (开发规范 §3.6). The fork's hard rules
// are only as strong as their discovery: each tool reads a different entry
// point — Cursor reads .cursor/rules/*.mdc, Claude Code reads CLAUDE.md,
// Copilot reads .github/copilot-instructions.md, Codex/Gemini CLI/Aider read
// AGENTS.md, and upstream's agents read .octorules. If one entry file is
// missing, or silently stops pointing at the spec during a rename, every agent
// using that tool loses the fork rules at once.
//
// That failure is invisible in review, which is why it is a ratchet like the
// other guards in this directory: the check runs in CI (.github/workflows/go.yml,
// norms-guard job), locally via `make norms-check`, and at the very start of a
// packaging run (scripts/preflight.mjs).
//
// It also enforces 开发规范 §3.6's premise: .octorules and CLAUDE.md are
// *binding upstream specs* of equal standing, not reference material, so the
// canonical file must name both.
//
// Usage:
//   node scripts/norms-guard.mjs
//
// No npm dependencies.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// The canonical fork spec. Every entry point must name this exact path.
export const NORMS_PATH = 'dev-docs-usdable/开发规范.md'

// The upstream normative pair (开发规范 §3.6). Both are binding, both must keep
// pointing at the fork spec, and the canonical file must acknowledge both.
export const UPSTREAM_NORMS = ['.octorules', 'CLAUDE.md']

// One entry point per AI tool family. `alwaysApply` marks Cursor's rule file,
// whose frontmatter must set it true or Cursor will not inject the rule on
// requests that match no glob.
export const ENTRY_FILES = [
  { file: 'AGENTS.md', why: 'cross-tool convention (Codex, Cursor, Gemini CLI, Aider, …)' },
  { file: '.cursor/rules/dev-norms.mdc', why: 'Cursor project rule', alwaysApply: true },
  { file: '.github/copilot-instructions.md', why: 'GitHub Copilot' },
  { file: 'CLAUDE.md', why: 'Claude Code' },
  { file: '.octorules', why: 'upstream-agent project rules' },
]

// checkEntry reports the problems in one entry file's content.
export function checkEntry(rel, content, { alwaysApply = false } = {}) {
  const problems = []
  if (!content.includes(NORMS_PATH)) {
    problems.push(
      `${rel}: does not point at ${NORMS_PATH} — an agent reading only this file would miss the fork rules.`,
    )
  }
  if (alwaysApply && !/^alwaysApply:\s*true\s*$/m.test(content)) {
    problems.push(`${rel}: Cursor rule must set "alwaysApply: true" or it is not injected on every request.`)
  }
  return problems
}

export async function check(root) {
  const problems = []
  const notes = []

  // The canonical file must exist, and must itself treat the upstream pair as
  // binding (开发规范 §3.6), or the whole hierarchy is a one-way pointer.
  let canonical = null
  try {
    canonical = await fs.readFile(path.join(root, NORMS_PATH), 'utf8')
  } catch (error) {
    if (error?.code === 'ENOENT') {
      problems.push(`${NORMS_PATH}: canonical fork spec is missing.`)
    } else {
      throw error
    }
  }
  if (canonical !== null) {
    for (const upstream of UPSTREAM_NORMS) {
      if (!canonical.includes(upstream)) {
        problems.push(`${NORMS_PATH}: must name ${upstream} as a binding upstream spec (开发规范 §3.6).`)
      }
    }
    notes.push(`${NORMS_PATH}: present, names ${UPSTREAM_NORMS.join(' + ')}.`)
  }

  for (const entry of ENTRY_FILES) {
    let content
    try {
      content = await fs.readFile(path.join(root, entry.file), 'utf8')
    } catch (error) {
      if (error?.code === 'ENOENT') {
        problems.push(`${entry.file}: missing — ${entry.why} would not see ${NORMS_PATH}.`)
        continue
      }
      throw error
    }
    problems.push(...checkEntry(entry.file, content, { alwaysApply: entry.alwaysApply }))
    notes.push(`${entry.file}: present (${entry.why}).`)
  }

  return { problems, notes }
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

async function main() {
  const root = repositoryRoot(import.meta.url)
  const { problems, notes } = await check(root)

  for (const note of notes) console.log(`norms-guard: ${note}`)

  if (problems.length > 0) {
    console.error('norms-guard failed:')
    for (const problem of problems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }
  console.log('norms-guard passed: every AI-tool entry point points at the fork spec.')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
