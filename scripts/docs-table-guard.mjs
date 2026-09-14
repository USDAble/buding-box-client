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
// Three assertions: cell counts, a row that lost its table, and a step that lost
// its section. The second
// exists because the first could be bypassed: a blank line inside a table ends
// the table for the parser (and for GFM), so every row below it is compared
// against nothing and passes. That is exactly how V-64 / V-65 / V-66 sat in
// 需求基线 §5.6 unchecked while this guard reported a clean tree — 50-odd rows
// were being rendered as a paragraph of pipe text, and the guard said nothing.
// Reported as *one* defect per row so a cut of N rows names all N.
//
// The row-fragment rule is deliberately narrow: a line that starts with `|` and
// is neither a delimiter row nor the header of the table below it. A single-row
// table written without a header is therefore reported too — and it should be,
// because that is how GFM renders it: not as a table.
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

/** A heading line, capturing its level so a step can be told from a section. */
const HEADING = /^(#{1,6})\s+(.*)$/

/**
 * A "step" heading is 开发计划.md's per-PR skeleton — `#### 第 0 步 · …`. Those
 * four headings belong to exactly one `###` section, and the way a section
 * loses its own heading is that the steps stay where they are.
 */
const STEP = /^第\s*\d+\s*步(?![0-9])/

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
      // Either a header (when the next line delimits it) or a row fragment.
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
      } else {
        problems.push(
          `${rel}:${lineNo}: row with no table above it (${cells.length} cell(s)). A blank ` +
            `line inside a table ends it: this row and the ones after it are rendered as ` +
            `text, not as a table, and are never compared against a header. Delete the ` +
            `blank line, or move the row into its table.`,
        )
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

/**
 * checkHeadingStructure reports a step that lost the section it belongs to.
 *
 * Why this is a *container* defect of the same family as the table rules above,
 * and why the tables rule could not see it (V-74). 开发计划.md keeps one
 * `###` section per PR, each carrying the same skeleton of `#### 第 N 步`
 * headings. When a new section is inserted by replacing a heading line, the
 * steps stay where they are, so the new section's 第 0–3 步 nest under the
 * previous PR's `###` and read as part of it. Nothing is false — every sentence
 * is still true — and nothing is caught: a reader attributes the steps to the
 * wrong change, and the writer of the *next* window hunts for an anchor that is
 * not there.
 *
 * The measurement that justified a check: by 2026-09-14 this had happened
 * twice in this one file, once as a section-*order* inversion (`1.2.4` below
 * `1.2.5`, caught by eye) and once as `L-A5`'s whole 第 0–3 步 nested under
 * `PR-6a` — committed, and visible only from the table of contents.
 *
 * The criterion is "one `###` section carries at most one 第 0 步", and it was
 * chosen after a wrong one. The first attempt asked whether the nearest
 * non-step heading above a step was shallower than `####`; that fired six
 * times and five were legitimate layouts (a section may put
 * `#### 落地结果` *before* the original 第 0–2 步 it landed). A criterion that
 * reports correct documents teaches the next reader to add an exemption, so it
 * was replaced rather than tolerated. This one is narrow and says so: a new
 * section whose plan opens somewhere other than 第 0 步 would slip through.
 * Catching that needs content, and the honest place for content is a human
 * reading the section — the same conclusion V-52 and V-56 reached about tables.
 */
export function checkHeadingStructure(rel, content) {
  const problems = []
  const lines = content.split('\n')

  let inFence = false
  let section = null // the nearest heading that is not itself a step
  let zeros = 0 // 第 0 步 headings seen under the current section
  let steps = 0

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]

    if (/^\s*(```|~~~)/.test(line)) {
      inFence = !inFence
      continue
    }
    if (inFence) continue

    const m = HEADING.exec(line)
    if (!m) continue

    const level = m[1].length
    const text = m[2].trim()

    if (!STEP.test(text)) {
      section = { level, lineNo: i + 1, text }
      zeros = 0
      continue
    }

    steps++
    if (!text.startsWith('第 0 步')) continue

    zeros++
    if (zeros === 1) {
      if (section === null) {
        problems.push(
          `${rel}:${i + 1}: step heading "${text}" has no heading above it at all. ` +
            `Its steps belong to no section.`,
        )
      }
      continue
    }

    problems.push(
      `${rel}:${i + 1}: a second "第 0 步" under the same \`###\` section ` +
        `(line ${section.lineNo}: "${section.text}"). Steps read as belonging to that ` +
        `section: give the new section its own \`###\` heading instead of letting its ` +
        `skeleton nest under the one before it.`,
    )
  }

  return { problems, steps }
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
  let steps = 0

  for (const dir of SCAN_ROOTS) {
    for (const rel of await markdownFilesIn(root, dir)) {
      files++
      const content = await fs.readFile(path.join(root, rel), 'utf8')
      const result = checkDocument(rel, content)
      problems.push(...result.problems)
      tables += result.tables
      const structure = checkHeadingStructure(rel, content)
      problems.push(...structure.problems)
      steps += structure.steps
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
  } else if (steps === 0) {
    // Same argument one level up: the step rule would report nothing, forever,
    // if 开发计划.md's skeleton were renamed — and green-while-blind is the
    // failure this guard exists to refuse.
    problems.push(
      `${files} Markdown file(s) scanned but no step heading recognised — ` +
        `the guard examined no \`#### 第 N 步\`, which is a parser failure, not a clean tree.`,
    )
  } else {
    notes.push(
      `${files} file(s), ${tables} table(s) checked for container integrity, ` +
        `${steps} step heading(s) checked for a section above them.`,
    )
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

  console.log('docs-table-guard passed: every table row matches its header, and every step has a section.')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
