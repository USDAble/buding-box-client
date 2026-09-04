import type { TranslationKey } from '../portable-i18n'

export interface CountryCode {
  iso: string
  dialCode: string
  labelKey: TranslationKey
}

// 常用国际电话区号静态表；只保存号码和翻译键，不包含任何用户数据。
export const countryCodes: CountryCode[] = [
  { iso: 'CN', dialCode: '+86', labelKey: 'country.cn' },
  { iso: 'HK', dialCode: '+852', labelKey: 'country.hk' },
  { iso: 'MO', dialCode: '+853', labelKey: 'country.mo' },
  { iso: 'TW', dialCode: '+886', labelKey: 'country.tw' },
  { iso: 'US', dialCode: '+1', labelKey: 'country.usCa' },
  { iso: 'GB', dialCode: '+44', labelKey: 'country.gb' },
  { iso: 'JP', dialCode: '+81', labelKey: 'country.jp' },
  { iso: 'KR', dialCode: '+82', labelKey: 'country.kr' },
  { iso: 'SG', dialCode: '+65', labelKey: 'country.sg' },
  { iso: 'AU', dialCode: '+61', labelKey: 'country.au' },
  { iso: 'DE', dialCode: '+49', labelKey: 'country.de' },
  { iso: 'FR', dialCode: '+33', labelKey: 'country.fr' },
]
