<script lang="ts">
  import { t } from '../../lib/i18n'
  import { isPrivacyMode } from '../../lib/chatMode'

  let { mode = '' }: { mode?: string } = $props()
  let expanded = $state(false)
</script>

{#if isPrivacyMode(mode)}
  <div class="privacy-bar" data-privacy-bar>
    <div class="privacy-main">
      <iconify-icon icon="lucide:shield-check" width="14"></iconify-icon>
      <span>{$t('privacy.notice')}</span>
      <button
        type="button"
        class="details-toggle"
        aria-expanded={expanded}
        aria-label={expanded ? $t('privacy.collapse') : $t('privacy.expand')}
        onclick={() => { expanded = !expanded }}
      >
        <iconify-icon icon={expanded ? 'lucide:chevron-up' : 'lucide:chevron-down'} width="13"></iconify-icon>
      </button>
    </div>
    {#if expanded}
      <div class="privacy-detail">{$t('privacy.notice_detail')}</div>
    {/if}
  </div>
{/if}

<style>
  .privacy-bar {
    max-width: var(--chat-content-max-width, 1080px);
    margin: 0 auto;
    padding: 5px 24px 0;
    color: var(--blue-6);
    font-size: 12px;
    line-height: 1.45;
  }
  .privacy-main {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .privacy-main > span { min-width: 0; }
  .privacy-main iconify-icon { flex: 0 0 auto; }
  .details-toggle {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    flex: 0 0 20px;
    padding: 0;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: inherit;
    cursor: pointer;
  }
  .details-toggle:hover { background: var(--active-blue-bg); }
  .privacy-detail {
    padding: 2px 0 0 20px;
    color: var(--text-secondary);
  }
</style>
