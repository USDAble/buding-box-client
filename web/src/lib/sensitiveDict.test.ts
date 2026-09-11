import { describe, it, expect } from 'vitest'
import { normalizeWord, isUsableWord, containsWord, parseDictText, serializeDict, type SensitiveDict } from './sensitiveDict'

describe('normalizeWord', () => {
  it('collapses whitespace and symbols so 发 票 and 发票 are the same', () => {
    expect(normalizeWord('发 票')).toBe(normalizeWord('发票'))
    expect(normalizeWord('发-票')).toBe(normalizeWord('发票'))
  })

  it('folds case', () => {
    expect(normalizeWord('INVOICE')).toBe(normalizeWord('invoice'))
  })

  it('normalizes to empty for pure symbols / whitespace', () => {
    expect(normalizeWord('!!!')).toBe('')
    expect(normalizeWord('   ')).toBe('')
    expect(normalizeWord('')).toBe('')
  })
})

describe('isUsableWord', () => {
  it('accepts real words, rejects empty/pure-symbol', () => {
    expect(isUsableWord('发票')).toBe(true)
    expect(isUsableWord('!!!')).toBe(false)
    expect(isUsableWord('   ')).toBe(false)
  })
})

describe('containsWord', () => {
  const dict: SensitiveDict = { builtin: ['赌博', '发票'], user: ['测试词'] }

  it('detects built-in duplicates (normalized)', () => {
    expect(containsWord(dict, '发 票')).toBe(true)
    expect(containsWord(dict, '赌博')).toBe(true)
  })

  it('detects user duplicates (normalized)', () => {
    expect(containsWord(dict, '测 试 词')).toBe(true)
  })

  it('does not match a new word', () => {
    expect(containsWord(dict, '苹果')).toBe(false)
  })
})

describe('parseDictText', () => {
  it('splits lines, skips comments and blanks, trims whitespace', () => {
    expect(parseDictText('# header\n\n  苹果  \n发票\n')).toEqual(['苹果', '发票'])
  })
})

describe('serializeDict', () => {
  it('writes one word per line in the file format', () => {
    expect(serializeDict(['苹果', '发票'])).toBe('苹果\n发票\n')
  })
})
