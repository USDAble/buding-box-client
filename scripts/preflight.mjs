// scripts/preflight.mjs
//
// Runs the fork guards at the START of a packaging run, so a shipped artifact
// cannot be produced from a tree the guards would reject.
//
// Why this exists separately from CI: the guards already run as their own CI
// jobs, but CI runs them on a *commit*. Packaging is a later, different act —
// it can be started from a dirty tree, from a stale checkout, or from a local
// `make desktop-portable` where the developer skipped the CI jobs entirely.
// The failure that motivates this file is concrete: release-profile-guard
// exists because three packaging paths once disagreed about the
// `product_production` tag, and each still produced a *working* binary. A
// build that silently ships a developer package is exactly the class of
// mistake that has to be stopped at the build, not only at the PR.
//
// Two tiers, because the guards do not all have the same input requirements:
//
//   HARD (fail the build)
//     datapath-guard          source has no ".octo"/home-dir violations
//     release-profile-guard    this build carries product_production
//     reuse-guard              no fork package re-implements a provider
//     norms-guard              every AI-tool entry point points at the fork spec
//     agents-guard             AGENTS.md matches .octorules and still carries
//                              the fork's three hard rules
//     All five are pure source scans with no external refs.
//
//   ADVISORY (warn, do not fail)
//     release-config-guard     the embedded production profile's values are real
//     server-diff-guard        fork drift vs the upstream-tracking branch
//
//     release-config-guard is advisory because packaging a build whose
//     control-plane host is not set yet is legitimate during B0/B1 and for
//     internal test packages; the profile is structurally valid (Go enforces
//     non-empty https hosts and ed25519 keys) but the host may still be the
//     RFC 6761 `.invalid` placeholder, which cannot resolve — so nothing is
//     sent anywhere, and the build log is the right place to say so.
//
//     server-diff-guard needs an `origin/main` (or `main`) ref to diff
//     against. A packaging host may legitimately lack it — a shallow clone, a
//     machine that only ever fetched one branch, a release runner checking out
//     a tag. Blocking packaging on "you have not fetched main" would push
//     people to disable the check, which is worse than warning. When the ref
//     IS present the result is reported either way, so real drift is still
//     visible in the build log.
//
// Usage:
//   node scripts/preflight.mjs
//
// Called from: scripts/package-portable.mjs (main), scripts/package-desktop-macos.sh,
// scripts/package-desktop-linux.sh. No npm dependencies.

import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { check as checkDatapath } from './datapath-guard.mjs'
import { check as checkReleaseProfile } from './release-profile-guard.mjs'
import { check as checkReuse } from './reuse-guard.mjs'
import { check as checkNorms } from './norms-guard.mjs'
import { check as checkAgents } from './sync-agents.mjs'
import { check as checkServerDiff, resolveUpstream } from './server-diff-guard.mjs'
import { check as checkReleaseConfig } from './release-config-guard.mjs'

// HARD: returns a list of problem strings. Every entry is prefixed with its
// guard name so a build log shows which rule stopped the build.
export async function runHardChecks(root) {
  const problems = []

  const datapath = await checkDatapath(root)
  for (const p of datapath) problems.push(`datapath-guard: ${p}`)

  const profile = await checkReleaseProfile(root)
  for (const p of profile) problems.push(`release-profile-guard: ${p}`)

  const { problems: reuse } = await checkReuse(root)
  for (const p of reuse) problems.push(`reuse-guard: ${p}`)

  const { problems: norms } = await checkNorms(root)
  for (const p of norms) problems.push(`norms-guard: ${p}`)

  const { problems: agents } = await checkAgents(root)
  for (const p of agents) problems.push(`agents-guard: ${p}`)

  return problems
}

// ADVISORY: returns { warnings, notes }. Never fails the build.
export async function runAdvisoryChecks(root, { resolve = resolveUpstream } = {}) {
  const warnings = []
  const notes = []

  // The production profile is structurally valid (Go enforces the shape) but
  // may still carry `.invalid` placeholders, which resolve on no DNS. Packaging
  // such a build is legitimate during B0/B1, so this warns rather than fails;
  // the runtime is safe because `.invalid` cannot reach anything.
  const releaseConfig = await checkReleaseConfig(root)
  for (const p of releaseConfig) warnings.push(`release-config-guard: ${p}`)

  if (!resolve(root)) {
    warnings.push(
      'server-diff-guard: no upstream ref (origin/main or main) is fetched — ' +
        'fork-drift vs upstream was NOT verified for this build. ' +
        'Run `git fetch origin main` and re-package if you want that check.',
    )
    return { warnings, notes }
  }

  const { problems, notes: diffNotes } = await checkServerDiff(root)
  for (const p of problems) warnings.push(`server-diff-guard: ${p}`)
  notes.push(...diffNotes)

  return { warnings, notes }
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

// runPreflight is the entry point the packaging scripts call. It returns
// { failureCount, warningCount } rather than throwing or exiting, so each
// packaging script decides how to report and whether to abort — the portable
// builder wraps it in its own output style, the shell scripts just exit.
//
// `hardChecks` is injectable so the failure path can be tested without
// synthesising a whole fake repository on disk; production callers omit it.
export async function runPreflight(root, { log = console, hardChecks = runHardChecks } = {}) {
  log.log('==> 出包前置检查 (fork guards)')

  const hard = await hardChecks(root)
  const { warnings, notes } = await runAdvisoryChecks(root)

  for (const note of notes) log.log(`    ${note}`)

  if (warnings.length > 0) {
    for (const w of warnings) log.warn(`    警告: ${w}`)
  }

  if (hard.length > 0) {
    log.error('出包前置检查失败 —— 拒绝构建：')
    for (const p of hard) log.error(`  - ${p}`)
    return { failureCount: hard.length, warningCount: warnings.length }
  }

  log.log('    通过。')
  return { failureCount: 0, warningCount: warnings.length }
}

async function main() {
  const { failureCount } = await runPreflight(repositoryRoot(import.meta.url))
  if (failureCount > 0) process.exitCode = 1
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
