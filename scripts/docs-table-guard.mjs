// scripts/docs-table-guard.mjs
//
// Guards the *container* of every Markdown table in this fork's docs: a row
// whose cell count does not match its header cannot be read as what it says,
// because the renderer silently shifts the columns.
//
// Why this exists (V-56 → V-59). The fork's registers carry their facts in
// tables, and every one of the following was a *container* defect, not a
// content defect — the sentences were still true and the readers still could
// not see them:
//
//   * 需求基线.md §6 had two revision records on one line (a missing newline
//     while appending), so V-46's row rendered as a four-column row in a
//     two-column table and **the record was invisible**.
//   * 需求基线.md §5.6 had four rows whose cell count was wrong: V-6 had a
//     bare `|` where a `（` belonged, V-19 had two cells merged into one,
//     V-31 had a stray empty cell, V-56 (the row registering *this* class of
//     bug) quoted the offending `|` without escaping it.
//   * 开发计划.md quoted the Go selector regex `Gateway|Ordinary|Refusal`
//     unescaped, and the row became four columns.
//
// The measured rate is what justifies a parser: one audit on 2026-09-14 found
// six instances, and one of them was written *while registering the defect* —
// Markdown requires `\|` inside a table cell, and the person writing about
// pipes is the person holding the pipe. 4 of the 55 rows in the register were
// malformed and had been for days. Wiring this guard then surfaced eight more
// that no audit had found (see the commit that added it): a `-tool=terminal|
// read_file` flag in a code span, a `network_unavailable|upstream_unavailable`
// alternation, four rows mid-table, and one row that was simply a cell short.
//
// Why a whole-line regex cannot do this (V-52's finding, seconded by V-56).
// Searching for "a row that does not match the expected shape" gives false
// green: the L-C3b row's own prose contains the string 「标 ✅ 是错的」, which a
// line-shaped pattern reads as evidence that the row is in sync. The unit of
// truth is the **cell**, which means splitting on unescaped pipes — so that is
// what this guard does. It asserts nothing about whether a table's *content*
// is up to date; V-52 and V-56 both concluded that a content-sync guard needs
// per-column semantics, and that the honest place for those judgements is a
// human reading the row. Container integrity is mechanical; content is not.
//
// One assertion only. A single rule covers both diagnosed shapes, because a
// concatenation brings the second record's own pipes with it: "two revision
// records on one line" shows up as a two-column table row with four cells.
//
// A rejected candidate is worth recording. Counting ISO dates inside a cell was
// tried first, on the theory that a joined record repeats its date — it fired
// twelve times on this tree and every one was legitimate (a register row
// routinely says "2026-09-11 补… 2026-09-13 再补"). That is a heuristic about
// *content* wearing a container's clothes, and a guard whose criterion is wrong
// is worse than no guard: it teaches the next reader to add an exemption.
//
// Scope (开发规范 §3.10). `dev-docs-usdable/` only — this fork's own
// documents, which is where the class was observed. Upstream's `dev-docs/` is
// deliberately excluded: a malformed table there is upstream's to fix, and
// failing on it would make every upstream merge look like a regression here.
//
// Usage:
//   node scripts/docs-table-guard.mjs
//
// No npm dependencies. Run it in CI (.github/workflows/go.yml, docs-table-guard
// job) and locally via `make docs-table-check`.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

/** Directories scanned, relative to the repository root. */
export const SCAN_ROOTS = ['dev-docs-usdable']

/** A cell that is only dashes (optionally colon-anchored) is the delimiter row. */
const DELIMITER_CELL = /^:?-+:?$/

// ─── pure analyzers (unit-tested) ───────────────────────────────────────────

// isEscaped reports whether the backslash run ending at `i - 1` is odd, which
// is what makes the following character literal. `\|` is a literal pipe, `\\|`
// is a literal backslash followed by a delimiter.
function isEscaped(line, i) {
  let backslashes = 0
  for (let j = i - 1; j >= 0 && line[j] === '\\'; j--) backslashes++
  return backslashes % 2 === 1
}

/**
 * splitRow splits one table row into its cells on unescaped pipes.
 *
 * Returns null when `line` is not a table row at all (GFM requires the row to
 * be delimited by pipes). The returned strings are trimmed and keep any
 * escaping, so a caller can still see the original text.
 */
export function splitRow(line) {
  const trimmed = line.trim()
  if (!trimmed.startsWith('|')) return null
  const cells = []
  let current = ''
  for (let i = 1; i < trimmed.length; i++) {
    const ch = trimmed[i]
    if (ch === '|' && !isEscaped(trimmed, i)) {
      cells.push(current)
      current = ''
      continue
    }
    current += ch
  }
  // A row written with a closing pipe leaves `current` empty. Anything left is
  // the last cell of a row written without one, which GFM tolerates.
  if (current.trim().length > 0) cells.push(current)
  return cells.map((cell) => cell.trim())
}

// isDelimiterRow reports whether every cell of a row is a delimiter cell, which
// is what distinguishes a table header from a body row.
export function isDelimiterRow(cells) {
  return cells !== null && cells.length > 0 && cells.every((cell) => DELIMITER_CELL.test(cell))
}

/**
 * checkDocument reports the container defects in one Markdown document.
 *
 * Returns `{ problems, tables }`; `tables` is the number of tables with a
 * recognised header, which the caller uses to refuse to pass vacuously.
 */
export function checkDocument(rel, content) {
  const problems = []
  let tables = 0
  const lines = content.split('\n')

  let inFence = false
  let headerCount = null

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const lineNo = i + 1

    // Fenced code blocks may contain anything, including table-shaped text.
    if (/^\s*(```|~~~)/.test(line)) {
      inFence = !inFence
      continue
    }
    if (inFence) continue

    const cells = splitRow(line)

    if (cells === null) {
      // A table ends at the first line that is not a row.
      headerCount = null
      continue
    }

    if (isDelimiterRow(cells)) {
      // The delimiter row belongs to the header immediately above it.
      continue
    }

    if (headerCount === null) {
      // Either a header (when the next line delimits it) or a lone row.
      const next = splitRow(lines[i + 1] ?? '')
      if (isDelimiterRow(next)) {
        headerCount = cells.length
        tables++
        // GFM takes the column count from the delimiter row, so a delimiter
        // that disagrees with the header silently drops the header's extra
        // columns — a whole column of the document, gone.
        if (next.length !== cells.length) {
          problems.push(
            `${rel}:${lineNo + 1}: delimiter row has ${next.length} cell(s), header has ` +
              `${cells.length}. GFM takes the column count from the delimiter row, so the ` +
              `header's extra column is dropped.`,
          )
        }
      }
      continue
    }

    if (cells.length !== headerCount) {
      problems.push(
        `${rel}:${lineNo}: row has ${cells.length} cell(s), header has ${headerCount}. ` +
          `A row that does not match its header renders as shifted columns — escape any ` +
          `literal pipe in the cell as \\| , or split the row if two records were joined.`,
      )
    }
  }

  return { problems, tables }
}

// ─── filesystem-backed scan ─────────────────────────────────────────────────

async function markdownFilesIn(root, dir) {
  const abs = path.join(root, dir)
  const found = []
  let entries
  try {
    entries = await fs.readdir(abs, { withFileTypes: true })
  } catch (error) {
    if (error?.code === 'ENOENT') return found
    throw error
  }
  for (const entry of entries) {
    const rel = path.posix.join(dir, entry.name)
    if (entry.isDirectory()) {
      found.push(...(await markdownFilesIn(root, rel)))
      continue
    }
    if (entry.isFile() && entry.name.endsWith('.md')) found.push(rel)
  }
  return found
}

export async function check(root) {
  const problems = []
  const notes = []
  let files = 0
  let tables = 0

  for (const dir of SCAN_ROOTS) {
    for (const rel of await markdownFilesIn(root, dir)) {
      files++
      const content = await fs.readFile(path.join(root, rel), 'utf8')
      const result = checkDocument(rel, content)
      problems.push(...result.problems)
      tables += result.tables
    }
  }

  // A guard that examined nothing is not a passing guard. Both numbers matter:
  // the file count catches a wrong SCAN_ROOTS, the table count catches a parser
  // that stopped recognising tables (which reports zero problems for every
  // input, forever, while looking green) — the failure mode fork-marker-guard
  // records as "examining zero files is a failure, not a pass".
  if (files === 0) {
    problems.push(`${SCAN_ROOTS.join(', ')}: no Markdown files found — the guard examined nothing.`)
  } else if (tables === 0) {
    problems.push(
      `${files} Markdown file(s) scanned but no table headers recognised — ` +
        `the guard examined no table, which is a parser failure, not a clean tree.`,
    )
  } else {
    notes.push(`${files} file(s), ${tables} table(s) checked for container integrity.`)
  }

  return { problems, notes }
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

async function main() {
  const root = repositoryRoot(import.meta.url)
  const { problems, notes } = await check(root)

  for (const note of notes) console.log(`docs-table-guard: ${note}`)

  if (problems.length > 0) {
    console.error('docs-table-guard failed:')
    for (const problem of problems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }

  console.log('docs-table-guard passed: every table row matches its header.')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
