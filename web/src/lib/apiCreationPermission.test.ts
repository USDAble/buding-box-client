import { afterEach, expect, it, vi } from 'vitest'
import { createSession } from './api'
afterEach(()=>vi.unstubAllGlobals())
it('submits the Web default atomically and preserves an explicit strict choice',async()=>{
 const fetcher=vi.fn<typeof fetch>(async()=>new Response(JSON.stringify({session:{id:'created'}}),{status:200}))
 vi.stubGlobal('fetch',fetcher)
 await createSession({model:'a'});await createSession({model:'b',permission_mode:'strict'})
 expect(JSON.parse(String(fetcher.mock.calls[0][1]?.body))).toMatchObject({model:'a',permission_mode:'auto'})
 expect(JSON.parse(String(fetcher.mock.calls[1][1]?.body))).toMatchObject({model:'b',permission_mode:'strict'})
})
