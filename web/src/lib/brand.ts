import config from './brand.config.json'

export type Locale = string
export type Localized = Record<string, string>

export interface BrandConfig {
  schemaVersion: number
  brandId: string
  product: {
    names: Localized
    shortName: Localized
    category: Localized
    tagline: Localized
    description: Localized
  }
  about: {
    teamName: Localized
    copyright: Localized
    contactEmail: string
    supportEmail: string
    securityEmail: string
    license: string
    notice: Localized
  }
  links: Record<string, string>
  visual: {
    logo: { mark: string; favicon: string; temporary: boolean }
    colors: Record<string, string>
  }
  compatibility: {
    cliCommand: string
    configDir: string
    environmentPrefix: string
    uiProtocol: string
    pairingProtocol: string
    goModule: string
  }
  futureMigration: Record<string, string>
  copy: Record<string, Localized>
}

export const brand = config as BrandConfig

function localized(values: Localized, locale = 'en-US'): string {
  if (values[locale]) return values[locale]
  const language = locale.split('-')[0]
  const regional = Object.keys(values).find((key) => key.startsWith(`${language}-`))
  return values[regional ?? 'en-US'] ?? Object.values(values)[0] ?? ''
}

export function brandName(locale: Locale): string {
  return localized(brand.product.names, locale)
}

export function brandText(key: string, locale: Locale): string {
  const copy = brand.copy[key]
  if (copy) return localized(copy, locale)

  // Keep frequently reused product copy available through the same helper even
  // when a future config author places it under product instead of copy.
  const productCopy = brand.product[key as keyof BrandConfig['product']]
  return productCopy && typeof productCopy === 'object' ? localized(productCopy as Localized, locale) : ''
}

export function brandUrl(key: string): string {
  return brand.links[key] ?? ''
}

export function brandLocale(locale: Locale): string {
  return locale.startsWith('zh') ? 'zh-CN' : locale
}
