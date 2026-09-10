// Product identity for the mobile boot layer. Mirrors the localize() logic in
// web/src/lib/brand.ts but trimmed to what boot.ts needs: the pairing gate runs
// before the frontend loads, so it cannot read the frontend's i18n store and
// resolves the name against the device language instead.
import config from './brand.config.json'

interface BrandConfig {
  product: {
    names: Record<string, string>
    shortName: Record<string, string>
  }
}

const brand = config as BrandConfig

// brandName resolves a locale to the product name with the same three-level
// fallback as the web layer: exact tag, then bare language prefix, then en-US,
// then any available value.
export function brandName(locale: string): string {
  const names = brand.product.names
  const exact = names[locale]
  if (exact) return exact

  const language = locale.split('-')[0]
  if (language) {
    for (const [tag, value] of Object.entries(names)) {
      if (value && (tag === language || tag.startsWith(`${language}-`))) return value
    }
  }

  return names['en-US'] ?? Object.values(names).find(Boolean) ?? ''
}
