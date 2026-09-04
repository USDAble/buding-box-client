import { derived } from 'svelte/store'
import { locale, setLocale } from '../../../web/src/lib/i18n'
import en from './i18n/en.json'
import zh from './i18n/zh.json'

export type SupportedLanguage = 'en' | 'zh'

const LANGUAGE_STORAGE_KEY = 'buding_box_portable_language'

export const portableLanguages: { value: SupportedLanguage; label: string }[] = [
  { value: 'en', label: 'English' },
  { value: 'zh', label: '简体中文' },
]

export type TranslationKey = keyof typeof en
const translations: Record<SupportedLanguage, Record<TranslationKey, string>> = { en, zh }
let localePersistenceInitialized = false

export const portableLocale = derived(locale, (value): SupportedLanguage =>
  value.startsWith('zh') ? 'zh' : 'en',
)

export const portableT = derived(portableLocale, (language) =>
  (key: TranslationKey): string => translations[language][key],
)

function persistPortableLanguage(language: SupportedLanguage): void {
  localStorage.setItem(LANGUAGE_STORAGE_KEY, language)
  document.documentElement.lang = language === 'zh' ? 'zh-CN' : 'en'
}

export function setPortableLanguage(language: SupportedLanguage): void {
  persistPortableLanguage(language)
  setLocale(language)
}

export function initializePortableI18n(): void {
  const stored = localStorage.getItem(LANGUAGE_STORAGE_KEY)
  const initialLanguage = stored === 'en' || stored === 'zh' ? stored : 'zh'
  setPortableLanguage(initialLanguage)

  // 上游设置弹窗挂载时会先写入自身默认语言；等该初始化结束后恢复 U 盘值，再监听真实切换。
  if (!localePersistenceInitialized) {
    localePersistenceInitialized = true
    queueMicrotask(() => {
      setPortableLanguage(initialLanguage)
      portableLocale.subscribe(persistPortableLanguage)
    })
  }
}
