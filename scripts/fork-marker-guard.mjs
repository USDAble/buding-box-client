// scripts/fork-marker-guard.mjs
//
// Hard rule 3 (开发规范 §3.3) says every change to an upstream file carries
// `// OCTO-FORK: <why> — see <design doc>`, and that
// `grep -rn "OCTO-FORK" .` is this fork's complete diff-from-upstream
// inventory. That inventory is what an upstream merge is done from: it is how
// a human tells "our line, keep it" from "upstream's line, take theirs"
// without re-reading the whole diff.
//
// Nothing checked it, and the rule had drifted to 68 of 324 files (21%) —
// measured on 2026-09-13, before this guard existed. The failure mode is worth
// naming, because it is the same one three other defects in this round shared:
//
//   The rule read as satisfied, because a reader who greps finds *a* hit.
//   CLAUDE.md and .octorules contain the token — they define the rule — so a
//   `grep -c` census says 70 while 24 of those files carry no marker at all.
//   A check that cannot notice a missing marker is not a check; it is a
//   ritual. That is why this guard:
//
//     - anchors on a marker LINE, not on the token anywhere (MENTION below is
//       the counter-example the loose form would have accepted);
//     - fails when it examined zero files, rather than reporting success. A
//       guard that checked nothing and a guard that found nothing wrong print
//       the same sentence, and only one of them is good news. server-diff-guard
//       shipped that bug (V-49): its R1 counted a symbol that does not exist
//       and announced "the ratchet target is reached" forever.
//
// The guard is a **ratchet**, like server-diff-guard: MARKER_DEBT_CEILING is
// today's count and may only shrink. The fix for a failure is to add the
// marker, not to raise the ceiling; raising it is an explicit, reviewable edit
// to this file.
//
// Two things are deliberately NOT this guard's job (开发规范 §3.10):
//
//   - It does not judge whether a reason is *good* — only that a marker exists.
//     A wrong-but-present reason is a review matter; the guard cannot read.
//   - It does not decide the comment syntax. It accepts the introducers in
//     MARKER_PATTERN, one of which works in every text format the fork
//     touches. Formats that cannot hold a comment at all (binaries, generated
//     lockfiles) are named in scripts/fork-marker-allowlist.txt with a reason.
//
// Usage:
//   node scripts/fork-marker-guard.mjs
//
// No npm dependencies. Run it in CI (.github/workflows/go.yml, fork-marker-guard
// job) and locally via `make marker-check`.

import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { UPSTREAM_REFS, git, resolveUpstream } from './server-diff-guard.mjs'

// A marker LINE: whitespace, a comment introducer, the token, and a colon.
// The colon is load-bearing. Without it prose *about* the rule matches — which
// is exactly how the loose census over-counted by 24 files. The introducers
// cover:
//   //      Go, TypeScript, Svelte <script>, C
//   #       YAML, shell, .desktop, Makefile
//   <!--    Markdown, HTML, XML, plist
//   /* *    block-comment continuations
//   ;       Inno Setup (.iss)
//   --      Lua, SQL
//   %       TeX, Erlang
//   '       Visual Basic
//
// The introducer group repeats: `;;` is a legal Inno Setup comment, and `**`
// opens a JSDoc block continuation.
export const MARKER_PATTERN =
  /^[ \t]*(?:(?:\/\/|#|<!--|\/\*|\*|;|--|%|')[ \t]*)+OCTO-FORK:/m

// Measured on 2026-09-13 against origin/main (6a9d040b): 324 modified upstream
// files, 68 with a marker line, 6 unable to hold one and named in the
// allowlist — 250 missing. The guard landed at 250 and the 250 markers were
// added in the same PR, so the ceiling is 0: every modified upstream file is
// marked, and a new one that is not fails immediately.
export const MARKER_DEBT_CEILING = 0

// The allowlist covers formats and generators that cannot carry a marker.
// ALLOWLIST_CEILING is a second ratchet: adding an entry is a deliberate act,
// and a file that could have carried a marker must not sneak in to dodge one.
export const ALLOWLIST_PATH = 'scripts/fork-marker-allowlist.txt'
export const ALLOWLIST_CEILING = 6

// ─── pure analyzers (unit-tested) ───────────────────────────────────────────

// parseAllowlist reads `<path>  <reason>` lines. Same contract as
// scripts/homedir-allowlist.txt: blank lines and `#` comments are ignored, and
// every entry must carry a written reason.
export function parseAllowlist(text) {
  const entries = []
  const problems = []
  for (const [index, raw] of text.split('\n').entries()) {
    const line = raw.trim()
    if (line.length === 0 || line.startsWith('#')) continue
    const match = /^(\S+)\s{2,}(.+)$/.exec(line)
    if (!match) {
      problems.push(
        `${ALLOWLIST_PATH}:${index + 1}: expected "<path>  <reason>" with two or more spaces before the reason, got: ${line}`,
      )
      continue
    }
    entries.push({ path: match[1], reason: match[2].trim() })
  }
  return { entries, problems }
}

// analyzeMarkers enforces the ratchet and the anti-no-op property.
//
// `examined` is how many modified upstream files the guard looked at. Zero is
// never good news: it means the upstream ref resolved to HEAD (so the diff is
// empty) or the diff filter stopped matching. Both would otherwise print the
// same success line as a clean fork.
export function analyzeMarkers({
  examined,
  missing,
  ceiling,
  allowlisted = [],
  allowlistCeiling = ALLOWLIST_CEILING,
  allowlistProblems = [],
  misplaced = [],
}) {
  const problems = [...allowlistProblems]
  const notes = []

  if (examined === 0) {
    problems.push(
      `examined 0 modified upstream files — a guard that checked nothing must not report success. ` +
        `Check that the upstream ref is fetched and that it is not the current HEAD.`,
    )
    return { problems, notes }
  }

  if (missing.length > ceiling) {
    problems.push(
      `${missing.length} modified upstream file(s) carry no "OCTO-FORK:" marker (ceiling ${ceiling}). ` +
        `Add the marker at the change, or add the file to ${ALLOWLIST_PATH} with a reason it cannot hold one. ` +
        `Missing: ${missing.slice(0, 10).join(', ')}${missing.length > 10 ? ` … (+${missing.length - 10} more)` : ''}`,
    )
  }

  for (const { file, line } of misplaced) {
    problems.push(
      `${file}:${line}: the marker sits inside a YAML frontmatter block. An HTML comment is not a YAML comment there — ` +
        `yaml.v3 reads it as a mapping entry keyed "\u003c!-- OCTO-FORK", so the file still parses and no test notices, ` +
        `but the metadata gains a junk key. Move the marker after the closing "---".`,
    )
  }

  if (allowlisted.length > allowlistCeiling) {
    problems.push(
      `${ALLOWLIST_PATH} has ${allowlisted.length} entries (ceiling ${allowlistCeiling}). ` +
        `Adding one is a §3.7 stop-and-ask item: a file that can hold a marker must hold one.`,
    )
  }

  notes.push(
    `${examined} modified upstream file(s); ${examined - missing.length} marked, ${missing.length} missing (ceiling ${ceiling}); ` +
      `${allowlisted.length}/${allowlistCeiling} allowlisted`,
  )
  return { problems, notes }
}

// markerInFrontmatter returns the 1-based line of a marker that sits inside a
// leading `---`-delimited block, or null.
//
// WHY THIS IS A RULE AND NOT A NICETY. An HTML comment is not a YAML comment.
// Inserted into a SKILL.md frontmatter, `yaml.v3` reads
// `<!-- OCTO-FORK: why -->` as a mapping entry whose key is `<!-- OCTO-FORK`,
// so the skill still loads, `name` and `description` still parse, and every
// existing test still passes — while the shipped skill metadata carries a junk
// key (measured 2026-09-13 against internal/skills; four default skills hit it).
// Nothing could notice, which is the same failure mode as V-48 itself.
export function markerInFrontmatter(text) {
  const lines = text.split('\n')
  if (lines.length === 0 || lines[0].trim() !== '---') return null
  let closing = -1
  for (let i = 1; i < lines.length; i += 1) {
    if (lines[i].trim() === '---') {
      closing = i
      break
    }
  }
  if (closing === -1) return null
  for (let i = 0; i <= closing; i += 1) {
    if (MARKER_PATTERN.test(lines[i]) && /OCTO-FORK:/.test(lines[i])) return i + 1
  }
  return null
}

// ─── git-backed fact gathering ──────────────────────────────────────────────

// listModifiedUpstreamFiles returns the paths this branch modified relative to
// upstream. Renames are included: a rename of an upstream file is a fork change
// with a merge consequence, and `--name-only` reports the destination path,
// which is the one that must carry the marker.
export function listModifiedUpstreamFiles(root, ref, run = git) {
  const out = run(root, ['diff', '--diff-filter=MR', '-M', '--name-only', ref, 'HEAD'])
  return out.split('\n').filter((l) => l.trim().length > 0)
}

// readMarkerState sorts the files into marked and missing, reading bytes rather
// than lines so a binary file cannot throw.
export function readMarkerState(root, files, read = (p) => fs.readFileSync(p)) {
  const marked = []
  const missing = []
  const misplaced = []
  for (const file of files) {
    let text = ''
    try {
      text = read(path.join(root, file)).toString('utf8')
    } catch {
      // A file we cannot read is reported as missing, not skipped: silently
      // dropping it is how a guard stops covering something.
      missing.push(file)
      continue
    }
    if (!MARKER_PATTERN.test(text)) {
      missing.push(file)
      continue
    }
    marked.push(file)
    const line = markerInFrontmatter(text)
    if (line !== null) misplaced.push({ file, line })
  }
  return { marked, missing, misplaced }
}

// ─── guard ──────────────────────────────────────────────────────────────────

export async function check(root, run = git, read) {
  const problems = []
  const notes = []

  const upstream = resolveUpstream(root, UPSTREAM_REFS, run)
  if (!upstream) {
    return {
      problems: [
        `cannot find an upstream ref (tried ${UPSTREAM_REFS.join(', ')}) — ` +
          `this guard compares the fork against upstream, so it needs one fetched. ` +
          `In CI, check out with fetch-depth: 0 or fetch the branch explicitly.`,
      ],
      notes,
    }
  }

  const allowlistFile = path.join(root, ALLOWLIST_PATH)
  let allowlistText = ''
  try {
    allowlistText = fs.readFileSync(allowlistFile, 'utf8')
  } catch {
    // A missing allowlist is legal: it means nothing is exempt. The count check
    // below still applies, and every modified upstream file must carry a marker.
    allowlistText = ''
  }
  const { entries, problems: allowlistProblems } = parseAllowlist(allowlistText)
  const allowlistedPaths = new Set(entries.map((e) => e.path))

  const files = listModifiedUpstreamFiles(root, upstream, run).filter(
    (f) => !allowlistedPaths.has(f),
  )
  const { missing, misplaced } = readMarkerState(root, files, read)

  // A stale allowlist entry pre-authorises a file that is no longer modified,
  // and more importantly lets a file be exempted *before* it is edited.
  for (const entry of entries) {
    const modified = run(root, ['diff', '--diff-filter=MR', '-M', '--name-only', upstream, 'HEAD', '--', entry.path])
    if (modified.trim().length === 0) {
      problems.push(
        `${ALLOWLIST_PATH}: "${entry.path}" is not a modified upstream file — ` +
          `remove the entry. Exempting a path before it is edited is how an allowlist becomes a bypass.`,
      )
    }
  }

  const result = analyzeMarkers({
    examined: files.length,
    missing,
    ceiling: MARKER_DEBT_CEILING,
    allowlisted: entries,
    allowlistProblems,
    misplaced,
  })
  problems.push(...result.problems)
  notes.push(...result.notes)

  return { problems, notes }
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

async function main() {
  const root = repositoryRoot(import.meta.url)
  const { problems, notes } = await check(root)

  for (const note of notes) console.log(`fork-marker-guard: ${note}`)

  if (problems.length > 0) {
    console.error('fork-marker-guard failed:')
    for (const problem of problems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }
  console.log('fork-marker-guard passed: every modified upstream file carries an OCTO-FORK marker.')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
