<script lang="ts">
  import { onMount } from 'svelte'
  import { t, locale } from '../../../lib/i18n'
  import { brandName, brandCopyrightFor } from '../../../lib/brand'
  import BrandMark from '../../BrandMark.svelte'

  // About page (P5): the version row replaces the old bottom-corner
  // VersionBadge. Only a display fetch — the check-for-updates entry stays a
  // disabled placeholder elsewhere in the panel (P2 closes auto-update).
  let version = $state('')

  onMount(async () => {
    try {
      const res = await fetch('/api/version', { cache: 'no-store' })
      const d = (await res.json()) as { current?: string; version?: string }
      version = d.current ?? d.version ?? ''
    } catch {
      /* keep the row at '—' */
    }
  })
</script>

<div class="about">
  <BrandMark size={40} />
  <p class="name">{brandName($locale)}</p>
  <div class="row">
    <span class="lbl">{$t('common.version')}</span>
    <span class="val">{version ? `v${version.replace(/^v/, '')}` : '—'}</span>
  </div>
  <p class="copy">{brandCopyrightFor($locale)}</p>
</div>

<style>
  .about {
    display: flex; flex-direction: column; align-items: center; gap: 14px;
    padding: 10px 2px 16px; text-align: center;
  }
  .name { margin: 0; font-size: 16px; font-weight: 600; color: var(--text-heading); }
  .row {
    width: 100%; display: flex; align-items: center; justify-content: space-between;
    padding: 8px 2px; border-top: 1px solid var(--border-secondary);
  }
  .lbl { font-size: 13px; color: var(--text-secondary); }
  .val { font-size: 13px; color: var(--text); font-variant-numeric: tabular-nums; }
  .copy { margin: 0; font-size: 11px; color: var(--text-tertiary); line-height: 1.6; }
</style>
