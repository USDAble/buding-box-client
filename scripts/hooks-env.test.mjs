import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

// OCTO-FORK: GUI-path recovery must preserve selected tools and still enforce gates.
test('both Git hooks find user tools with a GUI PATH and propagate failed checks', t => {
  if (process.platform === 'win32') return t.skip('POSIX Git hook execution')
  const home = mkdtempSync(path.join(tmpdir(), 'hook-env-'))
  t.after(() => rmSync(home, { recursive: true, force: true }))
  const bin = path.join(home, '.local/bin')
  mkdirSync(bin, { recursive: true })
  for (const tool of ['go', 'node']) {
    writeFileSync(path.join(bin, tool), '#!/bin/sh\nexit 0\n', { mode: 0o755 })
  }
  const selected = path.join(home, 'selected')
  mkdirSync(selected)
  writeFileSync(path.join(selected, 'dirname'), '#!/bin/sh\nprintf "%s\\n" "${1%/*}"\n', { mode: 0o755 })
  writeFileSync(path.join(selected, 'git'), '#!/bin/sh\nexit 0\n', { mode: 0o755 })
  writeFileSync(path.join(selected, 'xargs'), '#!/bin/sh\nexit 1\n', { mode: 0o755 })
  writeFileSync(path.join(selected, 'make'), '#!/bin/sh\nprintf "CHECK:%s\\n" "$1"\nexit "${HOOK_TEST_EXIT:-0}"\n', { mode: 0o755 })
  const env = { ...process.env, HOME: home, PATH: selected }
  const source = spawnSync('/bin/sh', ['-c', '. .githooks/env.sh; command -v go; command -v node; command -v make'], { env, encoding: 'utf8' })
  assert.equal(source.status, 0, source.stderr)
  assert.equal(source.stdout, `${bin}/go\n${bin}/node\n${selected}/make\n`)

  for (const [hook, target] of [['pre-commit', 'quick-check'], ['pre-push', 'gate']]) {
    for (const code of [0, 42]) {
      const result = spawnSync('/bin/sh', [`.githooks/${hook}`], { env: { ...env, HOOK_TEST_EXIT: String(code) }, encoding: 'utf8' })
      assert.match(result.stderr, new RegExp(`CHECK:${target}`))
      assert.equal(result.status, code === 0 ? 0 : 1, result.stderr)
    }
  }
})
