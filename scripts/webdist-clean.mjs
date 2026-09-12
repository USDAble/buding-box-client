#!/usr/bin/env node
// Empty internal/server/webdist/ of everything except the tracked .gitkeep,
// before the web build writes into it.
//
// WHY THIS EXISTS (V-34). web/vite.config.ts sets `emptyOutDir: false`, and the
// reason recorded there is real: this Vite (checked) preserves only ".git" when
// it empties a directory, so `true` would delete the tracked
// internal/server/webdist/.gitkeep and leave the git tree dirty — and
// goreleaser refuses to release from a dirty tree (it broke the v1.12.22 tag).
//
// But the sentence that made that acceptable — "stale hashed assets left behind
// are inert: index.html only references the fresh ones" — holds only for
// content-hashed names. A fixed-name file stays addressable and stays shipped:
// the two upstream favicon.svg files were deleted from web/public/ in the
// branding commit, yet a machine that had built before them still carried the
// old icon into the binary, because `go:embed all:webdist` embeds whatever is
// on disk. Nothing cleaned it: not vite, not `make clean`.
//
// So the rule this script implements is the one vite.config.ts cannot: the
// directory's contents are a function of the sources, not of build history.
// Deleting the hashed assets too is deliberate, not collateral — they are
// unreferenced dead bytes that would otherwise accumulate in every binary.
//
// WHY .gitkeep SURVIVES. `//go:embed all:webdist` fails on a fresh clone if the
// directory has no files at all, so the sentinel is tracked; deleting it would
// recreate the dirty-tree failure this script exists to avoid. It is the single
// exception, and it is named here rather than hidden in a find(1) expression.

import fs from 'node:fs/promises'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

/** The only entry a clean leaves behind, with the reason above. */
export const SURVIVORS = ['.gitkeep']

/** Where the web build writes, relative to the repository root. */
export const DEFAULT_DIR = 'internal/server/webdist'

/**
 * staleEntries returns the entries of a directory listing that a clean removes.
 *
 * Pure so the rule can be asserted without touching a filesystem: the test that
 * matters is "a stray favicon.svg is stale", and the test that protects the
 * release is ".gitkeep is not".
 */
export function staleEntries(names) {
  return names.filter((name) => !SURVIVORS.includes(name))
}

/** Removes every stale entry from dir. A missing dir is not an error. */
export async function clean(dir) {
  let names
  try {
    names = await fs.readdir(dir)
  } catch (err) {
    if (err.code === 'ENOENT') return []
    throw err
  }
  const stale = staleEntries(names)
  for (const name of stale) {
    await fs.rm(path.join(dir, name), { recursive: true, force: true })
  }
  return stale
}

async function main() {
  const dir = process.argv[2] ?? DEFAULT_DIR
  const removed = await clean(dir)
  // Silent when there was nothing to remove: this runs on every web build, and
  // a line of noise per build trains people to ignore the output.
  if (removed.length) {
    console.log(`webdist-clean: removed ${removed.length} stale entr${removed.length === 1 ? 'y' : 'ies'} from ${dir}`)
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main()
}
