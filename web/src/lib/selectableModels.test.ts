import { describe, expect, it } from 'vitest'
import { stableConfidentialSort, type SelectableModel } from './selectableModels'

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
