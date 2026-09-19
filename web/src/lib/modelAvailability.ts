// OCTO-FORK: one owner for the pre-turn model-availability judgement. Product
// profiles fail closed on the signed catalog; developer profiles may keep using
// explicitly configured local endpoints.
import { get } from 'svelte/store'
import { allowEnvironmentModelSource, catalogState } from './product'
import { selectableModels } from './selectableModels'
import { chatModel, sessions } from './stores'

export function canStartTurn(sid?: string | null): boolean {
  const catalogModels = get(selectableModels).filter(model => model.source === 'catalog')
  if (get(allowEnvironmentModelSource) === false) {
    if (get(catalogState) !== 'ready' || catalogModels.length === 0) return false
  }
  return !sessionCatalogModelWithdrawn(sid)
}

export function catalogNoticeKey(sid?: string | null): string {
  if (get(allowEnvironmentModelSource) === false) {
    const state = get(catalogState)
    if (state === 'absent') return 'catalog.absent'
    if (state === 'stale') return 'catalog.stale'
    if (state === 'unverifiable') return 'catalog.unverifiable'
  }
  if (sessionCatalogModelWithdrawn(sid)) return 'session.model_withdrawn'
  return 'catalog.no_models'
}

export function sessionCatalogModelWithdrawn(sid?: string | null): boolean {
  if (!sid) return false
  const bound = get(chatModel)[sid] || get(sessions).find(session => session.id === sid)?.model_id || ''
  if (!bound) return false

  const catalogModels = get(selectableModels).filter(model => model.source === 'catalog')
  const prefix = catalogCompositePrefix(catalogModels.map(model => model.id))
  if (!prefix || !bound.startsWith(prefix)) return false
  return !catalogModels.some(model => model.id === bound || model.modelId === bound)
}

function catalogCompositePrefix(ids: string[]): string {
  for (const id of ids) {
    const at = id.indexOf('::')
    if (at > 0) return id.slice(0, at + 2)
  }
  return ''
}
