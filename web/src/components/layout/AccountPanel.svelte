<script lang="ts">
  import { accountPanelOpen, accountPanelPage, closeAccountPanel, settingsModalOpen, type AccountPanelPage } from '../../lib/stores'
  import { productState, updateNickname, updateProductPrefs, logout } from '../../lib/product'
  import { confirmDialog } from '../../lib/confirm'
  import { locale, setLocale, t, tr } from '../../lib/i18n'
  import { licenseDaysLeft, licenseStatus } from '../../lib/license'
  import VersionBadge from './VersionBadge.svelte'

  let nickname = $state('')
  let saving = $state(false)
  let error = $state('')

  const pages = [
    ['plan', 'product.account_plan'], ['credits', 'product.account_credits'],
    ['license', 'product.account_license'], ['settings', 'product.account_settings'],
    ['sensitive', 'product.account_sensitive'], ['help', 'product.account_help'],
    ['updates', 'product.account_updates'], ['about', 'product.account_about'],
  ] as const

  $effect(() => {
    if ($accountPanelPage === 'settings') nickname = $productState?.account?.nickname ?? ''
  })

  function go(page: AccountPanelPage) { accountPanelPage.set(page) }
  function closeOnEscape(e: KeyboardEvent) {
    if (e.key !== 'Escape') return
    e.preventDefault(); e.stopPropagation(); closeAccountPanel()
  }
  function outside(e: MouseEvent) {
    const target = e.target as HTMLElement
    if (target.closest('.account-panel, .account-corner')) return
    closeAccountPanel()
  }
  async function saveNickname() {
    saving = true; error = ''
    try { await updateNickname(nickname); } catch (e: any) { error = e?.code === 'nickname_sensitive' ? tr('product.err_nickname_sensitive') : tr('product.err_nickname') } finally { saving = false }
  }
  async function saveLocale(value: 'zh' | 'en') {
    try { await updateProductPrefs({ locale: value }); setLocale(value) } catch { /* keep current language on failure */ }
  }
  async function saveMode(value: 'privacy' | 'smart' | 'default') {
    try { await updateProductPrefs({ defaultChatMode: value }) } catch {
      // The select is controlled by productState, so failed writes snap back.
    }
  }
  async function doLogout() {
    const ok = await confirmDialog(tr('product.logout_message'), { title: tr('product.logout_title'), danger: true, confirmLabel: tr('product.logout') })
    if (!ok) return
    await logout().catch(() => {})
    closeAccountPanel()
  }
</script>

<svelte:window onkeydown={closeOnEscape} onclick={outside} />

{#if $accountPanelOpen}
    <section class="account-panel" role="dialog" aria-label={$t('product.account')}>
    {#if $accountPanelPage === 'root'}
      <div class="summary">
        <span class="avatar">{Array.from($productState?.account?.nickname ?? '?')[0] ?? '?'}</span>
        <div><strong>{$productState?.account?.nickname ?? '?'}</strong><small>{$t('product.account_credits')} {$productState?.credits?.balance ?? 0}</small></div>
      </div>
      <nav>
        {#each pages as [page, label]}
          <button class="nav-item" onclick={() => go(page)}><span>{$t(label)}</span>{#if page === 'plan'}<small>{$t('product.trial')}</small>{:else if page === 'license'}<small>{$t('product.license_active')}</small>{:else if page === 'sensitive'}<small>{$t('product.coming_soon')}</small>{/if}</button>
        {/each}
      </nav>
    {:else}
      <header><button class="back" onclick={() => go('root')}>‹ {$t('product.account_back')}</button><strong>{$t(pages.find(p => p[0] === $accountPanelPage)?.[1] ?? 'product.account')}</strong></header>
      <div class="detail">
        {#if $accountPanelPage === 'plan'}
          <h3>{$t('product.trial')}</h3><p>{$t('product.coming_soon')}</p><button class="primary">{$t('product.upgrade')}</button>
        {:else if $accountPanelPage === 'credits'}
          <div class="metric">{$productState?.credits?.balance ?? 0}</div><p>{$t('product.account_credits')} · {$productState?.credits?.monthUsed ?? 0}</p><button class="primary">{$t('product.recharge')}</button>
        {:else if $accountPanelPage === 'license'}
          {@const activation = $productState?.activation}
          {@const expired = activation ? licenseStatus(activation.expiresAt) === 'expired' : true}
          <h3>{$t(expired ? 'product.license_expired' : 'product.license_active')}</h3>
          <p>{$t('product.days_left').replace('{n}', String(activation ? licenseDaysLeft(activation.expiresAt) : 0))}</p>
          <p>{$productState?.account?.phoneMasked ?? ''}</p><p>{$t('product.bound_directory')}</p>
        {:else if $accountPanelPage === 'settings'}
          <label>{$t('product.nickname_label')}<input bind:value={nickname} maxlength="16" /></label>
          {#if error}<p class="error">{error}</p>{/if}
          <button class="primary" disabled={saving} onclick={saveNickname}>{saving ? $t('product.submitting') : $t('product.nickname_save')}</button>
          <p class="muted">{$productState?.account?.phoneMasked ?? ''}</p>
          <label>{$t('product.account_settings')} · Language<select value={$locale.startsWith('zh') ? 'zh' : 'en'} onchange={(e) => saveLocale((e.currentTarget as HTMLSelectElement).value as 'zh' | 'en')}><option value="zh">中文</option><option value="en">English</option></select></label>
          <label>{$t('product.default_chat_mode')}<select value={$productState?.prefs?.defaultChatMode ?? 'default'} onchange={(e) => saveMode((e.currentTarget as HTMLSelectElement).value as 'privacy' | 'smart' | 'default')}><option value="privacy">{$t('product.mode_privacy')}</option><option value="smart">{$t('product.mode_smart')}</option><option value="default">{$t('product.mode_default')}</option></select></label>
          <button class="secondary" onclick={() => { closeAccountPanel(); settingsModalOpen.set(true) }}>{$t('product.open_full_settings')}</button>
        {:else if $accountPanelPage === 'help'}
          <p>{$t('product.help_demo')}</p><p>{$t('product.help_credits')}</p><p>{$t('product.help_portable')}</p><p>{$t('product.help_contact')}</p><p>{$t('product.help_smartscreen')}</p>
        {:else if $accountPanelPage === 'updates'}
          <p>{$t('product.coming_soon')}</p>
        {:else if $accountPanelPage === 'about'}
          <h3>{$t('product.account')}</h3><VersionBadge /><p>{$t('product.coming_soon')}</p>
        {:else}
          <p>{$t('product.account_sensitive')}: {$t('product.coming_soon')}</p>
        {/if}
      </div>
    {/if}
    <footer><button class="logout" onclick={doLogout}>{$t('product.logout')}</button></footer>
  </section>
{/if}

<style>
.account-panel { position:absolute; z-index:20; inset:0; display:flex; flex-direction:column; padding:12px; background:var(--bg-container); box-shadow:0 0 18px rgba(0,0,0,.16); animation:account-rise .2s ease-out; }
@keyframes account-rise { from { transform:translateY(100%); } to { transform:translateY(0); } }
.summary { display:flex; align-items:center; gap:10px; padding:8px 4px 14px; border-bottom:1px solid var(--border-secondary); }
.summary div { min-width:0; display:flex; flex-direction:column; gap:3px; }.summary strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.summary small,.nav-item small,.muted { color:var(--text-tertiary); font-size:11px; }
.avatar { width:36px;height:36px;display:grid;place-items:center;border-radius:50%;background:var(--blue-2);color:var(--blue-7);font-weight:600; }
nav,.detail { overflow:auto; flex:1; min-height:0; }.nav-item { width:100%; display:flex; justify-content:space-between; padding:10px 8px; border:0; border-radius:6px; background:transparent; color:var(--text); cursor:pointer; text-align:left; font:inherit; }.nav-item:hover { background:var(--hover-neutral); }
header { display:flex; align-items:center; gap:10px; padding:4px 0 12px; border-bottom:1px solid var(--border-secondary); }.back { border:0;background:transparent;color:var(--blue-6);cursor:pointer;font:inherit; }.detail { padding:16px 4px; }.detail h3 { margin:0 0 8px; }.detail p { line-height:1.6; color:var(--text-secondary); }.detail label { display:flex; flex-direction:column; gap:5px; margin-bottom:13px; font-size:12px; color:var(--text-secondary); }.detail input,.detail select { box-sizing:border-box; width:100%; padding:7px 8px; border:1px solid var(--border); border-radius:5px; background:var(--bg-container); color:var(--text); font:inherit; }.primary,.secondary { margin:5px 0 12px; padding:8px 11px; border-radius:6px; cursor:pointer; font:inherit; }.primary { border:0; background:var(--blue-6);color:#fff; }.secondary { border:1px solid var(--border);background:transparent;color:var(--text); }.error { color:var(--error)!important; }.metric { font-size:30px;font-weight:600; }.logout { width:100%; padding:9px; border:1px solid var(--error); border-radius:6px; background:transparent;color:var(--error);cursor:pointer;font:inherit; }footer { padding-top:10px; border-top:1px solid var(--border-secondary); }
</style>
