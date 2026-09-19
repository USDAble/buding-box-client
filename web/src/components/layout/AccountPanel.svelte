<script lang="ts">
  import { t, tr } from '../../lib/i18n'
  import { productState, refreshCredits } from '../../lib/product'
  import { accountPanelOpen, openSettingsAt, showToast } from '../../lib/stores'
  import * as api from '../../lib/api'
  import { avatarInitial, avatarColor } from '../../lib/avatar'
  import { licenseView } from '../../lib/license'
  import { dismissIntent } from '../../lib/globalKeys'

  // OCTO-FORK: the account corner is a compact status and shortcut surface.
  // Editing, long lists, and destructive actions belong to full Settings.
  let { anchorEl, fixedWidth }: { anchorEl: HTMLElement | null; fixedWidth?: number } = $props()

  const nickname = $derived($productState?.account?.nickname ?? '')
  const phone = $derived($productState?.account?.phoneMasked ?? '—')
  const points = $derived($productState?.credits?.balance ?? 0)
  const planLabel = $derived($productState?.plan?.name === 'trial' ? $t('product.plan_trial') : ($productState?.plan?.name || '—'))
  const license = $derived(licenseView($productState?.activation?.expiresAt))
  const licenseDetail = $derived.by(() => {
    if (!license) return '—'
    if (license.state === 'active') {
      return license.daysLeft > 0
        ? $t('product.panel.license_active').replaceAll('{n}', String(license.daysLeft))
        : $t('product.panel.license_active_today')
    }
    return $t('product.panel.license_expired')
  })

  let rootEl = $state<HTMLDivElement | null>(null)
  let refreshing = $state(false)

  let checkingUpdate = $state(false)
  let pos = $state({ left: '0px', width: '0px', bottom: '0px', maxHeight: '0px' })

  function portal(node: HTMLElement) {
    document.body.appendChild(node)
    return { destroy() { node.remove() } }
  }

  $effect(() => {
    if (!anchorEl) return
    let raf = 0
    const tick = () => {
      const aside = anchorEl.closest('aside')
      const asideR = aside?.getBoundingClientRect() ?? anchorEl.getBoundingClientRect()
      const anchorR = anchorEl.getBoundingClientRect()
      pos.left = `${asideR.left}px`
      pos.width = `${fixedWidth ?? asideR.width}px`
      pos.bottom = `${window.innerHeight - anchorR.top}px`
      pos.maxHeight = `${Math.max(120, anchorR.top - 8)}px`
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  })

  $effect(() => { rootEl?.focus() })

  function onDocPointerDown(e: PointerEvent) {
    const target = e.target as Element | null
    if (!target || rootEl?.contains(target) || anchorEl?.contains(target)) return
    if (target.closest('[aria-modal="true"], .backdrop')) return
    accountPanelOpen.set(false)
  }

  $effect(() => {
    window.addEventListener('pointerdown', onDocPointerDown, true)
    return () => window.removeEventListener('pointerdown', onDocPointerDown, true)
  })

  function onKeydown(e: KeyboardEvent) {
    if (dismissIntent(e) === 'overlay') {
      e.preventDefault()
      accountPanelOpen.set(false)
    }
  }

  function openSettings(category: string) {
    accountPanelOpen.set(false)
    openSettingsAt(category)
  }

  async function reloadCredits() {
    if (refreshing) return
    refreshing = true
    try {
      await refreshCredits()
    } catch {
      showToast($t('product.panel.refresh_failed'), 'error')
    } finally {
      refreshing = false
    }
  }

  function upgrade() { showToast(tr('product.panel.upgrade_soon')) }
  function recharge() { showToast(tr('product.panel.recharge_soon')) }
  async function checkUpdates() {
    if (checkingUpdate) return
    checkingUpdate = true
    try {
      const result = await api.checkNativeUpdates()
      showToast(result.available
        ? $t('settings.update_available').replaceAll('{version}', result.latest)
        : $t('settings.update.uptodate'), result.available ? 'warning' : 'success')
    } catch { showToast($t('product.send_failed'), 'error') }
    finally { checkingUpdate = false }
  }
</script>

<div
  class="account-panel"
  use:portal
  bind:this={rootEl}
  style="left:{pos.left};width:{pos.width};bottom:{pos.bottom};max-height:{pos.maxHeight};"
  role="dialog"
  aria-modal="false"
  aria-label={$t('product.panel.manage_account')}
  tabindex="-1"
  onkeydown={onKeydown}
>
  <header class="summary">
    <span class="avatar" style="background:{avatarColor(nickname)}">{avatarInitial(nickname)}</span>
    <div class="sum-meta">
      <span class="sum-name">{nickname}</span>
      <span class="sum-phone">{phone}</span>
    </div>
  </header>

  <section class="membership" aria-label={$t('product.panel.account_info')}>
    <div class="status-row">
      <span class="label">{$t('product.panel.plan')}</span>
      <span class="value">{planLabel}</span>
      <button class="text-action" onclick={upgrade}>{$t('product.panel.upgrade')}</button>
    </div>
    <div class="status-row">
      <span class="label">{$t('product.panel.credits')}</span>
      <span class="value">{points}</span>
      <button class="text-action" onclick={recharge}>{$t('product.panel.recharge')}</button>
      <button class="icon-action" onclick={reloadCredits} disabled={refreshing} aria-label={$t('product.panel.refresh')}>
        <iconify-icon icon="ant-design:reload-outlined" width="13"></iconify-icon>
      </button>
    </div>
    <div class="status-row">
      <span class="label">{$t('product.panel.license')}</span>
      <span class="value">{licenseDetail}</span>
    </div>
  </section>

  <nav class="actions" aria-label={$t('product.panel.manage_account')}>
    <button class="action-row" onclick={() => openSettings('account')}>
      <iconify-icon icon="ant-design:user-outlined" width="15"></iconify-icon>
      <span>{$t('product.panel.manage_account')}</span>
      <iconify-icon class="chevron" icon="lucide:chevron-right" width="14"></iconify-icon>
    </button>
    <button class="action-row" onclick={() => openSettings('safety')}>
      <iconify-icon icon="ant-design:safety-outlined" width="15"></iconify-icon>
      <span>{$t('product.panel.safety_privacy')}</span>
      <iconify-icon class="chevron" icon="lucide:chevron-right" width="14"></iconify-icon>
    </button>
    <button class="action-row" onclick={() => openSettings('general')}>
      <iconify-icon icon="ant-design:setting-outlined" width="15"></iconify-icon>
      <span>{$t('nav.settings')}</span>
      <iconify-icon class="chevron" icon="lucide:chevron-right" width="14"></iconify-icon>
    </button>
    <button class="action-row" onclick={() => openSettings('help')}>
      <iconify-icon icon="ant-design:question-circle-outlined" width="15"></iconify-icon>
      <span>{$t('product.panel.help')}</span>
      <iconify-icon class="chevron" icon="lucide:chevron-right" width="14"></iconify-icon>
    </button>
    <button class="action-row" onclick={checkUpdates} disabled={checkingUpdate}>
      <iconify-icon icon="ant-design:reload-outlined" width="15"></iconify-icon>
      <span>{checkingUpdate ? $t('settings.update.checking') : $t('settings.update.check')}</span>
    </button>
    <button class="action-row" onclick={() => openSettings('about')}>
      <iconify-icon icon="ant-design:info-circle-outlined" width="15"></iconify-icon>
      <span>{$t('product.panel.about')}</span>
      <iconify-icon class="chevron" icon="lucide:chevron-right" width="14"></iconify-icon>
    </button>
  </nav>
</div>

<style>
  .account-panel { position: fixed; z-index: 800; background: var(--bg-container); border: 1px solid var(--border-secondary); border-bottom: none; border-radius: 12px 12px 0 0; box-shadow: 0 -8px 32px rgba(0,0,0,0.14); overflow-x: hidden; overflow-y: auto; animation: ap-rise 0.2s cubic-bezier(0.2,0,0,1); outline: none; }
  @keyframes ap-rise { from { transform: translateY(8px); opacity: 0.6; } to { transform: translateY(0); opacity: 1; } }
  .summary { display: flex; align-items: center; gap: 10px; padding: 14px; }
  .avatar { width: 40px; height: 40px; border-radius: 50%; display: grid; place-items: center; color: #fff; font-size: 17px; font-weight: 600; }
  .sum-meta { display: flex; flex-direction: column; min-width: 0; gap: 2px; }
  .sum-name { color: var(--text-heading); font-size: 14px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sum-phone { color: var(--text-secondary); font-size: 12px; }
  .membership { margin: 0 8px 8px; border: 1px solid var(--border-secondary); border-radius: 9px; overflow: hidden; }
  .status-row { display: flex; align-items: center; gap: 7px; min-height: 36px; padding: 0 10px; border-bottom: 1px solid var(--border-secondary); }
  .status-row:last-child { border-bottom: none; }
  .label { flex: 0 0 auto; color: var(--text-secondary); font-size: 12px; }
  .value { min-width: 0; flex: 1; color: var(--text); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .text-action, .icon-action { border: none; background: transparent; color: var(--blue-6); font: inherit; font-size: 12px; cursor: pointer; padding: 3px; }
  .icon-action { color: var(--text-tertiary); display: grid; place-items: center; }
  .text-action:hover, .icon-action:hover:not(:disabled) { color: var(--blue-5); }
  .icon-action:disabled { opacity: .5; cursor: progress; }
  .actions { display: flex; flex-direction: column; gap: 2px; padding: 0 8px 10px; }
  .action-row { width: 100%; height: 36px; display: flex; align-items: center; gap: 9px; padding: 0 10px; border: none; border-radius: 8px; background: transparent; color: var(--text); font: inherit; font-size: 13px; text-align: left; cursor: pointer; }
  .action-row:hover { background: var(--hover-neutral); }
  .action-row span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .chevron { color: var(--text-quaternary); }
</style>
