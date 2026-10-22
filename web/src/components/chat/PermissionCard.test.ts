import { beforeEach, afterEach, it, expect, vi } from 'vitest'
import { mount, unmount, flushSync } from 'svelte'
import { activeSessionId, confirmModals } from '../../lib/stores'
import { locale } from '../../lib/i18n'
import { ws } from '../../lib/ws'
import PermissionCard from './PermissionCard.svelte'
vi.mock('../../lib/ws',()=>({ws:{answerConfirmation:vi.fn(),interrupt:vi.fn()}}))
let app: ReturnType<typeof mount>;let target:HTMLElement
const pending=(id='one',sessionId='a')=>({id,sessionId,kind:'yes_no_always',message:'approve?',command:'echo hello'})
beforeEach(()=>{vi.clearAllMocks();locale.set('en');activeSessionId.set('a');confirmModals.set({a:pending()});target=document.createElement('div');document.body.append(target);app=mount(PermissionCard,{target});flushSync()})
afterEach(()=>{unmount(app);target.remove();confirmModals.set({})})
const button=(text:string)=>[...target.querySelectorAll('button')].find(x=>x.textContent?.toLowerCase().includes(text.toLowerCase()))!
it('is inline, does not capture Enter, and approves only via an explicit action',()=>{
 expect(target.querySelector('[aria-modal]')).toBeNull()
 target.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true}))
 expect(ws.answerConfirmation).not.toHaveBeenCalled()
 button('Allow once').click();flushSync()
 expect(ws.answerConfirmation).toHaveBeenCalledWith('one','yes')
 expect(target.textContent).not.toContain('approve?')
})
it('preserves scoped remember and explicit deny semantics',()=>{
 button('Remember this operation').click();expect(ws.answerConfirmation).toHaveBeenCalledWith('one','always')
 confirmModals.set({a:pending('two')});flushSync();button('Deny').click()
 expect(ws.answerConfirmation).toHaveBeenLastCalledWith('two','deny')
})
it('ignores a stale card after switching sessions or expiration',()=>{
 const old=button('Allow once');activeSessionId.set('b');confirmModals.set({b:pending('new','b')});old.click()
 expect(ws.answerConfirmation).not.toHaveBeenCalled()
 flushSync();const expired=button('Allow once');confirmModals.set({});expired.click()
 expect(ws.answerConfirmation).not.toHaveBeenCalled()
})
it('stops the session without sending an allow or deny',()=>{
 button('Stop task').click();expect(ws.interrupt).toHaveBeenCalledWith('a');expect(ws.answerConfirmation).not.toHaveBeenCalled()
})
