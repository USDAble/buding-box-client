<script lang="ts">
  import { t, tr } from '../../../lib/i18n'
  import { productState } from '../../../lib/product'
  import { showToast } from '../../../lib/stores'

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

  // 升级 (E10's other half, landed with 充值 in PR-5d2). E10 names both entries
  // in one sentence and there is no closed-loop row for this one, so leaving it
  // `disabled` would ship a panel where one entry answers and its twin does not.
  // Same reasoning as CreditsPage.recharge: no URL exists in any layer, so the
  // answer is the app's coming-soon toast, not a link we would have to invent.
  function upgrade() {
    showToast(tr('product.panel.upgrade_soon'))
  }
</script>

<div class="plan">
  <div class="current">
    <span class="lbl">{$t('product.panel.plan')}</span>
    <span class="name">{planName()}</span>
  </div>
  <div class="actions">
    <button class="btn-upgrade" onclick={upgrade}>
      {$t('product.panel.upgrade')}
    </button>
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
    font-size: 13px; font-weight: 500; cursor: pointer; font-family: inherit;
  }
  .btn-upgrade:hover { background: var(--blue-1, rgba(22,119,255,0.06)); }
</style>
