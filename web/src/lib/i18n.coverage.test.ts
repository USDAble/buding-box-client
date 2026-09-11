import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, extname } from 'node:path'
import { get } from 'svelte/store'
import { en, zh, t, setLocale } from './i18n'
import { brandName, brandShortName } from './brand'

// A key used by a component but absent from the tables is not a crash: t()
// falls back to the raw key, so the UI silently renders "chat.cancel" as a
// tooltip. That shipped once (ChatView's cancel-edit button), which is why
// this sweeps the whole source tree instead of listing keys by hand.
//
// Only literal `$t('some.key')` / `t('some.key')` call sites are visible here;
// keys assembled at runtime (e.g. PERM_LABEL_KEY[mode]) are invisible to a
// regex and are the reason this file never asserts the reverse direction —
// "defined but unused" would flag those as dead and invite a wrong deletion.
// vitest runs with cwd = web/ (see vitest.config.ts); import.meta.url is not
// a file:// URL under the jsdom environment, so resolve from cwd instead.
const SRC = join(process.cwd(), 'src')
const KEY_CALL = /\$?\bt\(\s*'([A-Za-z0-9_.]+)'/g

function sourceFiles(dir: string): string[] {
  const out: string[] = []
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) {
      out.push(...sourceFiles(p))
      continue
    }
    if (!['.svelte', '.ts'].includes(extname(name))) continue
    if (name.includes('i18n') || name.endsWith('.test.ts')) continue
    out.push(p)
  }
  return out
}

function usedKeys(): Map<string, string> {
  const first = new Map<string, string>()
  for (const file of sourceFiles(SRC)) {
    const lines = readFileSync(file, 'utf8').split('\n')
    lines.forEach((line, i) => {
      for (const m of line.matchAll(KEY_CALL)) {
        if (!first.has(m[1])) first.set(m[1], `${file.slice(SRC.length + 1)}:${i + 1}`)
      }
    })
  }
  return first
}

describe('i18n coverage', () => {
  it('every literal key used in the app is defined in both locales', () => {
    const missing: string[] = []
    for (const [key, where] of usedKeys()) {
      if (!(key in en)) missing.push(`${key} (en) — ${where}`)
      if (!(key in zh)) missing.push(`${key} (zh) — ${where}`)
    }
    expect(missing).toEqual([])
  })

  it('finds a meaningful number of call sites', () => {
    // Guards the guard: a regex that silently stops matching would make the
    // test above pass vacuously.
    expect(usedKeys().size).toBeGreaterThan(200)
  })

  it('en and zh define exactly the same keys', () => {
    expect(Object.keys(zh).sort()).toEqual(Object.keys(en).sort())
  })

  it('no key maps to an empty string', () => {
    const blank = [
      ...Object.entries(en).filter(([, v]) => !v.trim()).map(([k]) => `en:${k}`),
      ...Object.entries(zh).filter(([, v]) => !v.trim()).map(([k]) => `zh:${k}`),
    ]
    expect(blank).toEqual([])
  })
})

// The product name belongs in branding/brand.json and reaches copy as a
// {brand} / {brandShort} placeholder. Spelling it out in a dictionary is what
// makes a rename a 1900-line sweep, and what makes a rename miss the strings
// nobody remembered.
//
// Capitalised "Octo" only: the lowercase form is the CLI command, the Go
// module path and the ~/.octo config directory, none of which this change
// touches (see 需求20260906.md §135).
const BRAND_LITERAL = /\bOcto\b|布丁盒子|Pudding Box/

// Keys allowed to name the product literally, each with the reason. Kept as a
// map rather than a list so a stale entry is self-documenting.
const BRAND_LITERAL_ALLOWED: Record<string, string> = {
  // Empty on purpose, and cheap to keep: the last entry,
  // settings.workspace_dir_desc, closed when P1 moved the default workspace to
  // data/workspace/. That was the documented order — the copy followed the
  // behaviour, so it no longer names the old directory. The staleness test
  // below is what deletes an entry that outlives its reason; leaving the map in
  // place gives the next genuine exemption somewhere to live.
}

describe('i18n brand placeholders', () => {
  const dicts = { en, zh }

  it('no dictionary value spells the product name out', () => {
    const offenders: string[] = []
    for (const [locale, dict] of Object.entries(dicts)) {
      for (const [key, value] of Object.entries(dict)) {
        if (!BRAND_LITERAL.test(value)) continue
        if (key in BRAND_LITERAL_ALLOWED) continue
        offenders.push(`${locale}:${key} — ${value}`)
      }
    }
    expect(offenders).toEqual([])
  })

  it('every allowlisted key still needs its exemption', () => {
    // Without this, the exemption above outlives the reason for it and
    // quietly re-opens the hole it was scoped to.
    const stale = Object.keys(BRAND_LITERAL_ALLOWED).filter(
      (key) => !BRAND_LITERAL.test(en[key] ?? '') && !BRAND_LITERAL.test(zh[key] ?? ''),
    )
    expect(stale).toEqual([])
  })

  it('brand placeholders are spelled exactly {brand} or {brandShort}', () => {
    // A typo renders literally rather than failing, so "Welcome to {Brand}"
    // would ship as those nine characters. Only these two are interpolated.
    const typos: string[] = []
    for (const [locale, dict] of Object.entries(dicts)) {
      for (const [key, value] of Object.entries(dict)) {
        for (const m of value.matchAll(/\{([A-Za-z]*[Bb]rand[A-Za-z]*)\}/g)) {
          if (m[1] === 'brand' || m[1] === 'brandShort') continue
          typos.push(`${locale}:${key} — {${m[1]}}`)
        }
      }
    }
    expect(typos).toEqual([])
  })

  it('Chinese copy does not space the placeholder off from adjacent CJK', () => {
    // The reason this is a placeholder and not a replaceAll of the old name:
    // "关于 Octo" needs the space, "关于布丁盒子" does not. A blanket
    // substitution keeps the space and leaves CJK copy looking gappy.
    //
    // Adjacency to a CJK character specifically — a space before the " · "
    // separator in settings.about.footer is correct and must not be flagged.
    const CJK_GAP = /[\u4e00-\u9fff]\s+\{brand(Short)?\}|\{brand(Short)?\}\s+[\u4e00-\u9fff]/
    const padded = Object.entries(zh)
      .filter(([, v]) => CJK_GAP.test(v))
      .map(([k, v]) => `zh:${k} — ${v}`)
    expect(padded).toEqual([])
  })

  it('t() substitutes {brand} in both locales', () => {
    for (const locale of ['en', 'zh']) {
      setLocale(locale)
      const rendered = get(t)('onboard.title')
      expect(rendered).toContain(brandName(locale))
      expect(rendered).not.toContain('{brand')
    }
    setLocale('en')
  })

  it('t() substitutes {brandShort}', () => {
    // English only: the Chinese chat placeholder reads "发消息…" and never
    // named the product, which a placeholder is free to preserve.
    setLocale('en')
    const rendered = get(t)('chat.placeholder')
    expect(rendered).toContain(brandShortName('en'))
    expect(rendered).not.toContain('{brand')
  })

  it('a meaningful number of values use a brand placeholder', () => {
    // Guards the guard: if the placeholders were dropped, every assertion
    // above would pass vacuously.
    const count = [...Object.values(en), ...Object.values(zh)].filter((v) =>
      v.includes('{brand'),
    ).length
    expect(count).toBeGreaterThan(20)
  })
})
