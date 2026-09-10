// scripts/reuse-guard.mjs
//
// Guards the rule that this fork reuses upstream capabilities instead of
// growing a second implementation of them (开发规范 §3.5 "复用优先",
// 问题盘点 §G7).
//
// Why it needs a guard: the mistake is invisible in review. Writing a fresh
// SSE parser looks like normal work — it is self-contained, it passes its own
// tests, and it reads well. The cost only shows up later, when upstream fixes
// a wire-format bug (the missing `[DONE]` sentinel, chunked tool-call JSON,
// the cache-token accounting difference) and the second implementation does
// not get the fix. The 中台 gateway is an OpenAI-compatible
// `chat/completions` endpoint, and this repo already has that client:
//
//     internal/provider/openai        SSE aggregation, tool-call reassembly,
//                                     usage normalisation, [DONE] tolerance
//     internal/provider/anthropic     the Anthropic-protocol sibling
//     internal/app.NewSender          the single constructor for both, with
//                                     arbitrary BaseURL / headers / RPM
//
// So the gateway package is allowed to be **assembly + observation**: build an
// `app.Sender` over a custom endpoint, and wrap it to surface `clientRequestId`,
// terminal usage and error codes. It is NOT allowed to speak HTTP or SSE.
//
// Usage:
//   node scripts/reuse-guard.mjs
//
// No npm dependencies. Run it in CI (.github/workflows/go.yml, reuse-guard job)
// and locally via `make reuse-check`.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// Where the duplicate would land, and which upstream capability must be used
// instead. Add an entry only together with a note in P0-01 §gateway 的复用形态.
export const REUSE_SCOPES = [
  {
    dir: 'internal/productclient/gateway',
    label: '中台模型网关 sender',
    useInstead: 'app.NewSender (internal/app) + internal/provider/{openai,anthropic}',
    // Symbols that mean this package started implementing the protocol itself.
    mustNot: [
      { pattern: /"net\/http"/, why: 'HTTP client — construct an app.Sender over a custom endpoint instead' },
      { pattern: /\bhttp\.Client\b|\bhttp\.NewRequest\b|\bhttp\.Do\(/, why: 'HTTP client — app.NewSender owns the transport' },
      { pattern: /\bbufio\b/, why: 'SSE line scanning — internal/provider/openai owns SSE aggregation' },
      { pattern: /text\/event-stream/, why: 'SSE protocol knowledge — internal/provider owns the wire format' },
      { pattern: /\[DONE\]/, why: 'SSE sentinel parsing — internal/provider/openai already tolerates it' },
      { pattern: /\bjson\.NewDecoder\b/, why: 'streaming JSON parsing — internal/provider assembles tool-call fragments' },
      {
        // Wire-format field names only. The agent-facing type is
        // agent.Usage{InputTokens, CacheReadTokens}, which the observer MAY
        // read — those names are deliberately not listed here.
        pattern: /\bprompt_?tokens\b|\bcompletion_?tokens\b/i,
        why: 'wire-format usage parsing — internal/provider normalises cache-token semantics',
      },
      {
        pattern: /\bbackoff\b|retry[-_]?after/i,
        why: 'retry policy — internal/provider/app owns transport retries',
      },
    ],
  },
]

// The explicit escape hatch, mirroring release-profile-guard. A line carrying
// this marker is skipped; the marker must be followed by a reason.
export const ALLOW_MARKER = 'reuse-guard:allow'

// ─── pure analyzers (unit-tested) ───────────────────────────────────────────

// stripComment removes a trailing `//` comment so a pattern mentioned in prose
// does not trip the guard, while a marker in that comment is still honoured.
export function stripComment(line) {
  const idx = line.indexOf('//')
  if (idx === -1) return { code: line, comment: '' }
  return { code: line.slice(0, idx), comment: line.slice(idx) }
}

// checkSource reports the reuse violations in one Go file.
export function checkSource(rel, content, scope) {
  const problems = []
  const lines = content.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const { code, comment } = stripComment(lines[i])
    if (`${code}${comment}`.includes(ALLOW_MARKER)) continue
    if (code.trim().length === 0) continue
    for (const rule of scope.mustNot) {
      if (!rule.pattern.test(code)) continue
      problems.push(
        `${rel}:${i + 1}: ${rule.why}. ` +
          `Use ${scope.useInstead}, or mark the line with "${ALLOW_MARKER} <reason>" if this is genuinely needed.`,
      )
    }
  }
  return problems
}

// ─── filesystem-backed scan ─────────────────────────────────────────────────

async function goFilesIn(root, dir) {
  const abs = path.join(root, dir)
  let entries
  try {
    entries = await fs.readdir(abs, { withFileTypes: true })
  } catch (error) {
    if (error?.code === 'ENOENT') return null // package not created yet
    throw error
  }
  const files = []
  for (const entry of entries) {
    if (!entry.isFile()) continue
    // Skip generated/test fixtures: the guard governs shipped implementation.
    if (!entry.name.endsWith('.go')) continue
    if (entry.name.endsWith('_test.go')) continue
    files.push(path.posix.join(dir, entry.name))
  }
  return files
}

export async function check(root) {
  const problems = []
  const notes = []

  for (const scope of REUSE_SCOPES) {
    const files = await goFilesIn(root, scope.dir)
    if (files === null) {
      notes.push(`${scope.dir}: not created yet — nothing to check (expected from P0-01 B0).`)
      continue
    }
    if (files.length === 0) {
      notes.push(`${scope.dir}: no non-test Go files — nothing to check.`)
      continue
    }
    for (const rel of files) {
      const content = await fs.readFile(path.join(root, rel), 'utf8')
      problems.push(...checkSource(rel, content, scope))
    }
    notes.push(`${scope.dir}: ${files.length} file(s) checked against ${scope.useInstead}.`)
  }

  return { problems, notes }
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

async function main() {
  const root = repositoryRoot(import.meta.url)
  const { problems, notes } = await check(root)

  for (const note of notes) console.log(`reuse-guard: ${note}`)

  if (problems.length > 0) {
    console.error('reuse-guard failed:')
    for (const problem of problems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }
  console.log('reuse-guard passed: no fork package re-implements an upstream capability.')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
