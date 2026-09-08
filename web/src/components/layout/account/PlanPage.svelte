<script lang="ts">
  import { t } from '../../../lib/i18n'
  import { productState } from '../../../lib/product'

  // Plan page (P5): the current plan's display name — plan.name stores the
  // machine key ("trial") and the display name comes from the copy layer, so
  // plan names never hardcode a brand/display string in state. "Upgrade" is a
  // placeholder: no real payment this phase (需求 §7 / §5.4.2).
  const name = $derived($productState?.plan?.name ?? '')

  function planName(): string {
    // Known plans map to a copy key; an unknown future plan falls back to its
    // machine key rather than inventing copy for it.
    if (name === 'trial') return $t('product.plan_trial')
    return name || '—'
  }
</script>

<div class="plan">
  <div class="current">
    <span class="lbl">{$t('product.panel.plan')}</span>
    <span class="name">{planName()}</span>
  </div>
  <div class="actions">
    <button class="btn-upgrade" disabled title={$t('product.panel.upgrade_soon')}>
      {$t('product.panel.upgrade')}
    </button>
    <p class="note">{$t('product.panel.upgrade_soon')}</p>
  </div>
</div>

<style>
  .plan { display: flex; flex-direction: column; gap: 18px; padding: 4px 2px 12px; }
  .current { display: flex; align-items: center; justify-content: space-between; }
  .lbl { font-size: 13px; color: var(--text-secondary); }
  .name { font-size: 15px; font-weight: 600; color: var(--text-heading); }
  .actions { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
  .btn-upgrade {
    height: 32px; padding: 0 18px; border: 1px solid var(--blue-6);
    background: transparent; color: var(--blue-6); border-radius: 8px;
    font-size: 13px; font-weight: 500; cursor: not-allowed; font-family: inherit;
    opacity: 0.55;
  }
  .note { margin: 0; font-size: 12px; color: var(--text-tertiary); line-height: 1.6; }
</style>
