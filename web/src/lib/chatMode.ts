// P9 chat-mode client: the mode→model grouping the selector renders, and the
// per-session mode attribute. OCTO-FORK: P9 模式与模型选择器 — see
// dev-docs-usdable/需求/2260906/技术方案/P9-模式与模型.md §4.
// 数据源自 PR-4d 起是中台签名目录；分组与模型名都来自它（需求基线 B5/B6）.
import { writable, get } from 'svelte/store'
import { tr, locale } from './i18n'
import { getChatModes, setSessionChatMode, updateSessionModel, type ChatModeDTO, type ChatModeModel } from './api'
import { catalogState, refreshCatalogState } from './product'
import { chatMode, chatModel, sessions } from './stores'

export type ChatMode = 'privacy' | 'smart' | 'default'

// Shared by P10's composer/sidebar affordances so every surface follows the
// same exact mode check. OCTO-FORK: P10 隐私模式与PII 处理 — see
// dev-docs-usdable/需求/2260906/技术方案/P10-隐私模式与PII.md.
export function isPrivacyMode(mode: string | null | undefined): boolean {
  return mode === 'privacy'
}

// The loaded mode list: the signed catalog, grouped by the product's modes.
export const chatModes = writable<ChatModeDTO[]>([])

// loadChatModes refreshes the grouping. Called when the mode menu opens, so the
// user sees a catalog refresh without a reload.
//
// There is nothing to "fall back" to and no flag saying we did: 需求基线 B1 规则 4
// retired the local chat-modes.json and the built-in list with it, so a目录 that
// cannot be read leaves the previous list in place rather than replacing it with
// an invented one. The user-visible wording for that state is PR-4c's.
//
// PR-4c's half is the two reads the menu now performs together, and the order is
// load-bearing: the availability read is what renews a lapsed catalog (B3), so it
// goes first and the list below it is the one that was just renewed. Reading the
// list first would show the user the pre-refresh list and then explain, from a
// fresh state, why it is empty.
export async function loadChatModes(): Promise<void> {
  await refreshCatalogState()
  try {
    const d = await getChatModes()
    chatModes.set(d.modes ?? [])
  } catch {
    // Keep the previous list on error (the menu still opens).
  }
}

// catalogNoticeKey is the ONE place "why is there nothing to pick / why can I not
// send" becomes a sentence (开发规范 §3.8, 需求基线 B9).
//
// B9 wants four distinct sentences, and it also says the two families must not
// share one: an unavailable catalog is a network or trust problem the user may be
// able to act on, while an empty group is simply a group with no models in it.
// The split is by judgement source, not by which component renders it — three of
// the four are decided by catalogState alone (server-side, one owner) and the
// fourth only when the catalog is fine and the projection still came up empty.
// Deciding it inside each component is how the picker ends up saying "the catalog
// is broken" while the banner says "this group is empty".
export function catalogNoticeKey(): string {
  const state = get(catalogState)
  if (state === 'absent') return 'catalog.absent'
  if (state === 'stale') return 'catalog.stale'
  if (state === 'unverifiable') return 'catalog.unverifiable'
  return 'mode.no_models'
}

// setSessionMode persists the session's mode and, when a concrete model is
// supplied, binds the session to it. The selector is two-level: picking a
// model under a mode group sets BOTH that mode and that model (需求 §5.6 规则
// 4/6 — only this session changes; the account default and other sessions are
// untouched). Both the sessions array and the per-session stores are patched
// so the Composer chip and ModeMenu highlight stay in sync without a refetch.
export async function setSessionMode(sid: string, mode: ChatMode, modelId?: string): Promise<void> {
  // Persist the mode first, then the model — the two are independent session
  // attributes (需求 §5.6 规则 7: only affects subsequent messages).
  await setSessionChatMode(sid, mode)
  chatMode.update(m => ({ ...m, [sid]: mode }))
  sessions.update(list => list.map(s => (s.id === sid ? { ...s, chat_mode: mode } : s)))

  if (modelId) {
    const res = await updateSessionModel(sid, modelId)
    chatModel.update(mx => ({ ...mx, [sid]: res.model }))
    sessions.update(list => list.map(s => (s.id === sid ? { ...s, model: res.model, model_id: res.model_id } : s)))
  }
}

// modeDisplayName localises a mode id. The mode's own name is interface copy, not
// catalog data (需求基线 B5 规则 1), so it stays in i18n and comes from here.
// An id with no copy falls back to the raw id rather than to another mode's name.
export function modeDisplayName(id: string): string {
  const key = `mode.${id}`
  const v = tr(key)
  return v === key ? id : v
}

// modelDisplayName renders the catalog's name for a model (需求基线 B6 规则 2):
// the current interface language, then English, then the raw id.
//
// The three tiers are the whole of the fallback, and there is deliberately no
// fourth one into a local table. A table would shadow the server's value - the
// platform renames a model and the interface keeps showing the old name - which
// is why the catalog is the only source and PR-4d deleted the eight `model.*`
// i18n keys that used to be it.
//
// Falling back to the id is for a catalog that ships only one language, and for
// debug/test output. It is not a licence to render ids in the normal path: the
// catalog always carries both languages, so a missing one is contract drift.
export function modelDisplayName(model: ChatModeModel | string): string {
  // The string form is the debug/test shim: callers with only an id get the id
  // back. It is not a path the production render takes.
  if (typeof model === 'string') return model
  const current = get(locale)
  const inCurrentLanguage = dictLocaleIsChinese(current) ? model.displayName.zh : model.displayName.en
  return inCurrentLanguage || model.displayName.en || model.id
}

// dictLocaleIsChinese mirrors i18n.dictFor's rule: any zh-* interface language is
// served by the Simplified dictionary, so it must also pick the zh model name.
// Keeping the two in step matters more than the branch being clever: an interface
// rendering Chinese copy beside English model names is the visible half of a
// mismatch whose other half is in i18n.ts.
function dictLocaleIsChinese(l: string): boolean {
  return l === 'zh' || l.startsWith('zh')
}
