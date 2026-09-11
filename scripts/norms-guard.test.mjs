// Tests for scripts/norms-guard.mjs. Uses node:test + a fake repository root,
// matching the other guard tests (datapath/reuse/server-diff/release-profile).
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'

import { check, checkEntry, ENTRY_FILES, NORMS_PATH, UPSTREAM_NORMS } from './norms-guard.mjs'

async function fakeRepo(files) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'norms-guard-'))
  for (const [rel, content] of Object.entries(files)) {
    const abs = path.join(root, rel)
    await fs.mkdir(path.dirname(abs), { recursive: true })
    await fs.writeFile(abs, content)
  }
  return root
}

const canonical = `# 开发规范\n上游规范见 .octorules 与 CLAUDE.md。\n`

// A repository that satisfies every assertion, so each test can break one thing.
async function goodRepo(overrides = {}) {
  const files = {
    [NORMS_PATH]: canonical,
    'AGENTS.md': `read ${NORMS_PATH}`,
    '.cursor/rules/dev-norms.mdc': `---\nalwaysApply: true\n---\nread ${NORMS_PATH}`,
    '.github/copilot-instructions.md': `read ${NORMS_PATH}`,
    'CLAUDE.md': `read ${NORMS_PATH}`,
    '.octorules': `read ${NORMS_PATH}`,
    ...overrides,
  }
  return fakeRepo(files)
}

test('checkEntry rejects a file that does not name the spec', () => {
  const problems = checkEntry('AGENTS.md', 'no pointer here')
  assert.equal(problems.length, 1)
  assert.match(problems[0], /does not point at/)
})

test('checkEntry rejects a Cursor rule without alwaysApply: true', () => {
  const problems = checkEntry('.cursor/rules/dev-norms.mdc', `---\ndescription: x\n---\n${NORMS_PATH}`, {
    alwaysApply: true,
  })
  assert.equal(problems.length, 1)
  assert.match(problems[0], /alwaysApply/)
})

test('checkEntry accepts a well-formed Cursor rule', () => {
  const problems = checkEntry('.cursor/rules/dev-norms.mdc', `---\nalwaysApply: true\n---\n${NORMS_PATH}`, {
    alwaysApply: true,
  })
  assert.deepEqual(problems, [])
})

test('a complete repository passes', async () => {
  const root = await goodRepo()
  const { problems } = await check(root)
  assert.deepEqual(problems, [])
})

test('a missing entry file fails', async () => {
  const root = await fakeRepo({ [NORMS_PATH]: canonical, 'AGENTS.md': NORMS_PATH })
  const { problems } = await check(root)
  assert.ok(problems.some((p) => p.startsWith('CLAUDE.md: missing')))
  assert.ok(problems.some((p) => p.startsWith('.octorules: missing')))
})

test('a missing canonical spec fails', async () => {
  const root = await fakeRepo({ 'AGENTS.md': NORMS_PATH })
  const { problems } = await check(root)
  assert.ok(problems.some((p) => p.startsWith(`${NORMS_PATH}: canonical fork spec is missing`)))
})

test('the canonical spec must name both upstream norms', async () => {
  const root = await goodRepo({ [NORMS_PATH]: '# 开发规范\nno upstream named\n' })
  const { problems } = await check(root)
  for (const upstream of UPSTREAM_NORMS) {
    assert.ok(problems.some((p) => p.includes(upstream) && p.includes('binding upstream spec')))
  }
})

test('every tool family has an entry point registered', () => {
  const files = ENTRY_FILES.map((e) => e.file)
  for (const expected of ['AGENTS.md', '.cursor/rules/dev-norms.mdc', '.github/copilot-instructions.md']) {
    assert.ok(files.includes(expected), `${expected} should be a registered entry point`)
  }
})
