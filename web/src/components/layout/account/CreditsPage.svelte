<script lang="ts">
  import { t } from '../../../lib/i18n'
  import { productState } from '../../../lib/product'

  // Points page (P5 shell): balance + used-this-month render from the product
  // state the login seeds (1280 / 0). The deduction rules are P6's; this page
  // only displays and carries the placeholder "top up" (no real payment, 需求
  // §7). Recharge is intentionally disabled here.
  const balance = $derived($productState?.credits?.balance ?? 0)
  const monthUsed = $derived($productState?.credits?.monthUsed ?? 0)
</script>

<div class="credits">
  <div class="rows">
    <div class="row">
      <span class="lbl">{$t('product.panel.balance')}</span>
      <span class="val">{balance}</span>
    </div>
    <div class="row">
      <span class="lbl">{$t('product.panel.month_used')}</span>
      <span class="val">{monthUsed}</span>
    </div>
  </div>
  <div class="actions">
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
  .btn-recharge {
    height: 32px; padding: 0 18px; border: 1px solid var(--blue-6);
    background: transparent; color: var(--blue-6); border-radius: 8px;
    font-size: 13px; font-weight: 500; cursor: not-allowed; font-family: inherit;
    opacity: 0.55;
  }
  .note { margin: 0; font-size: 12px; color: var(--text-tertiary); line-height: 1.6; }
</style>
