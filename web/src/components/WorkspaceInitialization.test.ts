import { afterEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import { productPhase, productState, refreshCredits } from '../lib/product'
import { locale } from '../lib/i18n'
import WorkspaceInitialization from './WorkspaceInitialization.svelte'
let app:ReturnType<typeof mount>|undefined
let target:HTMLDivElement|undefined
const state={loggedIn:true,activated:true,credits:{balance:1,known:true},prefs:{locale:'zh'}}
function reply(failed:boolean){return new Response(JSON.stringify({state,catalog:{state:'ready'},modules:{catalog:'ready',credits:failed?'unavailable':'ready',box:'ready',dictionary:'ready'}}))}
afterEach(async()=>{if(app)await unmount(app);app=undefined;target?.remove();vi.unstubAllGlobals();sessionStorage.clear();productPhase.set('blocked')})
function render(){locale.set('zh');sessionStorage.setItem('octo_window_token','test');productPhase.set('blocked');productState.set(state as never);productPhase.set('ready');target=document.createElement('div');document.body.append(target);app=mount(WorkspaceInitialization,{target});flushSync()}
it('retries only on user request and removes the failure banner after success',async()=>{
 const fetchMock=vi.fn().mockResolvedValueOnce(reply(true)).mockResolvedValueOnce(reply(false));vi.stubGlobal('fetch',fetchMock);render()
 await vi.waitFor(()=>expect(target?.querySelector('button')).not.toBeNull());expect(fetchMock).toHaveBeenCalledTimes(1)
 target!.querySelector('button')!.click();flushSync();await vi.waitFor(()=>expect(target?.querySelector('[role="status"]')).toBeNull());expect(fetchMock).toHaveBeenCalledTimes(2)
})
it('removes a failed wallet banner when the account panel refresh succeeds',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValueOnce(reply(true)).mockResolvedValueOnce(new Response(JSON.stringify({state}))));render()
 await vi.waitFor(()=>expect(target?.querySelector('button')).not.toBeNull());await refreshCredits();flushSync();expect(target?.querySelector('[role="status"]')).toBeNull()
})
