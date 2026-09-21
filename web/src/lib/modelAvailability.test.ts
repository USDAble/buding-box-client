import { beforeEach, describe, expect, it } from 'vitest'
import { get } from 'svelte/store'
import { allowEnvironmentModelSource, catalogState } from './product'
import { canStartTurn, catalogNoticeKey, sessionCatalogModelWithdrawn } from './modelAvailability'
import { selectableModels, type SelectableModel } from './selectableModels'
import { chatModel, sessions } from './stores'
import type { Session } from './types'

const PREFIX = 'gateway::'

function catalogModel(id: string): SelectableModel {
  return {
    id: PREFIX + id,
    vendorId: 'vendor',
    vendorName: 'Vendor',
    modelId: id,
    displayName: id,
    source: 'catalog',
    confidential: false,
    trust: 'signed-catalog',
    sourceOrder: 0,
  }
}

beforeEach(() => {
  allowEnvironmentModelSource.set(false)
  catalogState.set('ready')
  selectableModels.set([catalogModel('current')])
  chatModel.set({})
  sessions.set([])
})

describe('product model availability', () => {
  it('fails closed when the signed catalog is unavailable or empty', () => {
    for (const [state, key] of [
      ['absent', 'catalog.absent'],
      ['stale', 'catalog.stale'],
      ['unverifiable', 'catalog.unverifiable'],
    ] as const) {
      catalogState.set(state)
      expect(canStartTurn()).toBe(false)
      expect(catalogNoticeKey()).toBe(key)
    }
    catalogState.set('ready')
    selectableModels.set([])
    expect(canStartTurn()).toBe(false)
    expect(catalogNoticeKey()).toBe('catalog.no_models')
  })

  it('blocks a withdrawn catalog binding without silently changing it', () => {
    chatModel.set({ s1: PREFIX + 'withdrawn' })
    const before = get(chatModel)
    expect(sessionCatalogModelWithdrawn('s1')).toBe(true)
    expect(canStartTurn('s1')).toBe(false)
    expect(catalogNoticeKey('s1')).toBe('session.model_withdrawn')
    expect(get(chatModel)).toEqual(before)
  })

  it('reads a binding from the session list and accepts a current model', () => {
    sessions.set([{ id: 's1', model_id: PREFIX + 'current' } as Session])
    expect(sessionCatalogModelWithdrawn('s1')).toBe(false)
    expect(canStartTurn('s1')).toBe(true)
  })

  it('does not claim that a developer-local binding was withdrawn', () => {
    chatModel.set({ s1: 'local::model' })
    expect(sessionCatalogModelWithdrawn('s1')).toBe(false)
  })

  it('does not permit a new turn when all listed models are ineligible', () => {
    selectableModels.set([{ ...catalogModel('current'), eligible: false }])
    expect(canStartTurn()).toBe(false)
    chatModel.set({ s1: PREFIX + 'current' })
    expect(sessionCatalogModelWithdrawn('s1')).toBe(false)
    expect(catalogNoticeKey('s1')).toBe('session.model_unavailable')
  })
})

describe('developer model availability', () => {
  it('does not let a missing product catalog disable local testing', () => {
    allowEnvironmentModelSource.set(true)
    catalogState.set('absent')
    selectableModels.set([])
    expect(canStartTurn()).toBe(true)
  })
})
