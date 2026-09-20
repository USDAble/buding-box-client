import { windowToken, noteSessionLost } from './product'
export interface Wallet {available_points:string;reserved_points:string;total_recharged_points:string;total_consumed_points:string;total_corrected_points:string;status:string}
export interface Option {id:string;amount_cents:number;currency:string;points:string;enabled:boolean}
export interface Options {items:Option[];payment_methods:{id:string;name:string}[];payment_enabled:boolean}
export interface Order {id:string;option_id:string;payment_method:string;amount_cents:number;currency:string;points:string;status:'pending'|'paid'|'closed';payment_url?:string;expires_at:string;paid_at?:string;created_at:string;needs_review:boolean}
export interface Intent {option_id:string;payment_method:string;idempotencyKey:string;order_id?:string}
export interface Page<T> {items:T[];next_cursor:string;has_more:boolean}
export interface Ledger {id:number;event_type:string;points:string;available_change:string;reserved_change:string;available_points:string;reserved_points:string;created_at:string;reason?:string;run_id?:string;recharge_order_id?:string}
export interface Usage {run_id:string;model_name:string;run_status:string;billing_status:string;prompt_tokens:number;completion_tokens:number;net_points:string|null;reserved_points:string;created_at:string}
export interface UsagePage extends Page<Usage> {summary?:{run_count:number;prompt_tokens:number;completion_tokens:number;net_points:string;pending_count:number}}
export interface Price {id:string;logical_model_id:string;model_name:string;input_points_per_million:string;output_points_per_million:string;cache_read_points_per_million:string;effective_at?:string}
export class FinanceError extends Error {constructor(public code:string,public status=0){super(code)}}
async function request<T>(path:string,method='GET',body?:unknown,signal?:AbortSignal,scope?:string):Promise<T>{
 const res=await fetch('/api/product/'+path,{method,headers:{'Content-Type':'application/json','X-Octo-Window-Token':windowToken() ?? '',...(scope?{'X-Finance-Scope':scope}:{})},body:body===undefined?undefined:JSON.stringify(body),signal,cache:'no-store'})
 if(signal?.aborted)throw new FinanceError('cancelled')
 noteSessionLost(res.status)
 const data=await res.json().catch(()=>null)
 if(!res.ok)throw new FinanceError(data?.code || 'internal_error',res.status)
 if(data===undefined)throw new FinanceError('invalid_response')
 return data as T
}
function query(values:Record<string,string|undefined>){const q=new URLSearchParams();for(const [key,value]of Object.entries(values))if(value)q.set(key,value);return q.size?'?'+q:''}
export const getWallet=(signal?:AbortSignal)=>request<Wallet>('wallet','GET',undefined,signal)
export const getOptions=(signal?:AbortSignal)=>request<Options>('recharge/options','GET',undefined,signal)
export const getPrices=(signal?:AbortSignal)=>request<{items:Price[];points_per_usd:string}>('pricing','GET',undefined,signal)
export const getLedger=(cursor='',signal?:AbortSignal)=>request<Page<Ledger>>('wallet/ledger'+query({cursor,limit:'20'}),'GET',undefined,signal)
export const getUsage=(cursor='',from='',to='',signal?:AbortSignal)=>request<UsagePage>('usage'+query({cursor,limit:'20',date_from:from,date_to:to,include_summary:cursor?'false':''}),'GET',undefined,signal)
export const getOrders=(cursor='',signal?:AbortSignal)=>request<Page<Order>>('recharge/orders'+query({cursor,limit:'20'}),'GET',undefined,signal)
export const getOrder=(id:string,signal?:AbortSignal)=>request<Order>('recharge/orders/'+encodeURIComponent(id),'GET',undefined,signal)
export const createOrder=(intent:Intent,scope:string,signal?:AbortSignal)=>request<Order>('recharge/orders','POST',{option_id:intent.option_id,payment_method:intent.payment_method,idempotencyKey:intent.idempotencyKey,scope},signal)
export const getIntent=(signal?:AbortSignal)=>request<{scope:string;intent:Intent|null}>('recharge/intent','GET',undefined,signal)
export const saveIntent=(intent:Intent,scope:string,signal?:AbortSignal)=>request<Intent>('recharge/intent','PUT',intent,signal,scope)
export const clearIntent=(scope:string,signal?:AbortSignal)=>request('recharge/intent','DELETE',undefined,signal,scope)
export function paymentAction(order:Order,now=Date.now()):'qr'|'link'|null{
 if(order.status!=='pending'||order.needs_review||!order.payment_url||!Number.isFinite(Date.parse(order.expires_at))||Date.parse(order.expires_at)<=now)return null
 if(order.payment_method==='wechat' && (order.payment_url.toLowerCase().startsWith('weixin://wxpay/bizpayurl?') || order.payment_url.toLowerCase().startsWith('https://')))return 'qr'
 if(['alipay','stripe'].includes(order.payment_method)&&order.payment_url.toLowerCase().startsWith('https://'))return 'link'
 return null
}
export function money(cents:number,currency:string){if(!Number.isSafeInteger(cents)||cents<0)return '—';return currency+' '+Math.floor(cents/100)+'.'+String(cents%100).padStart(2,'0')}
export function points(value:string|undefined|null){return typeof value==='string'&&/^-?\d+(\.\d+)?$/.test(value)?value.replace(/(\.\d*?)0+$/,'$1').replace(/\.$/,''):'—'}
