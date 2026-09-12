// scripts/release-config-guard.mjs
//
// Checks the *content* of the embedded production profile. The Go side
// (`internal/productprofile`) already enforces the shape — non-empty hosts,
// https, well-formed ed25519 keys — and a malformed asset panics the launch.
// What Go cannot tell you is whether the values are *real*: a release that
// still carries the `.invalid` placeholders validates perfectly and then fails
// every control-plane call at runtime, because `.invalid` never resolves.
//
// That is the "present but not replaced" failure this file catches. It is the
// release-time half of the same rule: P0-01 §1 says the production host is a
// compile-time constant, and someone has to compile the right one in.
//
// ADVISORY, deliberately. Packaging a build with an unset host is legitimate
// while the deployment host does not exist yet (B0/B1 development, internal
// test packages). Blocking those builds would push people to disable the
// check, which is worse than a loud warning — the same reasoning that puts
// server-diff-guard in the advisory tier of preflight.mjs. The runtime is safe
// regardless: `.invalid` cannot resolve, so nothing is sent anywhere.
//
// No npm dependencies. Wired into scripts/preflight.mjs (advisory tier) and
// runnable on its own via `make release-config-check`.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// Repo-relative path of the asset compiled into every production desktop build.
export const PRODUCTION_ASSET = 'internal/productprofile/profiles/production.json'

// RFC 6761 reserves `.invalid`: it can never resolve, which is what makes a
// placeholder safe to ship by accident and detectable here.
const PLACEHOLDER = /\.invalid(\/|$)/

const ALLOW_FLAGS = ['allowDevWebview', 'allowEnvironmentModelSource', 'allowDataRootOverride']

// Only the control-plane host has a versioning requirement, and it is a real
// one: internal/productclient concatenates its own paths onto apiHost, so a host
// without /v1 sends every call to /v1/v1/… — that was V-13, a manual-integration
// find that 175 green unit tests missed because both harnesses passed a bare
// origin the profile never produces.
//
// The gateway host carries NO such requirement, and an earlier version of this
// guard asserted one for both fields ("should be versioned (/v1)"). That rule
// was V-37 — a false alarm raised while planning PR-5a, which read
// internal/provider/openai's ChatCompletionsPath constant, concluded a /v1 on the
// host would be appended to rather than normalised, and missed the normalisation
// branch twenty lines below it. The provider trims a trailing /v1 and upstream's
// own client_test.go pins that ("must not contain /v1/v1"), so BOTH gateway host
// shapes are correct and neither can produce the double prefix.
//
// The rule is therefore dropped rather than inverted: a guard that demands a
// particular shape for the gateway host would be enforcing a preference as if it
// were a contract, and its advice is read by whoever cuts the release.
export const VERSIONING_RULES = [
  {
    field: 'apiHost',
    versioned: true,
    why: 'internal/productclient concatenates its own paths onto this host, so the /v1 prefix belongs here',
  },
]

// checkContent reports the problems in one production profile asset. Pure, so
// a test can drive it with synthetic JSON.
export function checkContent(rel, text) {
  let profile
  try {
    profile = JSON.parse(text)
  } catch (error) {
    return [`${rel}: not valid JSON: ${error.message}`]
  }

  const problems = []

  if (profile.name !== 'production') {
    problems.push(`${rel}: name is ${JSON.stringify(profile.name)}, expected "production"`)
  }
  for (const flag of ALLOW_FLAGS) {
    if (profile[flag] !== false) {
      problems.push(`${rel}: ${flag} is ${JSON.stringify(profile[flag])}, must be false`)
    }
  }

  // Presence, reality and scheme apply to both hosts.
  for (const field of ['apiHost', 'gatewayHost']) {
    const value = profile[field]
    if (typeof value !== 'string' || value.trim() === '') {
      problems.push(`${rel}: ${field} is empty — the deployment host must be compiled in (P0-01 §1)`)
      continue
    }
    if (PLACEHOLDER.test(value)) {
      problems.push(
        `${rel}: ${field} is still the ${JSON.stringify(value)} placeholder — ` +
          'replace it with the deployment host before shipping; a `.invalid` host can never resolve',
      )
    }
    if (!value.startsWith('https://')) {
      problems.push(`${rel}: ${field} must use https (${value})`)
    }
  }

  // Versioning applies only where a rule exists. Kept as a separate pass so that
  // "no rule for this field" cannot silently take the shared checks with it -
  // which is exactly what happened when these checks lived in one loop.
  for (const { field, versioned, why } of VERSIONING_RULES) {
    const value = profile[field]
    if (typeof value !== 'string' || value.trim() === '') continue
    const hasVersion = /\/v1\/?$/.test(value)
    if (versioned && !hasVersion) {
      problems.push(`${rel}: ${field} should be versioned (/v1) (${value}) — ${why}`)
    }
  }

  const keys = profile.trustedKeyIDs
  if (keys === null || typeof keys !== 'object' || Array.isArray(keys)) {
    problems.push(`${rel}: trustedKeyIDs must be an object mapping keyId → base64 ed25519 public key`)
  } else if (Object.keys(keys).length === 0) {
    problems.push(
      `${rel}: trustedKeyIDs is empty — no signed policy or catalog can be verified, ` +
        'so this release has no usable model catalog (P0-01 §1)',
    )
  }

  return problems
}

export async function check(root) {
  let text
  try {
    text = await fs.readFile(path.join(root, PRODUCTION_ASSET), 'utf8')
  } catch (error) {
    if (error?.code === 'ENOENT') {
      return [`${PRODUCTION_ASSET}: missing — no production profile asset to embed`]
    }
    throw error
  }
  return checkContent(PRODUCTION_ASSET, text)
}

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

async function main() {
  const problems = await check(repositoryRoot(import.meta.url))
  if (problems.length > 0) {
    // Advisory: report, but exit 0. Packaging a build with an unset host is
    // legitimate during B0/B1, so this must never break a build or a `make`
    // target — preflight.mjs surfaces the same list as warnings.
    console.warn('release-config-guard: the production profile is not release-ready yet:')
    for (const problem of problems) console.warn(`- ${problem}`)
    return
  }
  console.log('release-config-guard passed: the production profile carries a real control plane.')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
