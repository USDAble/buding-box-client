// Generates AGENTS.md from .octorules.
//
// Why this file exists (开发规范 §7.4). AGENTS.md is the one instruction file
// Codex reads, and Codex has no include/import directive: `@path` inside an
// AGENTS.md is passed through as literal text (openai/codex#17401, still open),
// so a pointer only works if the model chooses to follow it. The only way to
// *guarantee* Codex receives the upstream rules is to inline them.
//
// A pointer is also insufficient because of the byte budget: Codex caps the
// whole root→cwd AGENTS.md chain at `project_doc_max_bytes` (default 32 KiB,
// cumulative per codex-rs/core/src/agents_md.rs). 开发规范.md alone is ~41 KB
// and CLAUDE.md ~15 KB, so neither can be inlined; `.octorules` (~10 KB) can,
// and it is already the concise index of the upstream rules — including the
// three hard rules.
//
// So: 开发规范.md and CLAUDE.md stay pointers, `.octorules` gets inlined
// verbatim. One source of truth — edit .octorules, run `make agents`, commit
// both. `--check` fails CI when the two drift, the same way sync-branding.mjs
// does for the generated brand copies.
//
// The fork rules live in an *upstream* file. `git diff origin/main -- .octorules`
// shows the fork added them (+35/-5), and an upstream merge could revert that
// silently — leaving AGENTS.md generated without the fork rules, still passing
// every other check. REQUIRED_OCTORULES_ANCHORS turns that into a hard failure.
//
// Usage:
//   node scripts/sync-agents.mjs           write AGENTS.md
//   node scripts/sync-agents.mjs --check   fail if AGENTS.md is stale (CI)
//
// No npm dependencies.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { NORMS_PATH, UPSTREAM_NORMS, repositoryRoot } from './norms-guard.mjs'

export const OCTORULES_PATH = '.octorules'
export const AGENTS_PATH = 'AGENTS.md'

export const GENERATED_NOTICE =
  `Generated from ${OCTORULES_PATH} by scripts/sync-agents.mjs. Do not edit directly.`

export const BEGIN_MARKER =
  `<!-- BEGIN inlined ${OCTORULES_PATH} — edit ${OCTORULES_PATH} and run \`make agents\` -->`
export const END_MARKER = `<!-- END inlined ${OCTORULES_PATH} -->`

// Anchors the inlined file must still carry. Each one is a rule an upstream
// merge could drop without any other check noticing: the three hard rules, the
// fork/upstream standing pointer, and the merge policy. If `.octorules` loses
// one, this is a fork-rule regression, not a formatting drift.
export const REQUIRED_OCTORULES_ANCHORS = [
  { needle: NORMS_PATH, why: '上游规则必须继续声明 fork 规范为 binding' },
  { needle: '## Fork rules', why: '三条硬规则所在的章节' },
  { needle: 'Never resolve a data path yourself', why: '硬规则 1（数据路径）' },
  { needle: 'Never hardcode a brand string', why: '硬规则 2（品牌字面量）' },
  { needle: 'Mark every change to an upstream file', why: '硬规则 3（OCTO-FORK 标记）' },
  { needle: 'never `rebase`', why: '上游合并策略（merge 不 rebase）' },
]

// checkOctorules reports the anchors missing from the inlined source. Returned
// rather than thrown so the caller decides how to report and exit.
export function checkOctorules(content) {
  const problems = []
  for (const { needle, why } of REQUIRED_OCTORULES_ANCHORS) {
    if (!content.includes(needle)) {
      problems.push(
        `${OCTORULES_PATH}: 缺少 ${JSON.stringify(needle)} —— ${why}。` +
          `可能是上游合并覆盖了本 fork 的改动；补齐后再运行 \`make agents\`。`,
      )
    }
  }
  return problems
}

// The preamble is fork-authored, not derived, so it lives here. It must name
// NORMS_PATH or norms-guard (which asserts every entry file points at the spec)
// fails.
export function renderPreamble() {
  return `# AGENTS.md

**This file is an entry point, not the spec.** Every AI coding tool that reads or edits this repository MUST read and obey, in this order:

1. **\`${NORMS_PATH}\`** — this fork's binding engineering norms (branching, the three hard rules, review, testing, DoD).
2. **\`${UPSTREAM_NORMS[0]}\`** and **\`${UPSTREAM_NORMS[1]}\`** — the upstream normative rules, of **equal standing** with the fork spec (开发规范 §3.6). \`${UPSTREAM_NORMS[1]}\` is the fuller write-up, \`${UPSTREAM_NORMS[0]}\` the short index.

The full \`${OCTORULES_PATH}\` text is **inlined below verbatim**, so a tool that reads only this file still receives the upstream rules — including the three hard rules under "Fork rules". \`${NORMS_PATH}\` and \`${UPSTREAM_NORMS[1]}\` stay pointers: both exceed the per-file instruction budget Codex imposes (\`project_doc_max_bytes\`, 32 KiB by default), so they cannot be inlined. Nothing here replaces them.

\`scripts/norms-guard.mjs\` and \`scripts/sync-agents.mjs --check\` (CI \`norms-guard\` / \`agents-guard\` jobs, \`make norms-check\` / \`make agents-check\`, and the packaging preflight) fail the build if this file is missing, stops pointing at the fork spec, or drifts from \`${OCTORULES_PATH}\`.

## Where things live

| Content | Location |
|---|---|
| This fork's norms and upstream-merge policy | \`${NORMS_PATH}\`, \`dev-docs-usdable/上游合并策略.md\` |
| This fork's requirements, plans, per-PR design docs | \`dev-docs-usdable/需求/<批次>/\` |
| Upstream architecture decisions | \`dev-docs/\` — **upstream directory, do not add downstream docs here** |
`
}

// renderAgents is the whole generated file. Exported so the tests can assert on
// it without touching disk.
export function renderAgents(octorules) {
  return `${GENERATED_NOTICE}

${renderPreamble()}
---

${BEGIN_MARKER}

${octorules.trimEnd()}

${END_MARKER}
`
}

async function readIfPresent(absolutePath) {
  try {
    return await fs.readFile(absolutePath)
  } catch (error) {
    if (error?.code === 'ENOENT') return null
    throw error
  }
}

// check is the shape every other guard exposes, so preflight.mjs can call it
// uniformly. It reports two distinct failures: the source lost its fork rules
// (an upstream merge reverted them), or the generated file drifted.
export async function check(root) {
  const problems = []
  const notes = []

  const octorules = await readIfPresent(path.join(root, OCTORULES_PATH))
  if (octorules === null) {
    problems.push(`${OCTORULES_PATH}: 缺失 —— 无法生成 ${AGENTS_PATH}。`)
    return { problems, notes }
  }
  const source = octorules.toString('utf8')

  const anchorProblems = checkOctorules(source)
  problems.push(...anchorProblems)
  if (anchorProblems.length > 0) {
    // Regenerating from a source that lost the fork rules would emit a
    // compliant-looking AGENTS.md without them, so do not advise that.
    notes.push(`锚点缺失时不要重新生成 ${AGENTS_PATH} —— 先把 ${OCTORULES_PATH} 里的 fork 规则恢复回来。`)
  } else {
    notes.push(`${OCTORULES_PATH}: 三条硬规则与合并策略锚点齐全。`)
  }

  const expected = Buffer.from(renderAgents(source), 'utf8')
  const actual = await readIfPresent(path.join(root, AGENTS_PATH))
  if (actual === null) {
    problems.push(`${AGENTS_PATH}: 缺失 —— Codex 这类只读该文件的工具会看不到上游规则。运行 \`make agents\`。`)
  } else if (!actual.equals(expected)) {
    problems.push(`${AGENTS_PATH}: 与 ${OCTORULES_PATH} 不一致（漂移）。运行 \`make agents\` 并提交。`)
  } else {
    notes.push(`${AGENTS_PATH}: 与 ${OCTORULES_PATH} 同步（${expected.length} 字节，已内联上游规则）。`)
  }

  return { problems, notes }
}

async function main(argv) {
  const flags = argv.slice(2)
  const checkOnly = flags.includes('--check')
  const unknown = flags.filter((flag) => flag !== '--check')
  if (unknown.length > 0) {
    console.error(`未知参数：${unknown.join(' ')}`)
    console.error('用法：node scripts/sync-agents.mjs [--check]')
    process.exitCode = 2
    return
  }

  const root = repositoryRoot(import.meta.url)

  if (checkOnly) {
    const { problems, notes } = await check(root)
    for (const note of notes) console.log(`sync-agents: ${note}`)
    if (problems.length > 0) {
      console.error('sync-agents failed:')
      for (const problem of problems) console.error(`- ${problem}`)
      process.exitCode = 1
      return
    }
    console.log('sync-agents passed: AGENTS.md 与 .octorules 同步。')
    return
  }

  let source
  try {
    source = await fs.readFile(path.join(root, OCTORULES_PATH), 'utf8')
  } catch (error) {
    if (error?.code === 'ENOENT') {
      console.error(`${OCTORULES_PATH} 不存在 —— 无法生成 ${AGENTS_PATH}。`)
      process.exitCode = 1
      return
    }
    throw error
  }

  const anchorProblems = checkOctorules(source)
  if (anchorProblems.length > 0) {
    console.error(`${OCTORULES_PATH} 校验未通过，已终止生成 ${AGENTS_PATH}：`)
    for (const problem of anchorProblems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }

  const expected = Buffer.from(renderAgents(source), 'utf8')
  await fs.writeFile(path.join(root, AGENTS_PATH), expected)
  console.log(`已从 ${OCTORULES_PATH} 生成 ${AGENTS_PATH}（${expected.length} 字节）。`)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main(process.argv)
}
