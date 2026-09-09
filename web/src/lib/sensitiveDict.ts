// sensitiveDict.ts — the client half of P13's dictionary-management page.
//
// The file data/sensitive-words.txt is the single source of truth: this module
// only edits it through the Go-side API and never keeps a second copy. The
// normalization here mirrors internal/sensitive/normalize.go's normalizeText
// for a single word (lowercase, strip whitespace, strip the fixed symbol set)
// and is used ONLY to detect duplicates/emptiness on the client; the server
// re-validates authoritatively on write.

import { get } from 'svelte/store'
import { request } from './api'
import { nativeShell } from './stores'
import * as api from './api'

export interface SensitiveDict {
  builtin: string[]
  user: string[]
}

export interface ImportPreview {
  added: number
  skipped: number
}

// STRIP_SYMBOLS mirrors the Go constant in internal/sensitive/normalize.go —
// keep the two in lockstep. unicode.IsPunct is deliberately not used (it would
// remove more than the product contract allows).
const STRIP_SYMBOLS = '!"#$%&\'()*+,-./:;<=>?@[\\]^_`{|}~' +
  '！＂＃＄％＆＇（）＊＋，－．／：；＜＝＞？＠［＼］＾＿｀｛｜｝～' +
  '、。「」『』《》—…·'

/** The normalized dictionary form of a word (lowercase, no whitespace/symbols). */
export function normalizeWord(word: string): string {
  let out = ''
  for (const ch of word.toLowerCase()) {
    if (/\s/.test(ch) || STRIP_SYMBOLS.includes(ch)) continue
    out += ch
  }
  return out
}

/** A word is usable when it does not normalize to empty (not pure symbols/space). */
export function isUsableWord(word: string): boolean {
  return normalizeWord(word) !== ''
}

/** Whether word (after normalization) already exists in builtin or user. */
export function containsWord(dict: SensitiveDict, word: string): boolean {
  const norm = normalizeWord(word)
  if (!norm) return false
  if (dict.builtin.some((w) => normalizeWord(w) === norm)) return true
  return dict.user.some((w) => normalizeWord(w) === norm)
}

/** Parses dictionary text (one word per line, `#` comments, blank lines). */
export function parseDictText(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter((l) => l !== '' && !l.startsWith('#'))
}

export async function fetchDict(): Promise<SensitiveDict> {
  return request<SensitiveDict>('/api/product/sensitive/dict')
}

export async function saveDict(user: string[]): Promise<{ user: string[] }> {
  return request<{ user: string[] }>('/api/product/sensitive/dict', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ user }),
  })
}

export async function importWords(words: string[], dryRun: boolean): Promise<ImportPreview> {
  return request<ImportPreview>('/api/product/sensitive/dict/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ words, dryRun }),
  })
}

/** Serializes the user list in the file's own format (one word per line). */
export function serializeDict(user: string[]): string {
  return user.map((w) => w + '\n').join('')
}

/** Exports the user list: OS save dialog in the shell, blob download in web. */
export async function exportDict(user: string[]): Promise<void> {
  const content = serializeDict(user)
  const name = 'sensitive-words.txt'
  if (get(nativeShell)) {
    await api.nativeSaveFile(name, content)
    return
  }
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
