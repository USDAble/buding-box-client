<script lang="ts">
  import { t, locale } from '../../../lib/i18n'
  import { productState } from '../../../lib/product'
  import { licenseView } from '../../../lib/license'

  // License page (P5): strictly the two states 需求 §5.4.2 allows — active with
  // calendar days left, or expired with 0. There is deliberately no third
  // "not activated" empty state: reaching this panel means the data root was
  // activated. Expiry does not block anything (需求 §10 T11).
  const view = $derived(licenseView($productState?.activation?.expiresAt))

  const dateFmt = $derived(
    new Intl.DateTimeFormat($locale === 'zh' ? 'zh-CN' : 'en-US', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
    }),
  )

  function validUntil(dateStr: string | undefined): string {
    if (!dateStr) return '—'
    return $t('product.panel.license_valid_until').replaceAll('{date}', dateFmt.format(new Date(dateStr)))
  }

  // The box code is a business attribute, shown verbatim (never masked). An
  // older data root predates the field, so absence renders "—" rather than
  // blocking anything (需求基线 E5 rule 1, PQ19).
  const boxCode = $derived($productState?.activation?.boxCode || '—')
</script>

<div class="license">
  {#if view?.state === 'active'}
    <div class="status active">
      <span class="dot"></span>
      <span class="st">
        {view.daysLeft > 0
          ? $t('product.panel.license_active').replaceAll('{n}', String(view.daysLeft))
          : $t('product.panel.license_active_today')}
      </span>
    </div>
  {:else}
    <div class="status expired">
      <span class="dot"></span>
      <span class="st">{$t('product.panel.license_expired')}</span>
    </div>
    <p class="note">{$t('product.panel.license_expired_note')}</p>
  {/if}

  <p class="valid">{validUntil($productState?.activation?.expiresAt)}</p>
  <p class="valid">{$t('product.panel.license_box_code').replaceAll('{code}', boxCode)}</p>
</div>

<style>
  .license { display: flex; flex-direction: column; gap: 14px; padding: 4px 2px 12px; }
  .status { display: flex; align-items: center; gap: 8px; padding: 10px 12px; border-radius: 10px; }
  .status.active { background: var(--success-bg, rgba(52,199,89,0.08)); color: var(--success); }
  .status.expired { background: var(--warning-bg, rgba(255,149,0,0.10)); color: var(--warning-text, #d46b08); }
  .dot { width: 8px; height: 8px; border-radius: 50%; flex: 0 0 auto; }
  .status.active .dot { background: var(--success); }
  .status.expired .dot { background: var(--warning); }
  .st { font-size: 14px; font-weight: 600; }
  .note { margin: 0; font-size: 12px; color: var(--text-tertiary); line-height: 1.7; }
  .valid { margin: 0; font-size: 12px; color: var(--text-secondary); }
</style>
