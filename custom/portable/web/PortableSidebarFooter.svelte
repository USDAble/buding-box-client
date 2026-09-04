<script>
  import { settingsModalOpen } from '../../../web/src/lib/stores'
  import OctoLogo from '../../../web/src/components/layout/OctoLogo.svelte'
  import { portableT } from './portable-i18n'

  const PROFILE_STORAGE_KEY = 'buding_box_portable_activation_profile'
  /** @type {any} 兼容旧版编辑器，运行时仍复用上游品牌组件。 */
  const Logo = OctoLogo

  let nickname = ''
  try {
    const profile = JSON.parse(localStorage.getItem(PROFILE_STORAGE_KEY) || '{}')
    if (typeof profile.nickname === 'string') nickname = profile.nickname.trim()
  } catch {
    // U 盘资料损坏时使用本地化默认昵称，不影响进入桌面。
  }

  $: displayName = nickname || $portableT('profile.defaultName')
</script>

<div class="portable-sidebar-footer" data-portable-sidebar-footer>
  <button
    class="profile"
    type="button"
    title={$portableT('profile.openSettings')}
    aria-label={`${$portableT('profile.openSettings')}：${displayName}`}
    on:click={() => settingsModalOpen.set(true)}
  >
    <span class="avatar" aria-hidden="true"><Logo size={38} /></span>
    <span class="name">{displayName}</span>
  </button>

  <span class="messages" role="img" title={$portableT('profile.messages')} aria-label={$portableT('profile.messages')}>
    <iconify-icon icon="lucide:bell" width="20"></iconify-icon>
  </span>
</div>

<style>
  .portable-sidebar-footer {
    width: 100%;
    min-width: 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .profile {
    min-width: 0;
    min-height: 44px;
    padding: 3px 7px 3px 3px;
    border: 0;
    border-radius: 999px;
    display: flex;
    align-items: center;
    gap: 10px;
    background: transparent;
    color: var(--text-heading);
    font: inherit;
    cursor: pointer;
    transition: background-color 180ms ease;
  }

  .profile:hover { background: var(--hover-neutral); }
  .profile:focus-visible {
    outline: 3px solid var(--focus-ring);
    outline-offset: 2px;
  }

  .avatar {
    width: 38px;
    height: 38px;
    flex: 0 0 38px;
    overflow: hidden;
    border-radius: 50%;
    color: #15c9a5;
  }

  .avatar :global(svg) { display: block; }

  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 14px;
    font-weight: 650;
  }

  .messages {
    width: 44px;
    height: 44px;
    flex: 0 0 44px;
    display: grid;
    place-items: center;
    color: var(--text-secondary);
  }
</style>
