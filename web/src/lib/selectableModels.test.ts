import { afterEach, describe, expect, it, vi } from 'vitest'
import { stableConfidentialSort, modelReasoningOptions, loadSelectableModels, saveModelReasoning, selectableModels, type SelectableModel } from './selectableModels'

function model(overrides: Partial<SelectableModel>): SelectableModel {
  return {
    id: 'vendor::model',
    vendorId: 'vendor',
    vendorName: 'Vendor',
    modelId: 'model',
    displayName: 'Model',
    source: 'catalog',
    confidential: false,
    trust: 'signed-catalog',
    sourceOrder: 0,
    ...overrides,
  }
}

describe('stableConfidentialSort', () => {
  it('keeps vendors intact while moving confidential vendors and models first', () => {
    const rows = stableConfidentialSort([
      model({ id: 'a::ordinary', vendorId: 'a', modelId: 'ordinary', sourceOrder: 0 }),
      model({ id: 'b::ordinary', vendorId: 'b', modelId: 'ordinary', sourceOrder: 1 }),
      model({ id: 'b::private-low', vendorId: 'b', modelId: 'private-low', confidential: true, confidentialPriority: 10, sourceOrder: 2 }),
      model({ id: 'b::private-high', vendorId: 'b', modelId: 'private-high', confidential: true, confidentialPriority: 100, sourceOrder: 3 }),
    ])
    expect(rows.map(row => row.id)).toEqual([
      'b::private-high',
      'b::private-low',
      'b::ordinary',
      'a::ordinary',
    ])
  })

  it('preserves source order when confidential priorities tie or are absent', () => {
    const rows = stableConfidentialSort([
      model({ id: 'v::first', confidential: true, sourceOrder: 4 }),
      model({ id: 'v::second', confidential: true, sourceOrder: 5 }),
    ])
    expect(rows.map(row => row.id)).toEqual(['v::first', 'v::second'])
  })
})

// OCTO-FORK: persisted reasoning must outlive stale reads and failed writes.
import * as api from './api'
import { get } from 'svelte/store'

afterEach(() => { vi.restoreAllMocks(); selectableModels.set([]) })

describe('reasoning snapshot persistence', () => {
  const catalog = {state: 'ready' as const,catalogVersion:'test',vendors:[{id:'v',displayName:'V',models:[{id:'m',compositeId:'v::m',displayName:'M',confidential:false,reasoningOptions:['default','low','high'],reasoningEffort:'high'}]}]}
  it('projects only supported public choices and always retains model default', () => {
    expect(modelReasoningOptions(['max','off','high','low','low'])).toEqual(['default','low','high'])
    expect(modelReasoningOptions(['off'])).toEqual(['default'])
    expect(modelReasoningOptions()).toEqual(['default'])
  })
  it('does not let a catalog read started before an acknowledged save restore the old preference', async () => {
    const getModels = vi.spyOn(api, 'getProductModels').mockResolvedValue(catalog)
    vi.spyOn(api, 'setModelReasoning').mockResolvedValue({modelId:'m',effort:'low'})
    await loadSelectableModels(false)
    const selected = get(selectableModels)[0]
    let complete!: (value: typeof catalog) => void
    getModels.mockImplementationOnce(() => new Promise(resolve => { complete = resolve }))
    const read = loadSelectableModels(false)
    await saveModelReasoning(selected, 'low')
    complete(catalog); await read
    expect(get(selectableModels)[0].reasoningEffort).toBe('low')
  })
  it('keeps the old preference on rejection or a mismatched acknowledgement', async () => {
    vi.spyOn(api, 'getProductModels').mockResolvedValue(catalog)
    const save = vi.spyOn(api, 'setModelReasoning').mockRejectedValue(new Error('offline'))
    await loadSelectableModels(false); const selected = get(selectableModels)[0]
    await expect(saveModelReasoning(selected,'low')).rejects.toThrow('offline')
    save.mockResolvedValue({modelId:'other',effort:'low'})
    await expect(saveModelReasoning(selected,'low')).rejects.toThrow('reasoning_save_mismatch')
    expect(get(selectableModels)[0].reasoningEffort).toBe('high')
  })
})
