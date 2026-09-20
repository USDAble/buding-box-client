import {beforeEach,afterEach,expect,it,vi} from 'vitest'
import {mount,unmount,flushSync} from 'svelte'
import {locale} from '../../lib/i18n'
import {productPhase,productState} from '../../lib/product'
import WalletSection from './WalletSection.svelte'
vi.mock('qrcode',()=>({default:{toDataURL:vi.fn().mockResolvedValue('data:image/png;base64,AA==')}}))
let app:ReturnType<typeof mount>|undefined,target:HTMLDivElement,intent:any,order:any,paid=false,failOnce=false,enabled=true
let submissions:any[],walletReads:number
const state={loggedIn:true,activated:true,credits:{balance:700,known:true},prefs:{locale:'zh'}}
function json(value:any){return new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}})}
beforeEach(()=>{
 locale.set('zh');sessionStorage.setItem('octo_window_token','test');productState.set(state as never);productPhase.set('ready');intent=null;order=null;paid=false;failOnce=false;enabled=true;submissions=[];walletReads=0
 vi.stubGlobal('fetch',vi.fn(async(input:any,init:any={})=>{
 const path=String(input).split('?')[0],method=init.method||'GET'
 if(path.endsWith('/wallet')){walletReads++;return json({available_points:paid?'25700.0000':'700.0000',reserved_points:'0.0000',total_recharged_points:'25000.0000',total_consumed_points:'0.0000',status:'active'})}
 if(path.endsWith('/credits'))return json({state})
 if(path.endsWith('/recharge/options'))return json({items:[{id:'backend-tier-special',amount_cents:3799,currency:'CNY',points:'25000.0000',enabled:true}],payment_enabled:enabled,payment_methods:enabled?[{id:'wechat',name:'微信支付'}]:[]})
 if(path.endsWith('/recharge/intent')){if(method==='PUT')intent=JSON.parse(init.body);if(method==='DELETE')intent=null;return json(method==='GET'?{scope:'account-source-scope',intent}:intent)}
 if(path.endsWith('/recharge/orders')&&method==='GET')return json({items:order?[order]:[],has_more:false,next_cursor:''})
 if(path.endsWith('/recharge/orders')&&method==='POST'){
 const body=JSON.parse(init.body);submissions.push(body);order={id:'server-order-001',option_id:body.option_id,payment_method:body.payment_method,amount_cents:3799,currency:'CNY',points:'25000.0000',status:'pending',payment_url:'weixin://wxpay/bizpayurl?pr=isolated-test',expires_at:'2099-01-01T00:00:00Z',created_at:'2026-09-20T00:00:00Z',needs_review:false}
 if(failOnce){failOnce=false;throw new TypeError('response lost')};return json(order)
 }
 if(path.endsWith('/recharge/orders/server-order-001')){order={...order,status:paid?'paid':'pending',paid_at:paid?'2026-09-20T01:00:00Z':null};return json(order)}
 throw new Error('unexpected '+path)
 }))
 target=document.createElement('div');document.body.append(target)
})
afterEach(async()=>{if(app)await unmount(app);app=undefined;target.remove();vi.unstubAllGlobals();sessionStorage.clear();productPhase.set('blocked')})
function render(){app=mount(WalletSection,{target});flushSync()}
function button(text:string){const found=[...target.querySelectorAll('button')].find(b=>b.textContent?.includes(text));expect(found).toBeDefined();return found!}
async function choose(){await vi.waitFor(()=>expect(target.textContent).toContain('CNY 37.99'));button('25000').click();const select=target.querySelector('select')!;select.value='wechat';select.dispatchEvent(new Event('change',{bubbles:true}));flushSync()}
it('loads backend tiers even while real channels are unavailable',async()=>{enabled=false;render();await vi.waitFor(()=>expect(target.textContent).toContain('CNY 37.99'));expect(target.textContent).toContain('真实支付通道尚未配置');expect(target.querySelector('select')).toBeNull();expect([...target.querySelectorAll('button')].some(b=>b.textContent?.includes('创建支付订单'))).toBe(false);expect(button('25000').disabled).toBe(true);expect(submissions).toHaveLength(0)})
it('retries a lost reply with the same persisted key and waits for server paid before crediting',async()=>{
 failOnce=true;render();await choose();button('创建支付订单').click();await vi.waitFor(()=>expect(target.querySelector('[role="alert"]')).not.toBeNull());expect(intent.idempotencyKey).toBe(submissions[0].idempotencyKey)
 button('重试本次订单').click();await vi.waitFor(()=>expect(target.textContent).toContain('server-order-001'));expect(submissions).toHaveLength(2);expect(submissions[0]).toEqual(submissions[1]);expect(submissions[0]).toEqual({option_id:'backend-tier-special',payment_method:'wechat',idempotencyKey:expect.any(String),scope:'account-source-scope'})
 expect(target.textContent).not.toContain('服务端已确认支付及入账');expect(target.querySelector('img')).not.toBeNull();const before=walletReads;paid=true;button('查询支付结果').click();await vi.waitFor(()=>expect(target.textContent).toContain('服务端已确认支付及入账'));await vi.waitFor(()=>expect(walletReads).toBeGreaterThan(before));expect(intent).toBeNull()
 await unmount(app!);app=undefined;render();await vi.waitFor(()=>expect(target.textContent).toContain('已支付'));button('查看／继续').click();await vi.waitFor(()=>expect(target.textContent).toContain('server-order-001'))
})
it('does not allow creation until the existing intent can be read',async()=>{
 const original=globalThis.fetch
 vi.stubGlobal('fetch',vi.fn(async(input:any,init:any)=>String(input).endsWith('/recharge/intent')&&(!init?.method||init.method==='GET')?Promise.reject(new Error('unreadable intent')):original(input,init)))
 render();await choose();await vi.waitFor(()=>expect(target.querySelector('[role="alert"]')).not.toBeNull());expect(button('创建支付订单').disabled).toBe(true);expect(submissions).toHaveLength(0)
})
it('offers explicit new purchase for an expired pending order without marking the old one closed or paid',async()=>{
 intent={option_id:'backend-tier-special',payment_method:'wechat',idempotencyKey:'old-intent-key',order_id:'server-order-001'}
 order={id:'server-order-001',option_id:intent.option_id,payment_method:'wechat',amount_cents:3799,currency:'CNY',points:'25000.0000',status:'pending',expires_at:'2000-01-01T00:00:00Z',created_at:'2000-01-01T00:00:00Z',needs_review:false}
 render();await vi.waitFor(()=>expect(target.textContent).toContain('重新创建订单'));expect(target.textContent).toContain('待支付');expect(submissions).toHaveLength(0)
 button('重新创建订单').click();await vi.waitFor(()=>expect(button('创建支付订单').disabled).toBe(false));button('创建支付订单').click();await vi.waitFor(()=>expect(submissions).toHaveLength(1));expect(submissions[0].idempotencyKey).not.toBe('old-intent-key')
})
it('does not create an order if the wallet closes while saving the retry intent',async()=>{
 let finish!:(response:Response)=>void
 const original=globalThis.fetch
 vi.stubGlobal('fetch',vi.fn(async(input:any,init:any)=>String(input).endsWith('/recharge/intent')&&init?.method==='PUT'?new Promise<Response>(resolve=>finish=resolve):original(input,init)))
 render();await choose();button('创建支付订单').click();await vi.waitFor(()=>expect(finish).toBeDefined());await unmount(app!);app=undefined;finish(json({}));await new Promise(resolve=>setTimeout(resolve,20));expect(submissions).toHaveLength(0)
})
it('keeps recovery available for an existing intent when channels become unavailable',async()=>{
 enabled=false;intent={option_id:'backend-tier-special',payment_method:'wechat',idempotencyKey:'persisted-retry-key'}
 render();await vi.waitFor(()=>expect(button('重试本次订单').disabled).toBe(false));expect(target.querySelector('select')).toBeNull()
 button('重试本次订单').click();await vi.waitFor(()=>expect(submissions).toHaveLength(1));expect(submissions[0].idempotencyKey).toBe('persisted-retry-key')
})
it('loads usage only on demand, prevents repeat queries and retains applied dates and summary on pagination',async()=>{
 const original=globalThis.fetch;const requests:string[]=[];let finish!:(r:Response)=>void
 const page={items:[],has_more:true,next_cursor:'next',summary:{net_points:'5.0000',pending_count:0}}
 vi.stubGlobal('fetch',vi.fn(async(input:any,init:any)=>{
  if(String(input).includes('/product/usage')){requests.push(String(input));if(requests.length===2)return new Promise<Response>(resolve=>finish=resolve);return json(requests.length===1?page:{items:[],has_more:false,next_cursor:''})}
  return original(input,init)
 }))
 render();await choose();expect(requests).toHaveLength(0)
 button('用量明细').click();await vi.waitFor(()=>expect(requests).toHaveLength(1));await vi.waitFor(()=>expect(target.querySelector('.filters button')?.textContent).toContain('查询'))
 button('用量明细').click();expect(requests).toHaveLength(1)
 const inputs=target.querySelectorAll<HTMLInputElement>('input[type="date"]');inputs[0].value='2026-09-01';inputs[0].dispatchEvent(new Event('input',{bubbles:true}));flushSync()
 ;(target.querySelector('.filters button') as HTMLButtonElement).click();await vi.waitFor(()=>expect(finish).toBeDefined());flushSync()
 expect((target.querySelector('.filters button') as HTMLButtonElement).disabled).toBe(true)
 ;(target.querySelector('.filters button') as HTMLButtonElement).click();expect(requests).toHaveLength(2)
 finish(json(page));await vi.waitFor(()=>expect((target.querySelector('.filters button') as HTMLButtonElement).disabled).toBe(false))
 inputs[0].value='2026-09-10';inputs[0].dispatchEvent(new Event('input',{bubbles:true}));flushSync()
 button('加载更多').click();await vi.waitFor(()=>expect(requests).toHaveLength(3))
 expect(requests[2]).toContain('date_from=2026-09-01');expect(requests[2]).toContain('include_summary=false');expect(target.textContent).toContain('5')
})
