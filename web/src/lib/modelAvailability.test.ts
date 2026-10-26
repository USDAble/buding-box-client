import { beforeEach, describe, expect, it } from 'vitest'
import { get } from 'svelte/store'
import { allowEnvironmentModelSource, catalogState } from './product'
import { canStartTurn, catalogNoticeKey, sessionCatalogModelWithdrawn, modelUnavailableKeys } from './modelAvailability'
import { locale, tr } from './i18n'
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

// OCTO-FORK: a signed operational reason must be understandable and actionable,
// including older reason codes and future codes from newer platform versions.
describe('model unavailability wording', () => {
  it.each([
    ['supplier_disabled', 'model.supplier_disabled'],
    ['channel_disabled', 'model.channel_disabled'],
    ['account_disabled', 'model.account_unavailable'],
    ['missing_key', 'model.account_unavailable'],
    ['account_unhealthy', 'model.account_unavailable'],
    ['account_cooldown', 'model.account_unavailable'],
    ['route_disabled', 'model.route_disabled'],
    ['weight_zero', 'model.route_disabled'],
    ['test_required', 'model.verification_required'],
    ['capability_not_verified', 'model.capability_unverified'],
    ['capability_unverified', 'model.capability_unverified'],
    ['capability_unsupported', 'model.capability_unsupported'],
    ['model_unpublished', 'model.unpublished'],
    ['upstream_missing', 'model.no_available_source'],
    ['endpoint_missing', 'model.no_available_source'],
    ['no_available_source', 'model.no_available_source'],
    ['pricing_not_configured', 'model.pricing_not_configured'],
    ['price_missing', 'model.pricing_not_configured'],
    ['capacity_full', 'model.busy'],
    ['new_reason_from_platform', 'model.unavailable'],
  ])('localizes %s with a next step', (reason, expected) => {
    const keys = modelUnavailableKeys(reason)
    expect(keys[0]).toBe(expected)
    for (const language of ['en', 'zh', 'zh-Hant'] as const) {
      locale.set(language)
      for (const key of keys) {
        expect(tr(key)).not.toBe(key)
        expect(tr(key)).not.toBe('')
      }
    }
  })
})
