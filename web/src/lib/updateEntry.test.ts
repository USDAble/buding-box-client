// 需求 §5.1.2 第 13 条 + PQ12: the portable form offers no updates. The
// requirement closes the automatic check and the tray entry and fixes the shape
// of the remaining one — "界面上更新入口常驻但不可用（点击给「即将支持」）", so a
// future integration does not have to move the layout.
//
// The dangerous half of that lives in Go (no outbound lookup, no in-place
// write-back) and is pinned there — cmd/octo-desktop/updates_disabled_test.go.
// This is the other half: the product UI must not render a live update
// affordance, because a live one is also a *lying* one once the shell stops
// checking. With UpdateCheck off, "Check for Updates" answers "You're on the
// latest version" — a claim the client never verified (§3.9: a fallback has to
// say where it fell back to, not invent an answer).
//
// WHY A SOURCE SCAN, AND WHY IT FOLLOWS IMPORTS. The failure is a template
// affordance, not a function's return value, so calling the components would
// only test the ones a test happens to mount. And the exemption this needs —
// VersionBadge.svelte still contains a live check and a download link — must not
// be a hand-written allowlist: a list would keep exempting it after someone
// mounts it again. So the scan computes what App.svelte can actually render,
// and asserts VersionBadge is outside that set. Mounting it puts it back inside
// and turns this red.
//
// Scope (§3.10): the render path reachable from web/src/App.svelte, by static
// relative .svelte imports. A component pulled in through a dynamic import or an
// aliased path would be missed; nothing in this tree does that today.
import { describe, expect, it } from 'vitest'
import { readFileSync, statSync, readdirSync } from 'node:fs'
import { dirname, extname, join, resolve } from 'node:path'

// vitest's root is web/, so this is web/src regardless of where this file lives.
const SRC = join(process.cwd(), 'src')
const ENTRY = join(SRC, 'App.svelte')

// Strings only a live update entry reads: the check/download/upgrade controls
// and their result copy. The placeholder row uses `settings.update` for the
// label and `product.panel.soon` for the detail, so it matches none of these —
// which is exactly the distinction this test draws.
const LIVE_UPDATE_MARKERS = [
  'settings.update.check', // "Check for Updates"
  'settings.update.checking', // "Checking…"
  'settings.update_available',
  'upgrade.btn.download',
  'upgrade.btn.upgrade',
  'onclick={checkUpdate}',
  'onclick={downloadUpdate}',
]

// templateOf returns the markup after the last </script>, or the whole source
// when there is none. Only the template is scanned: the script half still holds
// upstream's checkUpdate as deliberately unreachable code (硬规则 3), and a
// template-scoped scan is what distinguishes "kept" from "rendered".
function templateOf(source: string): string {
  const end = source.lastIndexOf('</script>')
  const body = end === -1 ? source : source.slice(end + '</script>'.length)
  return body.replace(/<!--[\s\S]*?-->/g, '')
}

function relativeImports(source: string): string[] {
  const out: string[] = []
  const patterns = [/from\s+['"](\.[^'"]+)['"]/g, /import\s+['"](\.[^'"]+)['"]/g]
  for (const re of patterns) {
    for (const match of source.matchAll(re)) out.push(match[1])
  }
  return out
}

// renderPath walks the relative .svelte imports from the mount root and returns
// every component App.svelte can render.
function renderPath(entry: string): Set<string> {
  const seen = new Set<string>()
  const queue = [entry]
  while (queue.length > 0) {
    const file = queue.pop() as string
    if (seen.has(file)) continue
    if (!statSync(file).isFile()) continue
    seen.add(file)
    const source = readFileSync(file, 'utf8')
    for (const spec of relativeImports(source)) {
      if (extname(spec) !== '.svelte') continue
      queue.push(resolve(dirname(file), spec))
    }
  }
  return seen
}

function filesUnder(dir: string): string[] {
  const out: string[] = []
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) {
      out.push(...filesUnder(path))
      continue
    }
    if (extname(name) === '.svelte') out.push(path)
  }
  return out
}

describe('the product UI offers no live update entry', () => {
  const reachable = renderPath(ENTRY)

  it('the render path really reaches the update entries', () => {
    // Without this, an import graph that silently resolved nothing would make
    // the scan below pass by examining an empty set.
    expect(
      [...reachable].some((p) => p.endsWith(join('overlays', 'SettingsModal.svelte'))),
      'SettingsModal.svelte must be reachable from App.svelte — if this fails the import walk ' +
        'is broken, not the UI',
    ).toBe(true)
  })

  it('no reachable component renders a live update affordance', () => {
    const offenders = [...reachable]
      .filter((path) => {
        const markup = templateOf(readFileSync(path, 'utf8'))
        return LIVE_UPDATE_MARKERS.some((marker) => markup.includes(marker))
      })
      .map((path) => path.slice(SRC.length + 1))

    expect(
      offenders,
      'the portable form does not update itself (需求 §5.1.2 第 13 条 / PQ12): its update entry is ' +
        'a permanent, unusable placeholder using `product.panel.soon` — see AccountPanel.svelte for ' +
        'the reference shape. A live control is also a lying one, because the shell no longer ' +
        'checks for releases.',
    ).toEqual([])
  })

  it('the excluded component is excluded because nothing mounts it', () => {
    // The reason VersionBadge.svelte is allowed to keep its live entry is that
    // it is dead: AboutPage.svelte replaced it (its own comment says so). Pin
    // the reason, not the filename — if it is ever mounted again the first two
    // assertions fail, and this one explains why that is intended rather than a
    // scan bug.
    const badge = join(SRC, 'components', 'layout', 'VersionBadge.svelte')
    expect(filesUnder(SRC)).toContain(badge)
    expect(reachable.has(badge)).toBe(false)
  })
})
