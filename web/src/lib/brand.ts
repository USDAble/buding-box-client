// Product identity for the web UI. Mirrors internal/brand (Go) so both sides
// read the same values through the same fallback rules.
//
// brand.config.json is generated from branding/brand.json by
// scripts/sync-branding.mjs — never edit it directly.
//
// Values fall into three classes (see the branding plan under
// dev-docs-usdable/需求/2260906/); the accessors mirror them:
//
//   A localized copy      brandName, brandShortName, brandTeamName, brandText, …
//   B fixed display value brandDisplay
//   C identifier or path  brandIdentifier, brandLink, brandAsset, brandColor
//
// Prefer these accessors over indexing the config: the UI locale store holds
// bare 'zh' / 'en' while the configuration is keyed 'zh-CN' / 'en-US', so
// `config.about.teamName[locale]` silently misses and renders English inside a
// Chinese UI. localize() is what bridges the two.

import config from './brand.config.json'

/** The brand.config.json layout this module understands. */
export const BRAND_SCHEMA_VERSION = 2

/** Locale used when a value is missing for the requested one. */
export const DEFAULT_LOCALE = 'en-US'

export type Localized = Record<string, string>

export interface BrandConfig {
  schemaVersion: number
  brandId: string
  product: {
    names: Localized
    shortName: Localized
    tagline: Localized
    description: Localized
  }
  about: {
    teamName: Localized
    copyright: Localized
    termsTitle: Localized
    privacyTitle: Localized
  }
  copy: Record<string, Localized>
  display: Record<string, Record<string, string>>
  identifiers: {
    current: Record<string, string>
    future: Record<string, string>
  }
  links: Record<string, Record<string, string>>
  visual: {
    logo: Record<string, string>
    colors: Record<string, string>
  }
}

export const brand = config as unknown as BrandConfig

/**
 * Resolves a locale against a value map: exact tag, then any tag sharing the
 * language subtag, then the English default, then any non-empty value.
 *
 * The language-subtag step is the important one — it is what lets the UI's
 * bare 'zh' find the configuration's 'zh-CN'.
 */
export function localize(values: Localized | undefined, locale: string): string {
  if (!values) return ''
  const exact = values[locale]
  if (exact) return exact

  const language = locale.split('-')[0]
  if (language) {
    for (const [tag, value] of Object.entries(values)) {
      if (!value) continue
      if (tag === language || tag.startsWith(`${language}-`)) return value
    }
  }

  if (values[DEFAULT_LOCALE]) return values[DEFAULT_LOCALE]
  for (const value of Object.values(values)) {
    if (value) return value
  }
  return ''
}

/** Full product name: 布丁盒子 / Pudding Box. */
export function brandName(locale: string): string {
  return localize(brand.product.names, locale)
}

/**
 * Short product name, for surfaces where the full name would be truncated:
 * tray tooltips, notification titles, narrow headers.
 */
export function brandShortName(locale: string): string {
  return localize(brand.product.shortName, locale) || brandName(locale)
}

export function brandTagline(locale: string): string {
  return localize(brand.product.tagline, locale)
}

export function brandDescription(locale: string): string {
  return localize(brand.product.description, locale)
}

/** Publisher shown in About. */
export function brandTeamName(locale: string): string {
  return localize(brand.about.teamName, locale)
}

/** Copyright line with the {year} placeholder still in it. */
export function brandCopyright(locale: string): string {
  return localize(brand.about.copyright, locale)
}

/** Copyright line with {year} substituted; defaults to the current year. */
export function brandCopyrightFor(locale: string, year: number = new Date().getFullYear()): string {
  return brandCopyright(locale).replaceAll('{year}', String(year))
}

export function brandTermsTitle(locale: string): string {
  return localize(brand.about.termsTitle, locale)
}

export function brandPrivacyTitle(locale: string): string {
  return localize(brand.about.privacyTitle, locale)
}

/**
 * One entry from the shared copy section — copy reused outside the web i18n
 * dictionaries, such as the in-app legal placeholder pages.
 */
export function brandText(key: string, locale: string): string {
  return localize(brand.copy[key], locale)
}

/**
 * A class-B fixed display value. Takes no locale on purpose: these surface in
 * OS-rendered metadata that is not re-rendered when the UI language changes.
 */
export function brandDisplay(group: string, key: string): string {
  return brand.display[group]?.[key] ?? ''
}

/** A class-C identifier or path. Always ASCII, never localized. */
export function brandIdentifier(key: string): string {
  return brand.identifiers.current[key] ?? ''
}

/** A URL or in-app route, e.g. brandLink('external', 'license'). */
export function brandLink(group: string, key: string): string {
  return brand.links[group]?.[key] ?? ''
}

/** A brand asset path relative to branding/. */
export function brandAsset(key: string): string {
  return brand.visual.logo[key] ?? ''
}

/** A brand colour, e.g. brandColor('primary'). */
export function brandColor(key: string): string {
  return brand.visual.colors[key] ?? ''
}

/**
 * Identifier keys. Constants rather than inline strings so a typo is a
 * compile error instead of an empty string at the call site.
 */
export const IDENTIFIER = {
  exeName: 'exeName',
  cliExeName: 'cliExeName',
  portableDirName: 'portableDirName',
  singleInstanceId: 'singleInstanceId',
  windowsInternalName: 'windowsInternalName',
  dataRoot: 'dataRoot',
  workspaceDir: 'workspaceDir',
  port: 'port',
  cliCommand: 'cliCommand',
  configDir: 'configDir',
  envPrefix: 'envPrefix',
  uiProtocol: 'uiProtocol',
  pairingProtocol: 'pairingProtocol',
  goModule: 'goModule',
  macBundleId: 'macBundleId',
  innoAppId: 'innoAppId',
  mobileAppId: 'mobileAppId',
  installDir: 'installDir',
  linuxDesktopEntry: 'linuxDesktopEntry',
} as const
