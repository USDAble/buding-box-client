// Tests for scripts/reuse-guard.mjs. The point of the guard is to catch a
// second SSE/HTTP implementation before it is written, so the tests pin the
// verdict for each capability the gateway package must NOT grow, plus the
// escape hatch and the comment handling.

import assert from 'node:assert/strict'
import { test } from 'node:test'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'

import { checkSource, check, stripComment, REUSE_SCOPES, ALLOW_MARKER } from './reuse-guard.mjs'

const SCOPE = REUSE_SCOPES[0]

// ─── comment handling ───────────────────────────────────────────────────────

test('stripComment splits code from a trailing comment', () => {
  assert.deepEqual(stripComment('x := 1 // note'), { code: 'x := 1 ', comment: '// note' })
  assert.deepEqual(stripComment('x := 1'), { code: 'x := 1', comment: '' })
})

test('a pattern named only in a comment does not trip the guard', () => {
  const content = '// this deliberately does NOT use net/http\nfunc f() {}\n'
  assert.deepEqual(checkSource('a.go', content, SCOPE), [])
})

// ─── each forbidden capability ──────────────────────────────────────────────

test('gateway package: an HTTP client is a violation', () => {
  const content = ['import "net/http"', 'func f() { resp, _ := http.Get(u) }'].join('\n')
  const problems = checkSource('gw.go', content, SCOPE)
  assert.ok(problems.length >= 1)
  assert.match(problems[0], /app\.NewSender/)
})

test('gateway package: SSE scanning is a violation', () => {
  const content = 'scanner := bufio.NewScanner(resp.Body)'
  const problems = checkSource('gw.go', content, SCOPE)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /SSE line scanning/)
})

test('gateway package: SSE protocol knowledge is a violation', () => {
  assert.equal(checkSource('gw.go', 'w.Header().Set("Content-Type", "text/event-stream")', SCOPE).length, 1)
  assert.equal(checkSource('gw.go', 'if line == "[DONE]" {', SCOPE).length, 1)
})

test('gateway package: streaming JSON parsing is a violation', () => {
  const problems = checkSource('gw.go', 'dec := json.NewDecoder(resp.Body)', SCOPE)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /streaming JSON parsing/)
})

test('gateway package: usage parsing is a violation', () => {
  const problems = checkSource('gw.go', 'in, out := u.PromptTokens, u.CompletionTokens', SCOPE)
  assert.equal(problems.length, 1)
  assert.match(problems[0], /usage parsing/)
  // The JSON wire form trips it too.
  assert.equal(checkSource('gw.go', '"prompt_tokens": 12,', SCOPE).length, 1)
})

test('gateway package: reading agent-facing usage is allowed', () => {
  // The observer must surface terminal usage; agent.Usage{InputTokens,...} is
  // the agent-facing type, not the wire format, so it must not be flagged.
  const content = 'n := u.InputTokens + u.CacheReadTokens'
  assert.deepEqual(checkSource('gw.go', content, SCOPE), [])
})

test('gateway package: a retry policy is a violation', () => {
  assert.equal(checkSource('gw.go', 'd := backoff(attempt)', SCOPE).length, 1)
  assert.equal(checkSource('gw.go', 'wait := resp.Header.Get("Retry-After")', SCOPE).length, 1)
})

// ─── what the package IS allowed to do ──────────────────────────────────────

test('gateway package: assembling an app.Sender is allowed', () => {
  const content = [
    'import "github.com/open-octo/octo-agent/internal/app"',
    'func build(opts app.SenderOptions) (agent.Sender, error) {',
    '\ts, err := app.NewSender(opts)',
    '\tif err != nil { return nil, err }',
    '\treturn &observer{inner: s}, nil',
    '}',
  ].join('\n')
  assert.deepEqual(checkSource('gw.go', content, SCOPE), [])
})

test('gateway package: a terminal-status query client is allowed', () => {
  const content = [
    'type controlClient struct { httpc *client }',
    'func (c *controlClient) Cancel(ctx context.Context, id string) error { return c.httpc.Post(ctx, id) }',
  ].join('\n')
  // `client` here is a productclient, not net/http — no forbidden symbol.
  assert.deepEqual(checkSource('gw.go', content, SCOPE), [])
})

// ─── escape hatch ───────────────────────────────────────────────────────────

test('the explicit allow marker skips a line', () => {
  const content = 'import "net/http" // reuse-guard:allow — no upstream client can do X'
  assert.deepEqual(checkSource('gw.go', content, SCOPE), [])
})

test('the marker is exported so the failure message stays accurate', () => {
  assert.ok(ALLOW_MARKER.length > 0)
  const problems = checkSource('gw.go', 'import "net/http"', SCOPE)
  assert.ok(problems[0].includes(ALLOW_MARKER))
})

// ─── filesystem scan ────────────────────────────────────────────────────────

test('check tolerates the governed package not existing yet', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'reuse-guard-'))
  try {
    const { problems, notes } = await check(root)
    assert.deepEqual(problems, [])
    assert.ok(notes.some((n) => n.includes('not created yet')))
  } finally {
    await fs.rm(root, { recursive: true, force: true })
  }
})

test('check scans real Go files and skips _test.go', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'reuse-guard-'))
  const dir = path.join(root, SCOPE.dir)
  try {
    await fs.mkdir(dir, { recursive: true })
    await fs.writeFile(path.join(dir, 'good.go'), 'func f() {}\n')
    // A test file may legitimately reference net/http (httptest); it must not count.
    await fs.writeFile(path.join(dir, 'gw_test.go'), 'import "net/http"\n')
    const { problems } = await check(root)
    assert.deepEqual(problems, [])
  } finally {
    await fs.rm(root, { recursive: true, force: true })
  }
})

test('check reports a violation in a shipped Go file', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'reuse-guard-'))
  const dir = path.join(root, SCOPE.dir)
  try {
    await fs.mkdir(dir, { recursive: true })
    await fs.writeFile(path.join(dir, 'gw.go'), 'import "net/http"\n')
    const { problems } = await check(root)
    assert.equal(problems.length, 1)
  } finally {
    await fs.rm(root, { recursive: true, force: true })
  }
})
