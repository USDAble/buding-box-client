// P9 chat-mode client: the mode→model grouping the selector renders, and the
// per-session mode attribute. OCTO-FORK: P9 模式与模型选择器 — see
// dev-docs-usdable/需求/2260906/技术方案/P9-模式与模型.md §4.
import { writable } from 'svelte/store'
import { tr } from './i18n'
import { getChatModes, setSessionChatMode, updateSessionModel, type ChatModeDTO } from './api'
import { chatMode, chatModel, sessions } from './stores'

export type ChatMode = 'privacy' | 'smart' | 'default'

// Shared by P10's composer/sidebar affordances so every surface follows the
// same exact mode check. OCTO-FORK: P10 隐私模式与 PII 处理.
export function isPrivacyMode(mode: string | null | undefined): boolean {
  return mode === 'privacy'
}

// The loaded mode list (chat-modes.json grouping, resolved against config.yml).
export const chatModes = writable<ChatModeDTO[]>([])
// True when the last load served the built-in default because the user's
// chat-modes.json was unreadable (需求 §9: hint once).
export const chatModesFallback = writable(false)

// loadChatModes refreshes the grouping. Called when the mode menu opens (the
// same "refetch on open" discipline the old model list used), so a user edit
// to data/chat-modes.json takes effect without a reload.
export async function loadChatModes(): Promise<void> {
  try {
    const d = await getChatModes()
    chatModes.set(d.modes ?? [])
    chatModesFallback.set(d.fallback)
  } catch {
    // Keep the previous list on error (the menu still opens with stale rows).
  }
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

// modeDisplayName localises a mode id (falls back to the raw id for an unknown
// group the user typed into chat-modes.json).
export function modeDisplayName(id: string): string {
  const key = `mode.${id}`
  const v = tr(key)
  return v === key ? id : v
}

// modelDisplayName localises a model id. Factory models buding-* have i18n
// keys; anything else falls back to the raw id (a real endpoint's model name).
export function modelDisplayName(id: string): string {
  const key = `model.${id}`
  const v = tr(key)
  return v === key ? id : v
}
