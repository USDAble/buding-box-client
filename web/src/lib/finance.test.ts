import {get} from 'svelte/store'
import {productPhase,productState} from './product'
import { expect,it,vi } from 'vitest'
import {paymentAction,money,points,createOrder,getWallet,FinanceError,type Order} from './finance'
const pending={status:'pending',payment_method:'wechat',payment_url:'weixin://wxpay/bizpayurl?pr=test',expires_at:'2099-01-01T00:00:00Z',needs_review:false} as Order
it('only offers authentic pending payment addresses and never paid/expired orders',()=>{
 expect(paymentAction(pending)).toBe('qr');expect(paymentAction({...pending,status:'paid'})).toBeNull();expect(paymentAction({...pending,expires_at:'2000-01-01T00:00:00Z'})).toBeNull();expect(paymentAction({...pending,payment_url:'javascript:alert(1)'})).toBeNull();expect(paymentAction({...pending,payment_method:'alipay',payment_url:'https://payment.example.test/pay'})).toBe('link');expect(paymentAction({...pending,needs_review:true})).toBeNull()
})
it('formats cents exactly and preserves decimal point precision',()=>{expect(money(2501,'CNY')).toBe('CNY 25.01');expect(money(0.1,'CNY')).toBe('—');expect(points('123456789012345678.4500')).toBe('123456789012345678.45');expect(points(null)).toBe('—');expect(points('0.0000')).toBe('0')})

it('ignores a late cancelled order response before handling unauthorized',async()=>{
 let finish!:(response:Response)=>void
 vi.stubGlobal('fetch',vi.fn(()=>new Promise<Response>(resolve=>finish=resolve)))
 productState.set({loggedIn:true} as never);productPhase.set('ready');const controller=new AbortController()
 const pending=createOrder({option_id:'option',payment_method:'wechat',idempotencyKey:'stable-intent'},'scope',controller.signal).catch(error=>error)
 controller.abort();productState.set({loggedIn:true} as never);productPhase.set('ready');finish(new Response('{}',{status:401}));expect(await pending).toBeInstanceOf(Error);expect(get(productPhase)).toBe('ready');vi.unstubAllGlobals()
})

it('keeps wallet machine code and raw platform message for localized display and diagnostics',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({code:'WALLET_PROTECTED',message:'钱包正在核查'}),{status:409})))
 const error=await getWallet().catch(error=>error)
 expect(error).toBeInstanceOf(FinanceError)
 expect(error.code).toBe('WALLET_PROTECTED')
 expect(error.serverMessage).toBe('钱包正在核查')
 vi.unstubAllGlobals()
})
