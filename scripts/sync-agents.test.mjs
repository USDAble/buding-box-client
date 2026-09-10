// Tests for scripts/sync-agents.mjs. Uses node:test + a fake repository root,
// matching the other guard tests (datapath/reuse/server-diff/release-profile).
// The last two cases run against the real repo, because the failure this guards
// against — an upstream merge reverting the fork rules out of .octorules — can
// only be caught against the real file.
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'

import { NORMS_PATH, repositoryRoot } from './norms-guard.mjs'
import {
  AGENTS_PATH,
  BEGIN_MARKER,
  END_MARKER,
  GENERATED_NOTICE,
  OCTORULES_PATH,
  REQUIRED_OCTORULES_ANCHORS,
  check,
  checkOctorules,
  renderAgents,
} from './sync-agents.mjs'

const goodOctorules = [
  '# Project Rules',
  `fork-specific rules in ${NORMS_PATH} are binding`,
  '## Fork rules',
  '1. Never resolve a data path yourself.',
  '2. Never hardcode a brand string.',
  '3. Mark every change to an upstream file with `// OCTO-FORK:`.',
  'Upstream merges use `merge`, never `rebase`.',
].join('\n')

test('renderAgents carries the generated notice and the spec pointer', () => {
  const out = renderAgents(goodOctorules)
  assert.ok(out.startsWith(GENERATED_NOTICE), 'must open with the DO-NOT-EDIT notice')
  assert.ok(out.includes(NORMS_PATH), 'norms-guard requires every entry file to name the spec')
})

test('renderAgents inlines .octorules verbatim between the markers', () => {
  const out = renderAgents(goodOctorules)
  const start = out.indexOf(BEGIN_MARKER)
  const end = out.indexOf(END_MARKER)
  assert.ok(start !== -1 && end !== -1 && start < end, 'both markers must be present, in order')
  const inlined = out.slice(start + BEGIN_MARKER.length, end).trim()
  assert.equal(inlined, goodOctorules.trim(), 'the inlined region must be the source byte-for-byte')
})

test('renderAgents is stable across trailing whitespace in the source', () => {
  assert.equal(renderAgents(`${goodOctorules}\n\n`), renderAgents(goodOctorules))
})

test('checkOctorules accepts a source carrying every anchor', () => {
  assert.deepEqual(checkOctorules(goodOctorules), [])
})

test('checkOctorules rejects a source missing the Fork rules heading', () => {
  const problems = checkOctorules(goodOctorules.replace('## Fork rules', '## Rules'))
  assert.equal(problems.length, 1)
  assert.match(problems[0], /## Fork rules/)
})

test('checkOctorules rejects each dropped hard rule', () => {
  for (const { needle } of REQUIRED_OCTORULES_ANCHORS) {
    const problems = checkOctorules(goodOctorules.replaceAll(needle, 'REDACTED'))
    assert.ok(
      problems.some((p) => p.includes(needle)),
      `dropping ${JSON.stringify(needle)} must be reported`,
    )
  }
})

// Real-repo cases. These are the ratchets: an upstream merge that reverts the
// fork rules out of .octorules, or a hand-edit of the generated AGENTS.md,
// fails here rather than in a Codex session months later.
const root = repositoryRoot(import.meta.url)

test('the real .octorules still carries every fork-rule anchor', async () => {
  const content = await fs.readFile(path.join(root, OCTORULES_PATH), 'utf8')
  assert.deepEqual(checkOctorules(content), [])
})

test('the committed AGENTS.md is in sync with .octorules', async () => {
  const octorules = await fs.readFile(path.join(root, OCTORULES_PATH), 'utf8')
  const committed = await fs.readFile(path.join(root, AGENTS_PATH), 'utf8')
  assert.equal(
    committed,
    renderAgents(octorules),
    `${AGENTS_PATH} is stale — run \`make agents\` and commit the result`,
  )
})

async function fakeRepo(files) {
  const repo = await fs.mkdtemp(path.join(os.tmpdir(), 'sync-agents-'))
  for (const [rel, content] of Object.entries(files)) {
    const abs = path.join(repo, rel)
    await fs.mkdir(path.dirname(abs), { recursive: true })
    await fs.writeFile(abs, content)
  }
  return repo
}

test('check passes on a clean repository', async () => {
  const repo = await fakeRepo({
    [OCTORULES_PATH]: goodOctorules,
    [AGENTS_PATH]: renderAgents(goodOctorules),
  })
  const { problems } = await check(repo)
  assert.deepEqual(problems, [])
})

test('check reports a stale generated file', async () => {
  const repo = await fakeRepo({
    [OCTORULES_PATH]: goodOctorules,
    [AGENTS_PATH]: `${renderAgents(goodOctorules)}\nhand-edited\n`,
  })
  const { problems } = await check(repo)
  assert.ok(problems.some((p) => p.includes('不一致')))
})

test('check reports a missing generated file', async () => {
  const repo = await fakeRepo({ [OCTORULES_PATH]: goodOctorules })
  const { problems } = await check(repo)
  assert.ok(problems.some((p) => p.startsWith(`${AGENTS_PATH}: 缺失`)))
})

test('check reports a missing source', async () => {
  const repo = await fakeRepo({})
  const { problems } = await check(repo)
  assert.ok(problems.some((p) => p.startsWith(`${OCTORULES_PATH}: 缺失`)))
})

test('check reports an upstream merge that dropped the fork rules', async () => {
  const reverted = goodOctorules.replace('## Fork rules', '## Upstream rules')
  const repo = await fakeRepo({
    [OCTORULES_PATH]: reverted,
    [AGENTS_PATH]: renderAgents(reverted),
  })
  const { problems } = await check(repo)
  assert.ok(
    problems.some((p) => p.includes('## Fork rules')),
    'a reverted .octorules must fail even when AGENTS.md matches it',
  )
})
