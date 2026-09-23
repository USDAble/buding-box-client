<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { t, tr, platformErrorKey } from '../../lib/i18n'
  import { refreshCredits } from '../../lib/product'
  import { openUrl } from '../../lib/externalLinks'
  import QrCode from '../ui/QrCode.svelte'
  import * as finance from '../../lib/finance'

  // OCTO-FORK: every amount and payment status comes from the authenticated platform.
  let tab = $state<'recharge' | 'ledger' | 'usage' | 'pricing'>('recharge')
  let wallet = $state<finance.Wallet | null>(null)
  let options = $state<finance.Options | null>(null)
  let orders = $state<finance.Page<finance.Order>>({ items: [], next_cursor: '', has_more: false })
  let ledger = $state<finance.Page<finance.Ledger>>({ items: [], next_cursor: '', has_more: false })
  let usage = $state<finance.UsagePage | null>(null)
  let prices = $state<finance.Price[]>([])
  let selected = $state<finance.Order | null>(null)
  let optionID = $state('')
  let method = $state('')
  let intent = $state<finance.Intent | null>(null)
  let financeScope = $state('')
  let intentLoaded = $state(false)
  let busy = $state(false)
  let loading = $state(false)
  let polling = $state(false)
  let error = $state('')
  let loadError = $state('')
  let from = $state('')
  let to = $state('')
  let appliedFrom = ''
  let appliedTo = ''
  let timer: ReturnType<typeof setTimeout> | undefined
  let alive = true
  let pageRequest = 0
  let orderRequest = 0
  let now = $state(Date.now())
  let clock: ReturnType<typeof setInterval> | undefined
  const controller = new AbortController()
  const action = $derived(selected ? finance.paymentAction(selected, now) : null)
  const paymentsAvailable = $derived(!!options?.payment_enabled && options.payment_methods.length > 0)
  const canCreate = $derived(intentLoaded && !loading && wallet !== null && options?.payment_enabled && options.payment_methods.length > 0 && options.items.some(o => o.id === optionID && o.enabled) && options.payment_methods.some(m => m.id === method) && wallet?.status !== 'protected')

  const canResume = $derived(intentLoaded && !loading && wallet !== null && !!intent && intent.option_id === optionID && intent.payment_method === method && wallet.status !== 'protected')

  function message(e: unknown) {
    const code = e instanceof finance.FinanceError ? e.code : 'network_unavailable'
    const key = `wallet.error.${code.toUpperCase()}`
    const translated = tr(key)
    if (translated !== key) return translated
    const platformKey = platformErrorKey(code)
    return platformKey === 'platform.error.generic' ? tr('wallet.error.generic') : tr(platformKey)
  }
  function stopPolling() { if (timer) clearTimeout(timer); timer = undefined }
  async function loadWallet() {
    const value = await finance.getWallet(controller.signal)
    if (!alive) return
    wallet = value
    await refreshCredits(controller.signal)
  }
  async function loadInitial() {
    loading = true; loadError = ''; intentLoaded = false
    const results = await Promise.allSettled([
      loadWallet(),
      finance.getOptions(controller.signal).then(data => { if (alive) options = data }),
      finance.getOrders('', controller.signal).then(data => { if (alive) orders = data }),
      finance.getIntent(controller.signal).then(async result => {
        if (!alive) return
        const data = result.intent
        financeScope = result.scope
        intentLoaded = true
        intent = data
        if (data) { optionID = data.option_id; method = data.payment_method }
        if (data?.order_id) await selectOrder(data.order_id)
      }),
    ])
    if (!alive) return
    const failure = results.find(result => result.status === 'rejected')
    if (failure?.status === 'rejected') loadError = message(failure.reason)
    loading = false
  }
  async function acceptOrder(order: finance.Order) {
    if (!alive) return
    selected = order
    orders = { ...orders, items: [order, ...orders.items.filter(item => item.id !== order.id)] }
    if (order.status === 'paid') {
      stopPolling()
      await loadWallet()
      if (intent?.order_id === order.id) { await finance.clearIntent(financeScope, controller.signal); if (alive) intent = null }
    } else if (order.status === 'pending' && Date.parse(order.expires_at) > Date.now()) {
      stopPolling(); timer = setTimeout(() => { void pollOrder() }, 5000)
    }
  }
  async function selectOrder(id: string) {
    stopPolling(); error = ''; polling = true
    const sequence = ++orderRequest
    try {
      const order = await finance.getOrder(id, controller.signal)
      if (sequence === orderRequest) await acceptOrder(order)
    } catch (e) { if (alive && sequence === orderRequest) error = message(e) }
    finally { if (alive) polling = false }
  }
  async function pollOrder() {
    if (!selected || !alive || polling) return
    await selectOrder(selected.id)
  }
  async function submit() {
    if (busy || (!canCreate && !canResume)) return
    busy = true; error = ''; stopPolling()
    const scope = financeScope
    try {
      if (!intent || intent.option_id !== optionID || intent.payment_method !== method || (selected && selected.id === intent.order_id && selected.status !== 'pending')) {
        intent = { option_id: optionID, payment_method: method, idempotencyKey: crypto.randomUUID() }
      }
      // Persist before sending: retry after restart uses the same key even if the success reply was lost.
      await finance.saveIntent(intent, scope, controller.signal)
      if (!alive) return
      const order = await finance.createOrder(intent, scope, controller.signal)
      if (!alive) return
      intent = { ...intent, order_id: order.id }
      await finance.saveIntent(intent, scope, controller.signal)
      if (!alive) return
      await acceptOrder(order)
    } catch (e) { if (alive) error = message(e) }
    finally { if (alive) busy = false }
  }
  async function newAttempt() {
    if (busy) return
    try { await finance.clearIntent(financeScope, controller.signal); if (alive) { intent = null; selected = null; error = ''; stopPolling() } }
    catch(e) { if (alive) error = message(e) }
  }
  async function loadPage(next = false) {
    if (loading) return
    const current = tab, sequence = ++pageRequest
    loading = true; loadError = ''
    try {
      if (current === 'ledger') {
        const data = await finance.getLedger(next ? ledger.next_cursor : '', controller.signal)
        if (sequence === pageRequest && alive) ledger = { ...data, items: next ? [...ledger.items, ...data.items.filter(item => !ledger.items.some(old => old.id === item.id))] : data.items }
      } else if (current === 'usage') {
        const dateFrom = next ? appliedFrom : from, dateTo = next ? appliedTo : to
        const data = await finance.getUsage(next ? usage?.next_cursor : '', dateFrom, dateTo, controller.signal)
        if (!next && sequence === pageRequest && alive) { appliedFrom = dateFrom; appliedTo = dateTo }
        if (sequence === pageRequest && alive) usage = { ...data, summary: data.summary ?? (next ? usage?.summary : undefined), items: next ? [...(usage?.items ?? []), ...data.items.filter(item => !usage?.items.some(old => old.run_id === item.run_id))] : data.items }
      } else if (current === 'pricing') {
        const data = await finance.getPrices(controller.signal)
        if (sequence === pageRequest && alive) prices = data.items ?? []
      } else {
        const data = await finance.getOrders(next ? orders.next_cursor : '', controller.signal)
        if (sequence === pageRequest && alive) orders = { ...data, items: next ? [...orders.items, ...data.items.filter(item => !orders.items.some(old => old.id === item.id))] : data.items }
      }
    } catch (e) { if (alive && sequence === pageRequest) loadError = message(e) }
    finally { if (alive && sequence === pageRequest) loading = false }
  }
  async function switchTab(value: typeof tab) { if (loading || tab === value) return; tab = value; await loadPage() }
  async function refreshCurrent() {
    if (loading || busy) return
    if (tab === 'recharge') { await loadInitial(); return }
    await Promise.all([loadPage(), loadWallet().catch(e => { if (alive) loadError = message(e) })])
  }
  function label(prefix: string, value: string) { const key = `wallet.${prefix}.${value}`; const text = tr(key); return text === key ? value : text }
  function date(value?: string | null) { return value ? new Date(value).toLocaleString() : '—' }
  onMount(() => { clock = setInterval(() => now = Date.now(), 1000); void loadInitial() })
  onDestroy(() => { alive = false; ++pageRequest; controller.abort(); stopPolling(); if (clock) clearInterval(clock) })
</script>

<section class="wallet" aria-label={$t('wallet.title')}>
  <header><h2>{$t('wallet.title')}</h2><button onclick={refreshCurrent} disabled={loading || busy}>{$t('common.refresh')}</button></header>
  {#if loadError}<p class="error" role="alert">{loadError}</p>{/if}
  <div class="balances">
    {#each ['available_points', 'reserved_points', 'total_recharged_points', 'total_consumed_points'] as key}
      <div><span>{$t(`wallet.${key}`)}</span><strong>{finance.points(wallet?.[key as keyof finance.Wallet])}</strong></div>
    {/each}
  </div>
  {#if wallet?.status === 'protected'}<p class="error">{$t('wallet.error.WALLET_PROTECTED')}</p>{/if}
  <nav>{#each ['recharge', 'ledger', 'usage', 'pricing'] as value}<button class:active={tab === value} disabled={loading} onclick={() => switchTab(value as typeof tab)}>{$t(`wallet.${value}`)}</button>{/each}</nav>
  {#if tab === 'recharge'}
    <p>{$t('wallet.recharge_hint')}</p>
    {#if options}
      <div class="options">{#each options.items.filter(item => item.enabled) as option}<button class:active={optionID === option.id} aria-pressed={optionID === option.id} disabled={busy || !paymentsAvailable} onclick={() => optionID = option.id}><strong>{finance.points(option.points)} <small>{$t('wallet.points')}</small></strong><span>{finance.money(option.amount_cents, option.currency)}</span></button>{/each}</div>
      {#if !options.items.some(item => item.enabled)}<p>{$t('wallet.no_options')}</p>{/if}
      {#if paymentsAvailable}<label>{$t('wallet.payment_method')} <select bind:value={method} disabled={busy}><option value="">{$t('wallet.choose')}</option>{#each options.payment_methods as item}<option value={item.id}>{item.name}</option>{/each}</select></label>
      {:else}<p class="notice" role="status">{$t('wallet.payment_unavailable')}</p>{/if}
    {/if}
    {#if paymentsAvailable || canResume}<button class="primary" onclick={submit} disabled={(!canCreate && !canResume) || busy}>{busy ? $t('common.loading') : intent && intent.option_id === optionID && intent.payment_method === method ? $t('wallet.resume_order') : $t('wallet.create_order')}</button>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if selected}
      <article class="payment">
        <h3>{$t('wallet.order')}</h3><span class="order-id">{selected.id}</span><p>{finance.money(selected.amount_cents, selected.currency)} · {finance.points(selected.points)} {$t('wallet.points')} · <strong>{label('status', selected.status)}</strong></p>
        {#if selected.needs_review}<p class="notice">{$t('wallet.review')}</p>{/if}
        {#if selected.status === 'paid'}<p class="success">{$t('wallet.paid')} {date(selected.paid_at)}</p>
        {:else if action === 'qr'}{#key selected.id + selected.payment_url}<QrCode text={selected.payment_url!} />{/key}<p>{$t('wallet.scan')}</p>
        {:else if action === 'link'}<button onclick={() => openUrl(selected!.payment_url!)}>{$t('wallet.open_payment')}</button>
        {:else if selected.status === 'pending'}<p>{$t('wallet.pending_url')}</p>{/if}
        {#if selected.status !== 'pending' || Date.parse(selected.expires_at) <= now}<button onclick={newAttempt} disabled={busy}>{$t('wallet.new_order')}</button>{/if}
        <p>{$t('wallet.expires')} {date(selected.expires_at)}</p><p>{$t('wallet.payment_truth')}</p>
        <button onclick={() => selectOrder(selected!.id)} disabled={polling || busy}>{polling ? $t('common.loading') : $t('wallet.check_payment')}</button>
      </article>
    {/if}
    <div class="section-heading"><h3>{$t('wallet.orders')}</h3><p>{$t('wallet.orders_range')}</p></div>
    <div class="table"><table><thead><tr><th>{$t('wallet.time')}</th><th>{$t('wallet.amount')}</th><th>{$t('wallet.points')}</th><th>{$t('wallet.status')}</th><th></th></tr></thead><tbody>{#each orders.items as order (order.id)}<tr><td>{date(order.created_at)}</td><td>{finance.money(order.amount_cents, order.currency)}</td><td>{finance.points(order.points)}</td><td>{label('status', order.status)}</td><td><button onclick={() => selectOrder(order.id)}>{$t('wallet.details')}</button></td></tr>{/each}</tbody></table></div>
    {#if !loading && orders.items.length === 0}<p class="empty">{$t('wallet.empty')}</p>{/if}
    {#if orders.has_more}<button disabled={loading} onclick={() => loadPage(true)}>{$t('wallet.more')}</button>{/if}
  {:else if tab === 'ledger'}
    <p>{$t('wallet.ledger_hint')}</p><button disabled={loading} onclick={() => loadPage()}>{$t('common.refresh')}</button>
    <div class="table"><table><thead><tr><th>{$t('wallet.time')}</th><th>{$t('wallet.event')}</th><th>{$t('wallet.available_change')}</th><th>{$t('wallet.reserved_change')}</th><th>{$t('wallet.available_points')}</th><th>{$t('wallet.reference')}</th></tr></thead><tbody>{#each ledger.items as item (item.id)}<tr><td>{date(item.created_at)}</td><td>{label('event', item.event_type)}</td><td>{finance.points(item.available_change)}</td><td>{finance.points(item.reserved_change)}</td><td>{finance.points(item.available_points)}</td><td>{item.recharge_order_id || item.run_id || item.reason || '—'}</td></tr>{/each}</tbody></table></div>
    {#if !loading && !ledger.items.length}<p>{$t('wallet.empty')}</p>{/if}{#if ledger.has_more}<button disabled={loading} onclick={() => loadPage(true)}>{$t('wallet.more')}</button>{/if}
  {:else if tab === 'usage'}
    <form class="filters" onsubmit={(event) => { event.preventDefault(); void loadPage() }}><label>{$t('wallet.from')}<input type="date" bind:value={from} max={to || undefined} disabled={loading} /></label><label>{$t('wallet.to')}<input type="date" bind:value={to} min={from || undefined} disabled={loading} /></label><button class="primary query-button" type="submit" disabled={loading} aria-busy={loading}>{loading ? $t('common.loading') : $t('wallet.search')}</button></form>
    <p>{$t('wallet.usage_hint')}</p>{#if usage?.summary}<p>{$t('wallet.total')}: {finance.points(usage.summary.net_points)} {$t('wallet.points')} · {$t('wallet.pending')}: {usage.summary.pending_count}</p>{/if}
    <div class="table"><table><thead><tr><th>{$t('wallet.time')}</th><th>{$t('wallet.model')}</th><th>{$t('wallet.input_tokens')}</th><th>{$t('wallet.output_tokens')}</th><th>{$t('wallet.net_points')}</th><th>{$t('wallet.status')}</th></tr></thead><tbody>{#each usage?.items ?? [] as item (item.run_id)}<tr><td>{date(item.created_at)}</td><td>{item.model_name}</td><td>{item.prompt_tokens}</td><td>{item.completion_tokens}</td><td>{item.net_points === null ? $t('wallet.pending') : finance.points(item.net_points)}</td><td>{label('billing', item.billing_status)}</td></tr>{/each}</tbody></table></div>
    {#if !loading && !usage?.items.length}<p>{$t('wallet.empty')}</p>{/if}{#if usage?.has_more}<button disabled={loading} onclick={() => loadPage(true)}>{$t('wallet.more')}</button>{/if}
  {:else}
    <p>{$t('wallet.pricing_hint')}</p><button disabled={loading} onclick={() => loadPage()}>{$t('common.refresh')}</button>
    <div class="table"><table><thead><tr><th>{$t('wallet.model')}</th><th>{$t('wallet.input')}</th><th>{$t('wallet.output')}</th><th>{$t('wallet.cache')}</th><th>{$t('wallet.effective')}</th></tr></thead><tbody>{#each prices as item (item.id)}<tr><td>{item.model_name}</td><td>{finance.points(item.input_points_per_million)}</td><td>{finance.points(item.output_points_per_million)}</td><td>{finance.points(item.cache_read_points_per_million)}</td><td>{date(item.effective_at)}</td></tr>{/each}</tbody></table></div>
    {#if !loading && !prices.length}<p>{$t('wallet.empty')}</p>{/if}
  {/if}
  {#if loading}<p role="status">{$t('common.loading')}</p>{/if}
</section>

<style>
  .wallet { display:flex; flex-direction:column; gap:12px; color:var(--text); min-width:0; font-size:12px; }
  header, nav, .filters { display:flex; align-items:center; gap:10px; flex-wrap:wrap; } header { justify-content:space-between; }
  h2,h3,p { margin:0; } h2 {font-size:15px;font-weight:600;} h3 {font-size:13px;font-weight:600;} p { font-size:12px; line-height:1.55; color:var(--text-secondary); }
  button,select,input { background:var(--bg-container);color:var(--text);border:1px solid var(--border);border-radius:7px;padding:7px 12px;font:inherit; }
  button { cursor:pointer; } button:disabled { opacity:.5;cursor:default; } button.active { border-color:var(--blue-6);color:var(--blue-6);background:var(--blue-1); } .primary { align-self:flex-start;background:var(--blue-6);color:var(--on-accent); }
  .balances {display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;}
  .balances>div {display:flex;flex-direction:column;gap:5px;padding:11px 13px;border:1px solid var(--border);border-radius:8px;background:var(--bg-container);min-width:0;}
  .balances>div:first-child {background:var(--blue-1);border-color:var(--blue-3,var(--border));}
  .balances>div:first-child strong {color:var(--blue-6);}
  .balances span {font-size:11px;color:var(--text-secondary);}.balances strong {font-size:18px;font-weight:600;overflow-wrap:anywhere;font-variant-numeric:tabular-nums;}
  nav {gap:3px;padding:3px;border-radius:8px;background:var(--bg-layout);}
  nav button {flex:1;padding:6px 8px;border-color:transparent;background:transparent;white-space:nowrap;}
  nav button.active {background:var(--bg-container);border-color:var(--border);}
  .options {display:grid;grid-template-columns:repeat(auto-fill,minmax(125px,1fr));gap:8px;}
  .options>button {position:relative;display:flex;flex-direction:column;align-items:flex-start;gap:7px;padding:12px;min-width:0;max-width:180px;min-height:76px;border-radius:8px;text-align:left;}
  .options strong {font-size:15px;font-weight:600;}.options small {font-size:11px;font-weight:400;}.options span {font-size:12px;color:var(--text-secondary);}
  .options>button.active {box-shadow:inset 0 0 0 1px var(--blue-6);}
  .options>button.active::after {content:'';position:absolute;right:8px;top:8px;width:6px;height:6px;border-radius:50%;background:var(--blue-6);}
  .options>button:disabled {opacity:1;cursor:default;color:var(--text-secondary);}
  .payment { border:1px solid var(--border);padding:12px;border-radius:8px;display:flex;flex-direction:column;align-items:flex-start;gap:8px;overflow-wrap:anywhere; }
  .order-id {font-size:11px;color:var(--text-tertiary);font-family:monospace;}
  .section-heading {display:flex;align-items:baseline;justify-content:space-between;gap:8px;flex-wrap:wrap;margin-top:8px;}.section-heading p {font-size:11px;}
  .empty {text-align:center;padding:20px 10px;border:1px dashed var(--border);border-radius:8px;}
  .error {color:var(--error);}.success {color:var(--green-6);}.notice {color:var(--text-secondary);background:var(--bg-layout);padding:10px 12px;border:1px solid var(--border);border-radius:8px;}
  .table {overflow:auto;} table {width:100%;border-collapse:collapse;font-size:12px;} th,td {text-align:left;padding:8px 7px;border-bottom:1px solid var(--border);white-space:nowrap;} th {color:var(--text-secondary);font-weight:500;font-size:11px;} td button {padding:4px 7px;font-size:11px;}label {font-size:12px;display:flex;gap:8px;align-items:center;}
  /* OCTO-FORK: keep bounded usage filters aligned and query progress visible. */
  .filters {display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr) auto;align-items:end;padding:12px;background:var(--bg-layout);border:1px solid var(--border);border-radius:8px;}
  .filters label {display:flex;flex-direction:column;align-items:stretch;gap:6px;color:var(--text-secondary);min-width:0;}
  .filters input {height:34px;box-sizing:border-box;min-width:0;width:100%;padding:6px 8px;}
  .filters .query-button {align-self:end;min-width:72px;height:34px;border-color:var(--blue-6);font-weight:600;white-space:nowrap;}
  .primary:hover:not(:disabled) {background:var(--blue-5);}
  button:focus-visible,input:focus-visible {outline:2px solid var(--blue-6);outline-offset:2px;}
  @media(max-width:420px) {.filters {grid-template-columns:minmax(0,1fr) minmax(0,1fr);}.filters .query-button {grid-column:1/-1;width:100%;}}
</style>
