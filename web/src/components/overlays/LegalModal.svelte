<script lang="ts">
  import { t } from '../../lib/i18n'

  // OCTO-FORK: fetch the current platform publication on every open; never use placeholder text.
  import { getAgreement, type Agreement } from '../../lib/api'
  interface Props {
    title: string
    kind: 'box' | 'privacy'
    open: boolean
    onClose: () => void
  }
  let { title, kind, open, onClose }: Props = $props()
  let agreement = $state<Agreement | null>(null)
  let loading = $state(false)
  let failed = $state(false)
  let attempt = $state(0)
  $effect(() => {
    const currentKind = kind
    const retry = attempt
    agreement = null
    failed = false
    loading = false
    if (!open) return
    const controller = new AbortController()
    loading = true
    getAgreement(currentKind, controller.signal).then(result => {
      if (!controller.signal.aborted) agreement = result
    }).catch(() => {
      if (!controller.signal.aborted) failed = true
    }).finally(() => {
      if (!controller.signal.aborted) loading = false
    })
    return () => controller.abort()
  })
</script>

{#if open}
<div class="legal-backdrop" role="dialog" aria-modal="true" aria-label={agreement?.title ?? title} onclick={onClose}>
  <div class="legal-card" onclick={(e) => e.stopPropagation()}>
    <div class="legal-head">
      <h2 class="legal-title">{agreement?.title ?? title}</h2>
      <button class="legal-close" onclick={onClose} aria-label={$t('product.legal_close')}>
        <iconify-icon icon="ant-design:close-outlined" width="16"></iconify-icon>
      </button>
    </div>
    <div class="legal-body" aria-busy={loading}>
      {#if loading}
        <p role="status">{$t('common.loading')}</p>
      {:else if failed}
        <p role="alert">{$t('product.legal_load_failed')}</p>
        <button onclick={() => attempt++}>{$t('product.legal_retry')}</button>
      {:else if agreement}
        <div>{agreement.content}</div>
      {/if}
    </div>
  </div>
</div>
{/if}

<style>
.legal-backdrop {
  position: fixed; inset: 0; z-index: 2500;
  background: var(--scrim);
  display: flex; align-items: center; justify-content: center;
  padding: 24px;
}
.legal-card {
  width: 100%; max-width: 520px; max-height: 80vh;
  display: flex; flex-direction: column;
  background: var(--bg-container);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 16px 48px rgba(0,0,0,0.18);
  animation: octo-fadein 0.16s ease;
}
.legal-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 18px 20px 0;
}
.legal-title { margin: 0; font-size: 16px; font-weight: 600; color: var(--text-heading); }
.legal-close {
  display: flex; align-items: center; justify-content: center;
  width: 28px; height: 28px; border: none; border-radius: 6px;
  background: transparent; color: var(--text-secondary); cursor: pointer;
}
.legal-close:hover { background: var(--hover-neutral); color: var(--text); }
.legal-body {
  padding: 14px 20px 20px;
  font-size: 13px; line-height: 1.7; color: var(--text);
  white-space: pre-wrap; overflow-y: auto;
}
</style>
