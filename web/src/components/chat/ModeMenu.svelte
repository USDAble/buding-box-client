<script lang="ts">
  import { t } from '../../lib/i18n'
  import { untrack } from 'svelte'
  import { chatModes, modeDisplayName, modelDisplayName } from '../../lib/chatMode'
  import { settingsModalOpen } from '../../lib/stores'
  import type { ChatModeModel } from '../../lib/api'
  import PrivacyMark from '../ui/PrivacyMark.svelte'

  let {
    currentModelId = '',
    currentMode = 'default',
    onPick,
    onClose,
  }: {
    currentModelId?: string
    currentMode?: string
    onPick: (mode: string, model: ChatModeModel) => void
    onClose: () => void
  } = $props()

  // Accordion: the current mode's group starts expanded; every other group is
  // folded until clicked (需求 §5.6「先模式后模型」+ P9 §3.3「当前模式默认展开」).
  // untrack signals intent: openGroups is a one-shot snapshot of the prop, not
  // reactive to a live chat_mode change mid-menu (the user's toggles own it).
  let openGroups = $state<string[]>(untrack(() => [currentMode]))

  function toggleGroup(id: string) {
    openGroups = openGroups.includes(id)
      ? openGroups.filter(g => g !== id)
      : [...openGroups, id]
  }

  function isActive(model: ChatModeModel): boolean {
    if (!currentModelId) return false
    return model.compositeId === currentModelId || model.id === currentModelId
  }

  function pick(modeId: string, model: ChatModeModel) {
    // A model with no composite id is listed but not configured (P9 §3.2 —
    // the four buding-* models land in config.yml via P11). Don't let it be
    // selected; it's shown only so the factory list is visible.
    if (!model.compositeId) return
    onPick(modeId, model)
  }
</script>

<div class="menu mode-menu" role="menu" onclick={(e) => e.stopPropagation()}>
  {#if $chatModes.length === 0}
    <div class="menu-empty">{$t('mode.no_models')}</div>
  {:else}
    {#each $chatModes as mode (mode.id)}
      <button class="mode-header" onclick={() => toggleGroup(mode.id)}>
        <span class="mode-name">
          <!-- Only the active privacy group gets P10's shield; inactive
               groups retain P9's original presentation. OCTO-FORK: P10. -->
          <PrivacyMark mode={mode.id === currentMode ? mode.id : ''} />
          <span>{modeDisplayName(mode.id)}</span>
        </span>
        <iconify-icon
          icon={openGroups.includes(mode.id) ? 'lucide:chevron-down' : 'lucide:chevron-right'}
          width="12"
        ></iconify-icon>
      </button>
      {#if openGroups.includes(mode.id)}
        {#if mode.models.length === 0}
          <div class="menu-empty">{$t('mode.no_models')}</div>
        {:else}
          {#each mode.models as m (m.id)}
            <button
              class="menu-item"
              class:active={isActive(m)}
              class:disabled={!m.compositeId}
              onclick={() => pick(mode.id, m)}
            >
              <span class="mi-name">{modelDisplayName(m.id)}</span>
            </button>
          {/each}
        {/if}
      {/if}
    {/each}
    <div class="menu-divider"></div>
    <button class="menu-item manage" onclick={() => { onClose(); settingsModalOpen.set(true) }}>
      <span class="mi-name">{$t('composer.manage_models')}</span>
    </button>
  {/if}
</div>

<style>
  .menu {
    position: absolute; bottom: calc(100% + 6px); left: 0; z-index: 50;
    min-width: 220px; max-width: 320px; max-height: 320px; overflow-y: auto;
    background: var(--bg-container); border: 1px solid var(--border-secondary); border-radius: 10px;
    box-shadow: 0 8px 24px rgba(15,23,42,0.14); padding: 4px;
  }
  .mode-header {
    width: 100%; display: flex; align-items: center; justify-content: space-between;
    gap: 6px; padding: 6px 10px 4px; border: none; background: transparent;
    cursor: pointer; font-family: inherit; text-align: left;
  }
  .mode-name { display: inline-flex; align-items: center; gap: 5px; font-size: 11px; font-weight: 600; color: var(--text-secondary); }
  .menu-item {
    width: 100%; display: flex; flex-direction: column; gap: 1px; align-items: flex-start;
    padding: 7px 10px 7px 20px; border: none; background: transparent; border-radius: 6px;
    cursor: pointer; font-family: inherit; text-align: left;
  }
  .menu-item:hover { background: var(--active-blue-bg); }
  .menu-item.active { background: var(--active-blue-bg); }
  .menu-item.disabled { cursor: default; opacity: 0.5; }
  .menu-item.disabled:hover { background: none; }
  .menu-divider { height: 1px; background: var(--border-secondary); margin: 4px 0; }
  .mi-name { font-size: 13px; color: var(--text); }
  .menu-empty { padding: 8px 10px 8px 20px; font-size: 12px; color: var(--text-tertiary); }
  .menu-item.manage { flex-direction: row; align-items: center; gap: 6px; padding-left: 10px; }
  .menu-item.manage .mi-name { color: var(--blue-6); }
</style>
