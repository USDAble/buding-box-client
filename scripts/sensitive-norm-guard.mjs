// scripts/sensitive-norm-guard.mjs
//
// Guards requirement `D6` / `G4` (需求基线.md): the frontend's copy of the
// normalization symbol table must not drift from the Go one.
//
// `internal/sensitive/normalize.go` is the owner (开发规范 §3.8) — it decides
// which characters a dictionary word drops before matching. The web bundle
// carries a second copy because the dictionary page needs a pre-check
// (duplicate / empty) without a round trip, and the server re-validates
// authoritatively on write. Two copies that must agree, with nothing comparing
// them, is `D-010`: today they are equal, so no test goes red — the drift would
// only appear when someone edits one side, and its symptom (the pre-check
// disagreeing with the verdict) is hard to trace back to a character set.
//
// What this asserts: the two tables drop the SAME SET of characters. What it
// deliberately does not:
//   - ORDER. Both sides test membership (`strings.ContainsRune` /
//     `STRIP_SYMBOLS.includes`), so order is not observable. A guard that
//     failed on a reorder would be enforcing a cosmetic rule.
//   - The rest of the algorithm (lowercasing, whitespace). That is each side's
//     own unit tests' job; comparing it here would mean running TypeScript from
//     Node, which this script has no dependencies for.
//
// Two failure modes get explicit handling, because both would otherwise turn
// this guard into a rubber stamp:
//   - A renamed or removed constant is a FAILURE, not a pass. The guard's whole
//     value is that two tables were compared, and a rename would leave it
//     comparing nothing while reporting success (the `V-49` shape: "a guard
//     counted a symbol that does not exist and announced the target reached").
//   - A declaration that is no longer a list of quoted literals is a reported
//     problem, not a crash. It is also not "skip": a computed table would make
//     "the same characters" unanswerable, so it has to be visible in the output.
//
// The literal list is parsed rather than split on line breaks, so reflowing a
// concatenation is not drift.
//
// Usage:
//   node scripts/sensitive-norm-guard.mjs
//
// No npm dependencies. Run it in CI (.github/workflows/go.yml, the
// sensitive-norm-guard job), locally via `make sensitive-norm-check`, and at
// packaging time via scripts/preflight.mjs.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const GO_REL = 'internal/sensitive/normalize.go'
export const TS_REL = 'web/src/lib/sensitiveDict.ts'
export const GO_CONST = 'stripSymbols'
export const TS_CONST = 'STRIP_SYMBOLS'

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

// extractConcatenation returns { parts, line } for `const <name> = <expr>` where
// <expr> is one or more quoted literals joined by `+`, or null when the
// declaration is absent. Shapes (`gofmt` and `prettier` both produce the first)
// are all accepted:
//
//   const a = "x" +        const a = "x" + "y"        const a = "x"
//             "y"                                     + "y"
//
// It throws when the declaration exists but is not a list of literals — see the
// header on why that must not be silently treated as "nothing to compare".
export function extractConcatenation(source, name) {
  const lines = source.split('\n')
  const pattern = new RegExp(`^\\s*(?:export\\s+)?const\\s+${name}\\s*=`)
  const start = lines.findIndex((line) => pattern.test(line))
  if (start === -1) return null

  const body = lines.slice(start).join('\n')
  let i = body.indexOf('=') + 1
  const parts = []

  const skipSpace = () => {
    while (i < body.length && /\s/.test(body[i])) i++
  }

  for (;;) {
    skipSpace()
    if (body[i] !== '"' && body[i] !== "'") {
      const what = parts.length === 0 ? 'is not a list of string literals' : 'has a "+" with no literal after it'
      throw new Error(`${name} ${what}`)
    }
    const literal = scanLiteral(body, i)
    parts.push(literal.value)
    i = literal.end
    skipSpace()
    if (body[i] !== '+') break
    i++
  }
  return { value: parts.join(''), parts, line: start + 1 }
}

// scanLiteral reads one quoted literal starting at `start` and returns its value
// and the index just past it. Go and JavaScript agree on the escapes that
// appear in these tables; anything else throws rather than guessing.
export function scanLiteral(text, start) {
  const quote = text[start]
  const escapes = { '\\': '\\', "'": "'", '"': '"', n: '\n', t: '\t', r: '\r' }
  let value = ''
  for (let i = start + 1; i < text.length; i++) {
    if (text[i] === '\\') {
      i++
      const esc = text[i]
      if (!(esc in escapes)) throw new Error(`unsupported escape \\${esc} in ${JSON.stringify(text.slice(start, i + 1))}`)
      value += escapes[esc]
      continue
    }
    if (text[i] === quote) return { value, end: i + 1 }
    if (text[i] === '\n') break
    value += text[i]
  }
  throw new Error(`unterminated string literal ${JSON.stringify(text.slice(start, start + 20))}`)
}

// readTable loads and evaluates one side's table. Returns { value, line }, or
// { problem } describing why the table could not be read.
export async function readTable(root, rel, name) {
  let source
  try {
    source = await fs.readFile(path.join(root, rel), 'utf8')
  } catch (error) {
    if (error?.code === 'ENOENT') return { problem: `${rel}: file not found` }
    throw error
  }

  let found
  try {
    found = extractConcatenation(source, name)
  } catch (error) {
    return { problem: `${rel}: ${error.message} — the guard compares two tables by value (see D-010)` }
  }
  if (found === null) {
    return {
      problem:
        `${rel}: no \`const ${name} = <string literals>\` — the guard compares two tables by ` +
        'name, so a rename here would leave it comparing nothing (see D-010)',
    }
  }
  return { value: found.value, line: found.line }
}

/** The distinct characters a table drops, as a Set of single-code-point strings. */
export function charSet(table) {
  return new Set([...table])
}

/** Characters present in `a` but not in `b`, in `a`'s order. */
export function missingFrom(a, b) {
  return [...a].filter((ch) => !b.has(ch))
}

// check returns a list of human-readable problems; an empty list means clean.
export async function check(root) {
  const go = await readTable(root, GO_REL, GO_CONST)
  const ts = await readTable(root, TS_REL, TS_CONST)

  const problems = []
  if (go.problem) problems.push(go.problem)
  if (ts.problem) problems.push(ts.problem)
  if (problems.length > 0) return problems

  const goSet = charSet(go.value)
  const tsSet = charSet(ts.value)
  const onlyGo = missingFrom(goSet, tsSet)
  const onlyTs = missingFrom(tsSet, goSet)

  if (onlyGo.length > 0) {
    problems.push(
      `${TS_REL}:${ts.line} (${TS_CONST}) does not drop ${JSON.stringify(onlyGo.join(''))}, ` +
        `which ${GO_REL}:${go.line} (${GO_CONST}) does — the client pre-check and the ` +
        'server verdict disagree on these characters',
    )
  }
  if (onlyTs.length > 0) {
    problems.push(
      `${TS_REL}:${ts.line} (${TS_CONST}) drops ${JSON.stringify(onlyTs.join(''))}, which ` +
        `${GO_REL}:${go.line} (${GO_CONST}) does not — the client pre-check would report a ` +
        'word as present that the server does not match',
    )
  }
  return problems
}

async function main() {
  const root = repositoryRoot(import.meta.url)
  const problems = await check(root)
  if (problems.length > 0) {
    console.error('sensitive-norm-guard failed:')
    for (const problem of problems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }
  const go = await readTable(root, GO_REL, GO_CONST)
  console.log(
    `sensitive-norm-guard passed: ${GO_REL} and ${TS_REL} drop the same ` +
      `${charSet(go.value).size} characters.`,
  )
}

// Only run the CLI when invoked directly, so importing check/readTable for a
// test is free of side effects.
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
