import { expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import { defaultSelectableModel, loadSelectableModels, selectableModels } from './selectableModels'

const { getProductModels } = vi.hoisted(() => ({ getProductModels: vi.fn() }))
vi.mock('./api', () => ({ getProductModels }))

it('shares the displayed platform default with new-session creation and clears it when unavailable', async () => {
  getProductModels.mockResolvedValue({
    state: 'ready', catalogVersion: '1', defaultModelId: 'gateway::second',
    vendors: [{ id: 'vendor', displayName: 'Vendor', models: [
      { id: 'first', compositeId: 'gateway::first', displayName: 'First' },
      { id: 'second', compositeId: 'gateway::second', displayName: 'Second' },
    ] }],
  })
  const shown = await loadSelectableModels(false)
  expect(shown.defaultModelId).toBe('gateway::second')
  expect(get(defaultSelectableModel)).toBe(shown.defaultModelId)
  getProductModels.mockResolvedValue({ state: 'absent', catalogVersion: '', vendors: [] })
  await loadSelectableModels(false)
  expect(get(defaultSelectableModel)).toBe('')
})

it('displays all configured models but excludes unpriced models from sending and defaults', async () => {
  getProductModels.mockResolvedValue({ state: 'ready', catalogVersion: '2', defaultModelId: 'gateway::pro', vendors: [
    { id: 'vendor', models: [
      { id: 'flash', compositeId: 'gateway::flash', eligible: true },
      { id: 'vision', compositeId: 'gateway::vision', eligible: false, availabilityReason: 'pricing_not_configured' },
      { id: 'pro', compositeId: 'gateway::pro', eligible: false, availabilityReason: 'pricing_not_configured' },
    ] },
  ] })
  const shown = await loadSelectableModels(false)
  expect(shown.models).toHaveLength(3)
  expect(shown.models[1].availabilityReason).toBe('pricing_not_configured')
  expect(get(selectableModels).map(model => model.id)).toEqual(['gateway::flash'])
  expect(get(defaultSelectableModel)).toBe('gateway::flash')
})
