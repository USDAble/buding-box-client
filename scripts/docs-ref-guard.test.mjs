import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'

import { check } from './docs-ref-guard.mjs'

async function fixture(files) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'docs-ref-guard-'))
  for (const [rel, content] of Object.entries(files)) {
    const absolute = path.join(root, rel)
    await fs.mkdir(path.dirname(absolute), { recursive: true })
    await fs.writeFile(absolute, content)
  }
  return root
}

test('accepts a current source reference and a resolving maintained-doc link', async () => {
  const docRoot = 'dev-docs-puddingbox'
  const current = `${docRoot}/规范/开发规范.md`
  const root = await fixture({
    'scripts/example.mjs': `// ${current}`,
    [current]: '# rules',
    [`${docRoot}/guide.md`]: '[rules](规范/开发规范.md)',
  })
  const { problems } = await check(root)
  assert.deepEqual(problems, [])
})

test('rejects retired source paths and broken maintained-doc links', async () => {
  const retired = 'dev-docs-' + 'usdable/开发规范.md'
  const docRoot = 'dev-docs-puddingbox'
  const root = await fixture({
    'internal/example.go': `// ${retired}`,
    [`${docRoot}/guide.md`]: '[missing](missing.md)',
  })
  const { problems } = await check(root)
  assert.equal(problems.length, 2)
  assert.ok(problems.some((problem) => problem.includes(retired)))
  assert.ok(problems.some((problem) => problem.includes('does not resolve')))
})
