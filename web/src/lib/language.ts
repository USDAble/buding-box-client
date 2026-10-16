import { get, writable } from 'svelte/store'
import { locale, setLocale, type Locale } from './i18n'
import { productState, setProductLocale } from './product'
import { updateLanguage } from './api'

// OCTO-FORK: both language pickers save through one serialized preference owner.
export const languageSaving = writable(false)

export async function saveLanguagePreference(next: Locale): Promise<void> {
  if (get(languageSaving)) return
  const previous = get(locale)
  let productSaved = false
  languageSaving.set(true)
  setLocale(next)
  try {
    if (get(productState)) {
      await setProductLocale(next)
      productSaved = true
    }
    await updateLanguage(next)
  } catch (error) {
    // A failed native mirror must not undo an already persisted product choice.
    if (!productSaved) setLocale(previous)
    throw error
  } finally {
    languageSaving.set(false)
  }
}
