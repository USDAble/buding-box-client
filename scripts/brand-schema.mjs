// Validates branding/brand.json against the three structural rules that keep
// brand values in the right bucket (see dev-docs-usdable/需求/2260906/品牌升级方案.md §2.5):
//
//   A 本地化文案   product / about / copy   → locale map, zh-CN + en-US both present
//   B 固定显示值   display                  → single string, never a locale map
//   C 标识符与路径 identifiers / links / visual → single ASCII string, no spaces, never localized
//
// Rule C is the one that matters most in practice: it makes "translate a path"
// and "give an identifier a zh-CN variant" unrepresentable rather than merely
// discouraged. Run directly for a CLI check, or import validateBrand.

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const SUPPORTED_SCHEMA_VERSION = 2

// Every A-class entry must carry these; a missing one ships a half-translated UI.
export const REQUIRED_LOCALES = ['zh-CN', 'en-US']

const LOCALIZED_SECTIONS = ['product', 'about', 'copy']
const FIXED_DISPLAY_SECTIONS = ['display']
const IDENTIFIER_SECTIONS = ['identifiers', 'links', 'visual']

const KNOWN_TOP_LEVEL = new Set([
  'schemaVersion',
  'brandId',
  ...LOCALIZED_SECTIONS,
  ...FIXED_DISPLAY_SECTIONS,
  ...IDENTIFIER_SECTIONS,
])

// Printable ASCII excluding space (0x20). Covers paths, protocol names, GUIDs,
// %ENV% references, Windows backslashes, URLs and hex colours.
const ASCII_NO_SPACE = /^[\x21-\x7E]+$/

// A locale map key such as zh-CN. Used to recognise "someone localized a C-class
// value" and report that specific mistake instead of a generic type error.
const LOCALE_KEY = /^[a-z]{2}-[A-Za-z0-9]{2,8}$/

// Entries consumers dereference without a fallback; a typo here is a runtime
// blank string on a user-visible surface, so it fails the build instead.
const REQUIRED_PATHS = [
  'product.names',
  'product.shortName',
  'product.tagline',
  'about.teamName',
  'about.copyright',
  'display.windows.productName',
  'display.windows.fileDescription',
  'identifiers.current.exeName',
  'identifiers.current.singleInstanceId',
  'identifiers.current.portableDirName',
  'links.external.license',
  'visual.logo.mark',
]

function isPlainObject(value) {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function label(segments) {
  return segments.join('.')
}

function resolvePath(root, dottedPath) {
  let cursor = root
  for (const segment of dottedPath.split('.')) {
    if (!isPlainObject(cursor)) return undefined
    cursor = cursor[segment]
  }
  return cursor
}

function validateLocalizedSection(section, name, errors) {
  for (const [field, value] of Object.entries(section)) {
    const at = label([name, field])
    if (!isPlainObject(value)) {
      errors.push(`${at}: A 类文案必须是语言 map（如 {"zh-CN": …, "en-US": …}），实际是 ${typeof value}`)
      continue
    }
    for (const locale of REQUIRED_LOCALES) {
      const text = value[locale]
      if (typeof text !== 'string' || text.trim() === '') {
        errors.push(`${at}: 缺少 ${locale} 值（A 类必须 ${REQUIRED_LOCALES.join(' 与 ')} 齐全）`)
      }
    }
    for (const [locale, text] of Object.entries(value)) {
      if (!LOCALE_KEY.test(locale)) {
        errors.push(`${label([name, field, locale])}: 不是合法的语言标签（应形如 zh-CN / en-US）`)
      }
      if (typeof text !== 'string') {
        errors.push(`${label([name, field, locale])}: 值必须是字符串，实际是 ${typeof text}`)
      } else if (text.trim() === '') {
        errors.push(`${label([name, field, locale])}: 值为空。语言键存在就是承诺，宁缺勿空`)
      }
    }
  }
}

function validateFixedDisplaySection(section, name, errors) {
  for (const [group, fields] of Object.entries(section)) {
    const groupAt = label([name, group])
    if (!isPlainObject(fields)) {
      errors.push(`${groupAt}: 应按平台分组（如 display.windows.*），实际是 ${typeof fields}`)
      continue
    }
    for (const [field, value] of Object.entries(fields)) {
      const at = label([name, group, field])
      if (isPlainObject(value)) {
        const localeKeys = Object.keys(value).filter((key) => LOCALE_KEY.test(key))
        errors.push(
          localeKeys.length > 0
            ? `${at}: B 类固定显示值不随界面语言切换，不能是语言 map（发现 ${localeKeys.join(', ')}）`
            : `${at}: B 类必须是单个字符串，实际是 object`,
        )
        continue
      }
      if (typeof value !== 'string') {
        errors.push(`${at}: B 类必须是单个字符串，实际是 ${typeof value}`)
      } else if (value.trim() === '') {
        errors.push(`${at}: 值为空`)
      }
    }
  }
}

function validateIdentifierTree(value, segments, errors) {
  if (isPlainObject(value)) {
    const localeKeys = Object.keys(value).filter((key) => LOCALE_KEY.test(key))
    if (localeKeys.length > 0) {
      errors.push(
        `${label(segments)}: C 类标识符与路径一律英文，永不本地化（发现语言键 ${localeKeys.join(', ')}）`,
      )
      return
    }
    for (const [key, child] of Object.entries(value)) {
      validateIdentifierTree(child, [...segments, key], errors)
    }
    return
  }
  const at = label(segments)
  if (typeof value !== 'string') {
    errors.push(`${at}: C 类必须是单个字符串，实际是 ${typeof value}`)
    return
  }
  if (value === '') {
    errors.push(`${at}: 值为空`)
    return
  }
  if (!ASCII_NO_SPACE.test(value)) {
    errors.push(
      `${at} = ${JSON.stringify(value)}: C 类必须是无空格的可打印 ASCII（不得含中文、空格、全角标点）`,
    )
  }
}

// validateBrand returns a list of human-readable problems; an empty list means
// the configuration is safe to distribute.
export function validateBrand(brand) {
  const errors = []

  if (!isPlainObject(brand)) {
    return ['brand.json 顶层必须是 JSON 对象']
  }

  if (brand.schemaVersion !== SUPPORTED_SCHEMA_VERSION) {
    errors.push(
      `schemaVersion: 期望 ${SUPPORTED_SCHEMA_VERSION}，实际 ${JSON.stringify(brand.schemaVersion)}。` +
        '改字段类别、改段名或删字段才需要升版本；升了要同步 Go 与 TS 两侧类型定义',
    )
  }

  if (typeof brand.brandId !== 'string' || !ASCII_NO_SPACE.test(brand.brandId)) {
    errors.push('brandId: 必须是无空格的 ASCII 字符串')
  }

  for (const key of Object.keys(brand)) {
    // A leading underscore marks generator-injected provenance fields.
    if (key.startsWith('_')) continue
    if (!KNOWN_TOP_LEVEL.has(key)) {
      errors.push(`${key}: 未知的顶层段。已知段：${[...KNOWN_TOP_LEVEL].join(', ')}`)
    }
  }

  for (const name of LOCALIZED_SECTIONS) {
    const section = brand[name]
    if (section === undefined) continue
    if (!isPlainObject(section)) {
      errors.push(`${name}: 必须是对象`)
      continue
    }
    validateLocalizedSection(section, name, errors)
  }

  for (const name of FIXED_DISPLAY_SECTIONS) {
    const section = brand[name]
    if (section === undefined) continue
    if (!isPlainObject(section)) {
      errors.push(`${name}: 必须是对象`)
      continue
    }
    validateFixedDisplaySection(section, name, errors)
  }

  for (const name of IDENTIFIER_SECTIONS) {
    const section = brand[name]
    if (section === undefined) continue
    validateIdentifierTree(section, [name], errors)
  }

  for (const dottedPath of REQUIRED_PATHS) {
    if (resolvePath(brand, dottedPath) === undefined) {
      errors.push(`${dottedPath}: 缺失。消费端会直接取这个值，没有回退`)
    }
  }

  return errors
}

export const BRAND_SOURCE_RELATIVE_PATH = 'branding/brand.json'

export function repositoryRoot(scriptUrl) {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), '..')
}

export async function loadBrand(root) {
  const source = path.join(root, BRAND_SOURCE_RELATIVE_PATH)
  return JSON.parse(await fs.readFile(source, 'utf8'))
}

async function main() {
  const root = repositoryRoot(import.meta.url)
  const brand = await loadBrand(root)
  const errors = validateBrand(brand)
  if (errors.length > 0) {
    console.error(`${BRAND_SOURCE_RELATIVE_PATH} 校验未通过：`)
    for (const error of errors) console.error(`- ${error}`)
    process.exitCode = 1
    return
  }
  console.log(`${BRAND_SOURCE_RELATIVE_PATH} 校验通过（schemaVersion ${brand.schemaVersion}）。`)
}

// Only run the CLI when invoked directly, so importing the validator is free of
// side effects.
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main()
}
