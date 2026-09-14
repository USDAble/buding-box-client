<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from '../../../lib/i18n'
  import { productState, refreshCredits } from '../../../lib/product'

  // Points page (P5 shell): the balance, plus the placeholder "top up" entry
  // point (no real payment, 需求 §7).
  //
  // THE BALANCE IS NEVER COMPUTED HERE. It is whatever the platform's ledger last
  // said (需求基线 E9 rule 2: the server deducts, the client re-reads), and this
  // page's job is to display it and to offer the moments at which it is asked
  // for. Opening the page is one of those moments - it is the user's clearest way
  // of saying "show me what I have now" - and the button below is the explicit
  // one, because a rule that names "the user refreshes" needs something the user
  // can press.
  const balance = $derived($productState?.credits?.balance ?? 0)

  let refreshing = $state(false)
  let failed = $state(false)

  async function reload() {
    refreshing = true
    failed = false
    try {
      await refreshCredits()
    } catch {
      // Shown, not swallowed: the number above keeps its old value, and the user
      // is told that is what happened. A silently stale balance is the failure
      // mode 开发规范 §3.9 is about.
      failed = true
    } finally {
      refreshing = false
    }
  }

  onMount(reload)
</script>

<div class="credits">
  <div class="rows">
    <div class="row">
      <span class="lbl">{$t('product.panel.balance')}</span>
      <span class="val">{balance}</span>
    </div>
  </div>
  <div class="actions">
    <button class="btn-refresh" onclick={reload} disabled={refreshing}>
      {$t('product.panel.refresh')}
    </button>
    {#if failed}
      <p class="note warn">{$t('product.panel.refresh_failed')}</p>
    {/if}
    <button class="btn-recharge" disabled title={$t('product.panel.recharge_soon')}>
      {$t('product.panel.recharge')}
    </button>
    <p class="note">{$t('product.panel.recharge_soon')}</p>
  </div>
</div>

<style>
  .credits { display: flex; flex-direction: column; gap: 18px; padding: 4px 2px 12px; }
  .rows { display: flex; flex-direction: column; }
  .row {
    display: flex; align-items: center; justify-content: space-between;
    padding: 10px 2px; border-bottom: 1px solid var(--border-secondary);
  }
  .row:last-child { border-bottom: none; }
  .lbl { font-size: 13px; color: var(--text-secondary); }
  .val { font-size: 15px; font-weight: 600; color: var(--text-heading); font-variant-numeric: tabular-nums; }
  .actions { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
  .btn-refresh {
    height: 32px; padding: 0 18px; border: 1px solid var(--border-secondary);
    background: transparent; color: var(--text-primary); border-radius: 8px;
    font-size: 13px; font-weight: 500; cursor: pointer; font-family: inherit;
  }
  .btn-refresh:disabled { cursor: progress; opacity: 0.55; }
  .btn-recharge {
    height: 32px; padding: 0 18px; border: 1px solid var(--blue-6);
    background: transparent; color: var(--blue-6); border-radius: 8px;
    font-size: 13px; font-weight: 500; cursor: not-allowed; font-family: inherit;
    opacity: 0.55;
  }
  .note { margin: 0; font-size: 12px; color: var(--text-tertiary); line-height: 1.6; }
  .warn { color: var(--red-6, #d4380d); }
</style>
