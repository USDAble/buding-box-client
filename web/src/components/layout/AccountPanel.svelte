<script lang="ts">
  import { t, tr } from '../../lib/i18n'
  import { productState, logout } from '../../lib/product'
  import { accountPanelOpen, accountPanelPage, showToast, type AccountPanelPage } from '../../lib/stores'
  import { confirmDialog } from '../../lib/confirm'
  import { avatarInitial, avatarColor } from '../../lib/avatar'
  import { licenseView } from '../../lib/license'
  import { dismissIntent } from '../../lib/globalKeys'
  import PlanPage from './account/PlanPage.svelte'
  import CreditsPage from './account/CreditsPage.svelte'
  import LicensePage from './account/LicensePage.svelte'
  import SettingsPage from './account/SettingsPage.svelte'
  import SensitiveDictPage from './account/SensitiveDictPage.svelte'
  import HelpPage from './account/HelpPage.svelte'
  import AboutPage from './account/AboutPage.svelte'

  // Account panel (P5): a bottom-anchored column exactly as wide as the
  // sidebar, covering it, with the trigger corner staying visible below as the
  // "tap again to collapse" anchor. Navigation rows plus shallow secondary
  // pages swap inside this one surface (never a second overlay).
  //
  // It is portaled to <body>: the sidebar <aside> clips its overflow (needed
  // for the width-collapse transition) and, in rail mode, the panel is WIDER
  // than the 64px rail, so an in-aside child would be cut off. Fixed geometry
  // is re-read every frame from the trigger's rect, so a mid-open sidebar
  // drag or window resize tracks without wiring every resize source.
  //
  // OCTO-FORK: P5 account panel — see
  // dev-docs-usdable/需求/2260906/技术方案/P5-个人中心.md.

  let { anchorEl, fixedWidth }: { anchorEl: HTMLElement | null; fixedWidth?: number } = $props()

  const nickname = $derived($productState?.account?.nickname ?? '')
  const points = $derived($productState?.credits?.balance ?? 0)
  const planLabel = $derived($productState?.plan?.name === 'trial' ? $t('product.plan_trial') : ($productState?.plan?.name || '—'))
  const license = $derived(licenseView($productState?.activation?.expiresAt))
  const pointsLabel = $derived($t('product.panel.points_value').replaceAll('{n}', String(points)))
  const licenseDetail = $derived.by(() => {
    if (!license) return ''
    if (license.state === 'active') {
      return license.daysLeft > 0
        ? $t('product.panel.license_active').replaceAll('{n}', String(license.daysLeft))
        : $t('product.panel.license_active_today')
    }
    return $t('product.panel.license_expired')
  })

  let rootEl = $state<HTMLDivElement | null>(null)
  let pos = $state({ left: '0px', width: '0px', bottom: '0px', maxHeight: '0px' })
  const page = $derived($accountPanelPage)

  // Move the surface to <body> (same reason the sidebar's other flyouts do).
  // The action receives the node Svelte has already mounted.
  function portal(node: HTMLElement) {
    document.body.appendChild(node)
    return { destroy() { node.remove() } }
  }

  // Re-measure every frame while open. getBoundingClientRect on two elements
  // per frame is trivial; it removes any need to hook every resize path
  // (sidebar drag, window resize, rail↔full switch, font load). The surface's
  // LEFT edge follows the sidebar's (not the footer's inner padding) so the
  // panel overlays the whole sidebar width; its BOTTOM stops at the footer's
  // top edge, keeping the corner visible below.
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

  // Focus for Esc (same pattern as the other overlays: bind the key handler
  // to the surface, never a window-level listener that would also fire for
  // whatever is focused underneath — see ConfirmModal #1105).
  $effect(() => {
    rootEl?.focus()
  })

  // Outside click closes. The corner itself must NOT close here — its own
  // click handler toggles, and pointerdown firing first would swallow the
  // toggle. A true modal opened ABOVE the panel (logout confirm, full
  // settings, palette) owns every pointerdown until it dismisses itself, so
  // one made inside it must not collapse the panel underneath mid-interaction.
  function onDocPointerDown(e: PointerEvent) {
    const t = e.target as Element | null
    if (!t) return
    if (rootEl?.contains(t)) return
    if (anchorEl?.contains(t)) return
    if (t.closest('[aria-modal="true"], .backdrop')) return
    accountPanelOpen.set(false)
  }

  $effect(() => {
    window.addEventListener('pointerdown', onDocPointerDown, true)
    return () => window.removeEventListener('pointerdown', onDocPointerDown, true)
  })

  // Esc mapping lives in lib/globalKeys (P5 §4.2) so the panel shares one
  // definition of "plain Esc" with anything else that dismisses; bound on the
  // focused surface like the other overlays, not on the window.
  function onKeydown(e: KeyboardEvent) {
    if (dismissIntent(e) === 'overlay') {
      e.preventDefault()
      accountPanelOpen.set(false)
    }
  }

  function go(page: AccountPanelPage) {
    accountPanelPage.set(page)
  }

  function goRoot() {
    accountPanelPage.set('root')
  }

  function pageTitle(p: AccountPanelPage): string {
    switch (p) {
      case 'plan': return tr('product.panel.plan')
      case 'credits': return tr('product.panel.credits')
      case 'license': return tr('product.panel.license')
      case 'settings': return tr('nav.settings')
      case 'sensitive': return tr('product.panel.sensitive_library')
      case 'help': return tr('product.panel.help')
      case 'about': return tr('product.panel.about')
      default: return ''
    }
  }

  async function onLogout() {
    const ok = await confirmDialog(tr('product.panel.logout_confirm'), {
      title: tr('product.panel.logout'),
      danger: true,
      confirmLabel: tr('product.panel.logout'),
    })
    if (!ok) return
    try {
      await logout()
      accountPanelOpen.set(false)
      accountPanelPage.set('root')
    } catch {
      showToast(tr('product.send_failed'), 'error')
    }
  }

  function onSoon() {
    showToast(tr('product.panel.soon'))
  }
</script>

<div
  class="account-panel"
  use:portal
  bind:this={rootEl}
  style="left:{pos.left};width:{pos.width};bottom:{pos.bottom};max-height:{pos.maxHeight};"
  role="dialog"
  aria-modal="false"
  aria-label={$t('product.panel.plan')}
  tabindex="-1"
  onkeydown={onKeydown}
>
  {#if page === 'root'}
    <div class="root">
      <div class="summary">
        <span class="avatar" style="background:{avatarColor(nickname)}">{avatarInitial(nickname)}</span>
        <div class="sum-meta">
          <span class="sum-name">{nickname}</span>
          <span class="sum-points">{pointsLabel}</span>
        </div>
      </div>

      <div class="nav">
        <button class="nav-row" onclick={() => go('plan')}>
          <span class="lbl">{$t('product.panel.plan')}</span>
          <span class="detail">{planLabel}</span>
        </button>
        <button class="nav-row" onclick={() => go('credits')}>
          <span class="lbl">{$t('product.panel.credits')}</span>
          <span class="detail">{points}</span>
        </button>
        <button class="nav-row" onclick={() => go('license')}>
          <span class="lbl">{$t('product.panel.license')}</span>
          <span class="detail">{licenseDetail}</span>
        </button>
        <button class="nav-row" onclick={() => go('settings')}>
          <span class="lbl">{$t('nav.settings')}</span>
          <iconify-icon icon="lucide:chevron-right" width="13" style="color:var(--text-quaternary)"></iconify-icon>
        </button>

        <button class="nav-row" onclick={() => go('sensitive')}>
          <span class="lbl">{$t('product.panel.sensitive_library')}</span>
          <iconify-icon icon="lucide:chevron-right" width="13" style="color:var(--text-quaternary)"></iconify-icon>
        </button>

        <button class="nav-row" onclick={() => go('help')}>
          <span class="lbl">{$t('product.panel.help')}</span>
          <iconify-icon icon="lucide:chevron-right" width="13" style="color:var(--text-quaternary)"></iconify-icon>
        </button>

        <div class="nav-row disabled" role="button" tabindex="-1" title={$t('product.panel.soon')}>
          <span class="lbl">{$t('product.panel.check_updates')}</span>
          <span class="detail">{$t('product.panel.soon')}</span>
        </div>

        <button class="nav-row" onclick={() => go('about')}>
          <span class="lbl">{$t('product.panel.about')}</span>
          <iconify-icon icon="lucide:chevron-right" width="13" style="color:var(--text-quaternary)"></iconify-icon>
        </button>
      </div>

      <div class="foot">
        <button class="logout" onclick={onLogout}>
          {$t('product.panel.logout')}
        </button>
      </div>
    </div>
  {:else}
    <div class="sub">
      <div class="page-head">
        <button class="back" aria-label={$t('product.panel.back')} onclick={goRoot}>
          <iconify-icon icon="ant-design:left-outlined" width="14"></iconify-icon>
        </button>
        <span class="page-title">{pageTitle(page)}</span>
      </div>
      <div class="page-body">
        {#if page === 'plan'}
          <PlanPage />
        {:else if page === 'credits'}
          <CreditsPage />
        {:else if page === 'license'}
          <LicensePage />
        {:else if page === 'settings'}
          <SettingsPage />
        {:else if page === 'sensitive'}
          <SensitiveDictPage />
        {:else if page === 'help'}
          <HelpPage />
        {:else}
          <AboutPage />
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .account-panel {
    position: fixed;
    z-index: 800;
    display: flex; flex-direction: column;
    background: var(--bg-container);
    border: 1px solid var(--border-secondary);
    border-bottom: none;
    border-radius: 12px 12px 0 0;
    box-shadow: 0 -8px 32px rgba(0,0,0,0.14);
    overflow-x: hidden; overflow-y: auto;
    animation: ap-rise 0.2s cubic-bezier(0.2,0,0,1);
    outline: none;
  }
  @keyframes ap-rise {
    from { transform: translateY(8px); opacity: 0.6; }
    to { transform: translateY(0); opacity: 1; }
  }

  .root { display: flex; flex-direction: column; min-height: 100%; }
  .summary {
    display: flex; align-items: center; gap: 10px;
    padding: 14px 14px 12px; flex: 0 0 auto;
  }
  .avatar {
    width: 40px; height: 40px; border-radius: 50%; flex: 0 0 auto;
    display: flex; align-items: center; justify-content: center;
    color: #fff; font-size: 17px; font-weight: 600;
  }
  .sum-meta { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .sum-name {
    font-size: 14px; font-weight: 600; color: var(--text-heading);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .sum-points { font-size: 12px; color: var(--text-secondary); font-variant-numeric: tabular-nums; }

  .nav { padding: 0 8px 8px; display: flex; flex-direction: column; }
  .nav-row {
    display: flex; align-items: center; gap: 8px;
    width: 100%; height: 36px; padding: 0 10px;
    border: none; border-radius: 8px; background: transparent;
    font-family: inherit; font-size: 13px; color: var(--text);
    cursor: pointer; text-align: left;
  }
  .nav-row:hover:not(.disabled) { background: var(--hover-neutral); }
  .nav-row.disabled { cursor: default; opacity: 0.55; color: var(--text-secondary); }
  .nav-row .lbl { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .detail { font-size: 12px; color: var(--text-tertiary); font-variant-numeric: tabular-nums; white-space: nowrap; }

  .foot { flex: 0 0 auto; position: sticky; bottom: 0; background: var(--bg-container); border-top: 1px solid var(--border-secondary); padding: 8px; }
  .logout {
    display: flex; align-items: center; justify-content: center; gap: 6px;
    width: 100%; height: 34px; border: none; border-radius: 8px;
    background: transparent; color: var(--error);
    font-family: inherit; font-size: 13px; font-weight: 500; cursor: pointer;
  }
  .logout:hover { background: var(--error-bg, rgba(255,59,48,0.08)); }

  .sub { display: flex; flex-direction: column; min-height: 100%; }
  .page-head {
    display: flex; align-items: center; gap: 6px;
    padding: 10px 12px; border-bottom: 1px solid var(--border-secondary); flex: 0 0 auto;
    position: sticky; top: 0; background: var(--bg-container); z-index: 1;
  }
  .back {
    display: flex; align-items: center; justify-content: center;
    width: 26px; height: 26px; border: none; border-radius: 6px;
    background: transparent; color: var(--text-secondary); cursor: pointer;
  }
  .back:hover { background: var(--hover-neutral); color: var(--text); }
  .page-title { font-size: 14px; font-weight: 600; color: var(--text-heading); }
  .page-body { padding: 12px 14px 4px; }
</style>
