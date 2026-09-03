import config from './brand.config.json'

export const brand = config

export function mobileBrandName(locale = navigator.language): string {
  const names = brand.product.names as Record<string, string>
  if (names[locale]) return names[locale]
  const language = locale.split('-')[0]
  const regional = Object.keys(names).find((key) => key.startsWith(`${language}-`))
  return names[regional ?? 'en-US'] ?? names['en-US']
}
