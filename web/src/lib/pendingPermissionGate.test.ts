import { expect, it, vi } from 'vitest'
import { createPendingPermissionGate, confirmPendingPermissionChoice } from './pendingPermissionGate'
import source from '../views/ChatView.svelte?raw'
// OCTO-FORK: permission persistence failures must not turn strict selection into auto execution.
it('blocks sending on failure, retries the same session and accepts a confirmed replacement', async () => {
 const apply = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue(undefined)
 const gate = createPendingPermissionGate(apply)
 const send = vi.fn(), restore = vi.fn()
 const attempt = async () => { try { await gate.ensure('session'); send() } catch { restore('draft') } }
 gate.require('session', 'strict')
 await attempt()
 expect(send).not.toHaveBeenCalled(); expect(restore).toHaveBeenCalledWith('draft')
 await attempt(); expect(send).toHaveBeenCalledOnce(); expect(apply).toHaveBeenLastCalledWith('session', 'strict')
 gate.require('session', 'strict'); confirmPendingPermissionChoice('session'); await gate.ensure('session'); expect(apply).toHaveBeenCalledTimes(2)
})
it('returns through the composer restoration path before any websocket send', () => {
 expect(source).toContain('pendingPermissionGate.require(newSess.id, permPick)')
 expect(source).toMatch(/pendingPermissionGate.require\(newSess.id, permPick\)[\s\S]*?return null/)
 expect(source).toMatch(/if \(!active\) \{[\s\S]*?composer\?\.restore\(text, files\)[\s\S]*?return/)
})
