<script lang="ts">
  import { accountPanelOpen, closeAccountPanel, openAccountPanel, sidebar } from '../../lib/stores'
  import { productState } from '../../lib/product'
  import { t } from '../../lib/i18n'

  export let rail = false

  function toggle() {
    if ($accountPanelOpen) {
      closeAccountPanel()
      if (rail) sidebar.set('rail')
    } else {
      if (rail) sidebar.set('full')
      openAccountPanel(rail)
    }
  }
</script>

<script lang="ts" context="module">
  export function avatarInitial(nickname: string): string {
    const first = Array.from(nickname.trim())[0] ?? '?'
    return /[a-z]/i.test(first) ? first.toUpperCase() : first
  }
</script>

<button class="account-corner" class:active={$accountPanelOpen} aria-expanded={$accountPanelOpen}
  aria-label={$t('product.account')} onclick={toggle}>
  <span class="avatar">{avatarInitial($productState?.account?.nickname ?? '')}</span>
  <span class="account-copy">
    <strong>{$productState?.account?.nickname ?? '?'}</strong>
    <small>{$t('product.account_credits')} {$productState?.credits?.balance ?? 0}</small>
  </span>
</button>

<style>
.account-corner { position:relative; z-index:30; width:100%; display:flex; align-items:center; gap:10px; padding:9px 10px; border:0; border-radius:8px; background:transparent; color:var(--text); cursor:pointer; text-align:left; font:inherit; }
.account-corner:hover,.account-corner.active { background:var(--hover-neutral); }
.avatar { width:32px; height:32px; flex:0 0 32px; display:grid; place-items:center; border-radius:50%; background:var(--blue-2); color:var(--blue-7); font-size:14px; font-weight:600; }
.account-copy { min-width:0; display:flex; flex-direction:column; gap:2px; }
.account-copy strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:13px; font-weight:600; }
.account-copy small { color:var(--text-tertiary); font-size:11px; }
:global(.rail) .account-corner { justify-content:center; padding:8px 0; }
:global(.rail) .account-copy { display:none; }
</style>
