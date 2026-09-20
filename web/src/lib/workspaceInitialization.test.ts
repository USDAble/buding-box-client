import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import { initializeWorkspace, productPhase, productState, refreshCatalogState, refreshCredits, workspaceModules, workspaceDictionaryFallback } from './product'
const state = { schemaVersion: 1, loggedIn: true, activated: true, account: {nickname:'Current'}, credits:{balance:5,known:true},plan:{name:''},prefs:{locale:'zh',inputSensitiveCheck:true},suppressOnboarding:true }
function response(unavailable = false) {return new Response(JSON.stringify({state, modules:{catalog: unavailable?'unavailable':'ready',credits:unavailable?'unavailable':'ready',box:'ready',dictionary:unavailable?'unavailable':'ready'},catalog:{state:unavailable?'absent':'ready',retryable:true},dictionaryNotice:unavailable?{state:'degraded',fallbackVersion:''}:undefined}))}
beforeEach(()=>{productPhase.set('blocked');productState.set(state as never);productPhase.set('ready');sessionStorage.setItem('octo_window_token','test')})
afterEach(()=>{vi.unstubAllGlobals();sessionStorage.clear();productPhase.set('blocked')})
it('clears failed modules after individual reads recover and clears dictionary fallback on retry',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValueOnce(response(true)).mockResolvedValueOnce(new Response(JSON.stringify({state:'ready'}))).mockResolvedValueOnce(new Response(JSON.stringify({state:{...state,credits:{balance:8,known:true}}}))));
 await initializeWorkspace();expect(get(workspaceDictionaryFallback)).toBe(true)
 await refreshCatalogState();await refreshCredits();expect(get(workspaceModules)).toEqual({catalog:'ready',credits:'ready',box:'ready',dictionary:'unavailable'})
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(response()));await initializeWorkspace();expect(get(workspaceModules).dictionary).toBe('ready');expect(get(workspaceDictionaryFallback)).toBe(false)
})
it('ignores a superseded unauthorized response without ending a newer session',async()=>{
 let resolve!:(response:Response)=>void
 vi.stubGlobal('fetch',vi.fn().mockImplementationOnce(()=>new Promise<Response>(r=>resolve=r)).mockResolvedValueOnce(response()))
 const old=initializeWorkspace().catch(error=>error);await initializeWorkspace();resolve(new Response('{}',{status:401}));expect(await old).toBeInstanceOf(Error);expect(get(productPhase)).toBe('ready');expect(get(workspaceModules).catalog).toBe('ready')
})
it('does not overwrite a newer independent balance with a pending initialization snapshot',async()=>{
 let resolve!:(response:Response)=>void
 vi.stubGlobal('fetch',vi.fn().mockImplementationOnce(()=>new Promise<Response>(r=>resolve=r)).mockResolvedValueOnce(new Response(JSON.stringify({state:{...state,credits:{balance:9,known:true}}}))))
 const pending=initializeWorkspace();await refreshCredits();resolve(response(true));await pending;expect(get(productState)?.credits.balance).toBe(9);expect(get(workspaceModules).credits).toBe('ready')
})
it('keeps the login after temporary network failure and ignores cancelled results',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockRejectedValue(new Error('offline')));await expect(initializeWorkspace()).rejects.toThrow('offline');expect(get(productPhase)).toBe('ready')
 const controller=new AbortController();vi.stubGlobal('fetch',vi.fn().mockResolvedValue(response(true)));controller.abort();await expect(initializeWorkspace(controller.signal)).rejects.toThrow('cancelled');expect(get(workspaceModules)).toEqual({})
})
