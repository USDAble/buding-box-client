import { brandText } from './brand'

export type SessionTitleLike = {
  name?: unknown
  title?: unknown
} | null | undefined

// OCTO-FORK: keep the upstream persisted value as a compatibility sentinel,
// but never expose that old product name on this fork's user-facing surfaces.
const upstreamDefaultTitle = '*Octo Agent'

/** Returns the first real session label, or the configured product placeholder. */
export function sessionDisplayTitle(
  session: SessionTitleLike,
  locale: string,
  fallback = '',
): string {
  const candidates = [session?.name, session?.title]
  let hasUpstreamPlaceholder = false

  for (const value of candidates) {
    if (typeof value !== 'string') continue
    const title = value.trim()
    if (!title) continue
    if (title === upstreamDefaultTitle) {
      hasUpstreamPlaceholder = true
      continue
    }
    return title
  }

  if (hasUpstreamPlaceholder) {
    return brandText('sessionDefaultTitle', locale) || fallback
  }
  return fallback
}
