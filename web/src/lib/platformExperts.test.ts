import { afterEach, describe, expect, it, vi } from 'vitest'
import { listAgents, listPlatformSkills } from './api'
import { summonAgent, openAgentSession, pendingModel, activeSessionId, sessions } from './stores'
import { selectableModels, defaultSelectableModel, type SelectableModel } from './selectableModels'

const modelRow = (id: string): SelectableModel => ({id,modelId:id,vendorId:'gateway',vendorName:'Gateway',displayName:'User model',source:'catalog',trust:'signed-catalog',confidential:false,sourceOrder:0,eligible:true})

afterEach(() => { vi.unstubAllGlobals(); selectableModels.set([]); defaultSelectableModel.set(''); pendingModel.set(''); activeSessionId.set(null); sessions.set([]) })
describe('platform expert catalog', () => {
  it('loads experts without loading or binding a model catalog and preserves local users', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      const responses: Record<string, unknown> = {
        '/api/agents': [{id:'template',source:'default'}, {id:'mine',source:'user',name:'Mine'}],
        '/api/product/experts': {experts:[{id:'expert',name:'Published',description:'real',version:4,default_model_id:'model',required_capabilities:['tools'],skills:[{id:'skill',name:'Skill',version:2}]}]},
        '/api/product/skills': {skills:[{id:'skill',name:'Skill',content:'instruction',version:2}]},
      }
      return new Response(JSON.stringify(responses[path]), {status:200})
    }))
    const result = await listAgents()
    expect(result.map(a => a.id)).toEqual(['platform:expert:4','mine'])
    expect(result[0]).toMatchObject({source:'platform', required_capabilities:['tools'], platform_skills:[{name:'Skill'}]})
    expect(result[0].model).toBeUndefined()
    expect(result[0].enabled).toBe(true)
    expect(vi.mocked(fetch).mock.calls.some(([path]) => String(path).includes('/models'))).toBe(false)
    expect((await listPlatformSkills())[0].content).toBe('instruction')
  })
  it('opens an expert with the chosen model using one local creation request and no extra directory/detail fetch', async () => {
    pendingModel.set('gateway::chosen-by-user'); activeSessionId.set(null); sessions.set([])
    selectableModels.set([modelRow('gateway::chosen-by-user')])
    const mocked = vi.fn(async (_path: string, _init?: RequestInit) => new Response(JSON.stringify({id:'new-task'}),{status:200}))
    vi.stubGlobal('fetch', mocked)
    await summonAgent('platform:expert:4','Published')
    expect(mocked).toHaveBeenCalledTimes(1)
    expect(mocked.mock.calls[0][0]).toBe('/api/sessions')
    expect(JSON.parse(mocked.mock.calls[0][1]?.body as string)).toMatchObject({agent_profile:'platform:expert:4',model:'gateway::chosen-by-user'})
    pendingModel.set(''); activeSessionId.set(null); sessions.set([])
  })
  it('opens an expert after startup with no pending or active model using the signed catalog default', async () => {
    selectableModels.set([modelRow('gateway::default')]); defaultSelectableModel.set('gateway::default')
    const mocked = vi.fn(async (_path: string, _init?: RequestInit) => new Response(JSON.stringify({id:'new-task'}),{status:200}))
    vi.stubGlobal('fetch',mocked)
    await summonAgent('platform:expert:4','Published')
    expect(mocked).toHaveBeenCalledTimes(1)
    expect(JSON.parse(mocked.mock.calls[0][1]?.body as string)).toMatchObject({agent_profile:'platform:expert:4',model:'gateway::default'})
  })
  it('loads the cold catalog once before creating a skill task with an explicit model', async () => {
    const mocked = vi.fn(async (path: string, _init?: RequestInit) => new Response(JSON.stringify(path === '/api/product/models'
      ? {state:'fresh',defaultModelId:'gateway::ready',vendors:[{id:'gateway',models:[{id:'ready',compositeId:'gateway::ready',eligible:true}]}]}
      : {id:'skill-task'}),{status:200}))
    vi.stubGlobal('fetch',mocked)
    await openAgentSession('Use selected skill','Skill')
    expect(mocked.mock.calls.map(([path])=>path)).toEqual(['/api/product/models','/api/sessions'])
    expect(JSON.parse(mocked.mock.calls[1][1]?.body as string).model).toBe('gateway::ready')
  })
  it('surfaces a platform outage instead of presenting bundled templates as published experts', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({message:'unavailable'}),{status:503})))
    await expect(listAgents()).rejects.toThrow('unavailable')
  })
})
