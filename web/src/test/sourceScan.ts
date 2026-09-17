// Helpers for the tests that nail a *second source* rather than a behaviour.
//
// Some failures are not "a function returns the wrong thing" but "a second place
// grew its own copy" — a second language source, a second consumer of an event,
// a re-inlined handler. No behavioural test catches that, because refactoring a
// working path into two copies keeps every assertion green. Those nails scan the
// tree instead, so they need two things the behavioural tests do not: the file
// list, and comments stripped (prose may name the thing being forbidden — that
// is how the reasoning stays in the tree — without being a second read of it).
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { extname, join } from 'node:path'

// vitest's root is web/, so this resolves to web/src whichever test file calls it.
export const SRC = join(process.cwd(), 'src')

/** Every shipped .ts/.svelte file under dir, tests excluded. */
export function sourceFiles(dir: string = SRC): string[] {
  const out: string[] = []
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) {
      out.push(...sourceFiles(path))
      continue
    }
    if (!['.svelte', '.ts'].includes(extname(name))) continue
    // Sibling tests describe behaviour, they are not shipped code.
    if (name.endsWith('.test.ts')) continue
    out.push(path)
  }
  return out
}

/**
 * stripComments removes line and block comments without trying to be a parser:
 * it only needs to be good enough that a comment cannot hide a read or fake one.
 */
export function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\/\/[^\n]*/g, '')
}

/** The files whose shipped code mentions needle, as paths relative to src/. */
export function filesMentioning(needle: string, dir: string = SRC): string[] {
  return sourceFiles(dir)
    .filter((path) => stripComments(readFileSync(path, 'utf8')).includes(needle))
    .map((path) => path.slice(dir.length + 1))
}
