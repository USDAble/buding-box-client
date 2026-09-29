// OCTO-FORK: panel-created tasks need an explicit usable model just like the composer.
import { get } from 'svelte/store'
import { selectableModels, defaultSelectableModel, loadSelectableModels, type SelectableModel } from './selectableModels'
import { tr } from './i18n'

let loading: ReturnType<typeof loadSelectableModels> | null = null

function choose(models: SelectableModel[], defaultID: string, preferred: string[]): string {
  const usable = models.filter(row => row.eligible !== false)
  for (const id of preferred) if (id && usable.some(row => row.id === id)) return id
  return usable.find(row => row.id === defaultID && row.source === 'catalog')?.id
    ?? usable.find(row => row.source === 'catalog')?.id ?? ''
}

export async function resolveActionModel(preferred: string[]): Promise<string> {
  const cached = choose(get(selectableModels), get(defaultSelectableModel), preferred)
  if (cached) return cached
  // Only a cold picker needs one local signed-catalog projection read. Never
  // consult the upstream empty config.yml default or an expert recommendation.
  if (!loading) loading = loadSelectableModels(false).finally(() => { loading = null })
  const snapshot = await loading
  const model = choose(snapshot.models, snapshot.defaultModelId, preferred)
  // OCTO-FORK: this client-generated model error follows the selected UI language.
  if (!model) throw new Error(tr('models.none_available'))
  return model
}
