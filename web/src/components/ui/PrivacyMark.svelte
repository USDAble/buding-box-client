<script lang="ts">
  import { t } from '../../lib/i18n'
  import { isPrivacyMode } from '../../lib/chatMode'

  let { mode = '', label = false }: { mode?: string; label?: boolean } = $props()
</script>

{#if isPrivacyMode(mode)}
  <span
    class="privacy-mark"
    class:with-label={label}
    title={$t('privacy.mode_label')}
    aria-label={$t('privacy.mode_label')}
    data-privacy-mark
  >
    <iconify-icon icon="lucide:shield-check" width="13"></iconify-icon>
    {#if label}<span>{$t('mode.privacy')}</span>{/if}
  </span>
{/if}

<style>
  .privacy-mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 13px;
    flex: 0 0 auto;
    color: var(--blue-6);
    line-height: 1;
  }
  .with-label { width: auto; gap: 3px; }
</style>
