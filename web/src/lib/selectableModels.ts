// OCTO-FORK: one adapter owns the signed-catalog + developer-local model view.
// Composer and future model consumers must not merge these sources themselves.
import * as api from './api'
import { writable } from 'svelte/store'

export type SelectableModel = {
  id: string
  vendorId: string
  vendorName: string
  modelId: string
  displayName: string
  source: 'catalog' | 'local'
  confidential: boolean
  confidentialPriority?: number
  trust: 'signed-catalog' | 'developer-local'
  sourceOrder: number
}

export type SelectableModelResult = {
  state: api.ProductModelsState
  models: SelectableModel[]
  defaultModelId: string
}

// Shared read-only snapshot used by the send gate. The adapter is the only
// writer, so the picker and the gate judge the same catalog projection.
export const selectableModels = writable<SelectableModel[]>([])

export async function loadSelectableModels(allowLocal: boolean): Promise<SelectableModelResult> {
  let catalog: api.ProductModelsResponse = { state: 'absent', catalogVersion: '', vendors: [] }
  try {
    catalog = await api.getProductModels()
  } catch (error) {
    if (!allowLocal) throw error
  }
  const models: SelectableModel[] = []
  let sourceOrder = 0
  for (const vendor of catalog.vendors ?? []) {
    for (const model of vendor.models ?? []) {
      models.push({
        id: model.compositeId,
        vendorId: vendor.id,
        vendorName: vendor.displayName || vendor.id,
        modelId: model.id,
        displayName: model.displayName || model.id,
        source: 'catalog',
        confidential: model.confidential === true,
        confidentialPriority: model.confidentialPriority,
        trust: 'signed-catalog',
        sourceOrder: sourceOrder++,
      })
    }
  }

  let defaultModelId = ''
  if (allowLocal) {
    const endpoints = await api.getEndpoints()
    defaultModelId = endpoints.default?.includes('::') ? endpoints.default : ''
    for (const endpoint of endpoints.endpoints ?? []) {
      const vendorName = endpoint.name || endpoint.provider || endpoint.id
      for (const model of endpoint.models ?? []) {
        models.push({
          id: `${endpoint.id}::${model.model}`,
          vendorId: endpoint.id,
          vendorName,
          modelId: model.model,
          displayName: model.model,
          source: 'local',
          confidential: model.confidential === true,
          trust: 'developer-local',
          sourceOrder: sourceOrder++,
        })
      }
    }
  }

  const sorted = stableConfidentialSort(models)
  if (!defaultModelId) defaultModelId = sorted[0]?.id ?? ''
  selectableModels.set(sorted)
  return { state: catalog.state, models: sorted, defaultModelId }
}

export function stableConfidentialSort(models: SelectableModel[]): SelectableModel[] {
  const vendorOrder: string[] = []
  const byVendor = new Map<string, SelectableModel[]>()
  for (const model of models) {
    if (!byVendor.has(model.vendorId)) {
      vendorOrder.push(model.vendorId)
      byVendor.set(model.vendorId, [])
    }
    byVendor.get(model.vendorId)!.push(model)
  }
  vendorOrder.sort((a, b) => {
    const aPrivate = byVendor.get(a)!.some(model => model.confidential)
    const bPrivate = byVendor.get(b)!.some(model => model.confidential)
    if (aPrivate !== bPrivate) return aPrivate ? -1 : 1
    return Math.min(...byVendor.get(a)!.map(model => model.sourceOrder)) -
      Math.min(...byVendor.get(b)!.map(model => model.sourceOrder))
  })
  return vendorOrder.flatMap(vendorId => byVendor.get(vendorId)!.sort((a, b) => {
    if (a.confidential !== b.confidential) return a.confidential ? -1 : 1
    if (a.confidential && b.confidential) {
      const byPriority = (b.confidentialPriority ?? 0) - (a.confidentialPriority ?? 0)
      if (byPriority !== 0) return byPriority
    }
    return a.sourceOrder - b.sourceOrder
  }))
}
