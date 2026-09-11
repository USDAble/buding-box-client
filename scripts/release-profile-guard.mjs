// scripts/release-profile-guard.mjs
//
// Guards the one rule that decides whether a packaged artifact is a real
// product or an accidental developer build: every build of cmd/octo-desktop
// that produces a **shipped** artifact must carry the `product_production`
// build tag (dev-docs-usdable/运行时Profile配置.md §2).
//
// Why this needs a guard rather than review: the default branch of
// internal/productprofile/profile_developer.go is `!product_production`, so a
// release pipeline that forgets the tag still compiles, still passes every
// test, and still produces a working binary — it just silently ships a
// developer package where OCTO_DESKTOP_DEV_URL, environment provider keys and
// OCTO_DATA_ROOT are all honoured. Nothing at runtime can tell you the tag was
// missing. That is exactly the failure this file catches, and it already
// happened once: package-portable.mjs, release.yml and the desktop.yml matrix
// did not agree on the tag set.
//
// Scanned: the files that build a distributed desktop binary. Every `go build`
// invocation there must name `product_production` (directly or via the
// BUILD_TAGS constant). Two escapes exist, both explicit:
//
//   - `release-profile-guard:allow` on the line — for a deliberate exception.
//   - a CLI build (`./cmd/octo`) — the CLI carries no product profile, so the
//     tag is meaningless there. Only the desktop module is governed.
//
// Deliberately NOT scanned: the Makefile's `desktop` / `desktop-binary` /
// `desktop-dev` targets. Those are local developer builds and must stay
// untagged so the dev loop keeps working; the packaging targets
// (`desktop-app`, `desktop-appimage`, `desktop-portable`) delegate to the
// scanned scripts below. `.goreleaser.yaml` is also out of scope: it builds
// `./cmd/octo`, not the desktop shell.
//
// Usage:
//   node scripts/release-profile-guard.mjs
//
// No npm dependencies. Run it in CI (see .github/workflows/go.yml, the
// release-profile-guard job) and locally via `make release-profile-check`.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// Every file that builds a desktop binary we actually hand to a user.
export const SITES = [
  {
    file: 'scripts/package-portable.mjs',
    label: 'portable Windows package (make desktop-portable, portable.yml)',
  },
  {
    file: 'scripts/package-desktop-macos.sh',
    label: 'macOS .app (make desktop-app / desktop-portable-all, portable.yml)',
  },
  {
    file: 'scripts/package-desktop-linux.sh',
    label: 'Linux AppImage (make desktop-appimage)',
  },
  {
    file: '.github/workflows/release.yml',
    label: 'Windows installer release artifact',
  },
  {
    file: '.github/workflows/desktop.yml',
    label: 'CI desktop GUI exe artifact',
  },
  {
    file: '.github/workflows/windows-installer-check.yml',
    label: 'installer check compile',
  },
]

export const PROFILE_TAG = 'product_production'
// The indirection used by package-portable.mjs; accepted in place of the literal.
export const TAGS_CONST = 'BUILD_TAGS'
// A build of the CLI carries no product profile, so it is out of scope. The
// negative lookahead keeps `./cmd/octo-desktop` from matching.
const CLI_TARGET = /\.\/cmd\/octo(?!-desktop)/
const ALLOW_MARKER = 'release-profile-guard:allow'
// Two shapes: a shell/workflow `go build …`, or the Node array form
// execFileSync('go', ['build', …]).
const BUILD_INVOCATION = /\bgo build\b|(['"])go\1\s*,\s*\[\s*(['"])build\2/

// buildLines returns the lines of `content` that invoke `go build`, skipping
// comment-only lines. It is intentionally line-based: in every scanned file the
// invocation is a single line, so a hit maps straight to a line number a human
// can edit.
export function buildLines(content) {
  const hits = []
  const lines = content.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const text = lines[i]
    const trimmed = text.trim()
    if (trimmed.startsWith('#') || trimmed.startsWith('//')) continue
    if (!BUILD_INVOCATION.test(text)) continue
    hits.push({ line: i + 1, text })
  }
  return hits
}

// checkSource reports the problems in one scanned file, given its content and
// repo-relative path. Pure, so a test can drive it with synthetic input.
export function checkSource(rel, content) {
  const problems = []
  for (const { line, text } of buildLines(content)) {
    if (CLI_TARGET.test(text)) continue
    if (text.includes(ALLOW_MARKER)) continue
    if (!text.includes(PROFILE_TAG) && !text.includes(TAGS_CONST)) {
      problems.push(`${rel}:${line}: desktop build without ${PROFILE_TAG} — would ship a developer package`)
    }
  }
  // A file that defers to BUILD_TAGS must actually define it with the tag.
  if (content.includes(TAGS_CONST) && !new RegExp(`${TAGS_CONST}\\s*=\\s*['"\`][^'"\`]*${PROFILE_TAG}`).test(content)) {
    problems.push(`${rel}: uses ${TAGS_CONST} but its definition does not include ${PROFILE_TAG}`)
  }
  return problems
}

export async function check(root) {
  const problems = []
  for (const site of SITES) {
    let content
    try {
      content = await fs.readFile(path.join(root, site.file), 'utf8')
    } catch (error) {
      if (error?.code === 'ENOENT') {
        problems.push(`${site.file}: missing — expected ${site.label}`)
        continue
      }
      throw error
    }
    const sourceProblems = checkSource(site.file, content)
    if (sourceProblems.length === 0 && buildLines(content).length === 0) {
      // A site that no longer builds anything is itself drift worth reporting.
      problems.push(`${site.file}: no \`go build\` found — did ${site.label} move?`)
      continue
    }
    problems.push(...sourceProblems)
  }
  return problems
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

async function main() {
  const problems = await check(repositoryRoot(import.meta.url))
  if (problems.length > 0) {
    console.error('release-profile-guard failed:')
    for (const problem of problems) console.error(`- ${problem}`)
    process.exitCode = 1
    return
  }
  console.log(`release-profile-guard passed: every shipped desktop build carries ${PROFILE_TAG}.`)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
