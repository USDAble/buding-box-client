import { getCountries, getCountryCallingCode, type CountryCode } from 'libphonenumber-js/min'

export type DialCodeOption = { region: CountryCode; code: string; name: string }

// OCTO-FORK: show the full international dialing-code catalog in the compact
// activation picker. Sending remains subject to the server's configured
// destination allowlist and the provider's current country coverage.
export function dialCodeOptions(locale: string, query = ''): DialCodeOption[] {
  const language = locale === 'zh' ? 'zh-CN' : 'en'
  const names = new Intl.DisplayNames([language], { type: 'region' })
  const search = query.trim().toLocaleLowerCase(language)
  const options = getCountries().map((region) => ({
    region,
    code: `+${getCountryCallingCode(region)}`,
    name: names.of(region) ?? region,
  }))
  const filtered = search
    ? options.filter((option) => `${option.code} ${option.name} ${option.region}`.toLocaleLowerCase(language).includes(search))
    : options
  const collator = new Intl.Collator(language)
  return filtered.sort((a, b) => (a.region === 'CN' ? -1 : b.region === 'CN' ? 1 : collator.compare(a.name, b.name)))
}
