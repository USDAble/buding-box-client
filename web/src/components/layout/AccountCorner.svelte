<script lang="ts">
  import { t } from '../../lib/i18n'
  import { productState } from '../../lib/product'
  import { accountPanelOpen, accountPanelPage } from '../../lib/stores'
  import { avatarInitial, avatarColor } from '../../lib/avatar'

  // Bottom-left account corner (P5), replacing the old "settings + version"
  // footer pair. Clicking toggles the account panel; the panel opens anchored
  // ABOVE this element (AccountPanel measures its rect), so the corner stays
  // visible while the panel is up and doubles as the collapse anchor.
  //
  // Two shapes: rail mode shows only the avatar (64px cannot hold text), full
  // mode adds nickname + points. The nickname is ellipsised, never truncated
  // by character count — CJK/Latin mixes cut badly by count (P5 §4.1).
  //
  // OCTO-FORK: P5 account corner — see
  // dev-docs-usdable/需求/2260906/技术方案/P5-个人中心.md.

  let { rail = false }: { rail?: boolean } = $props()

  const nickname = $derived($productState?.account?.nickname ?? '')
  const points = $derived($productState?.credits?.balance ?? 0)
  // $t (not tr): the label must re-render when the language switches, and the
  // corner has no other locale-reactive text to piggyback on.
  const pointsLabel = $derived($t('product.panel.points_value').replaceAll('{n}', String(points)))

  function toggle() {
    if ($accountPanelOpen) {
      accountPanelOpen.set(false)
    } else {
      // Reopening always lands on the navigation list, not the last subpage.
      accountPanelPage.set('root')
      accountPanelOpen.set(true)
    }
  }
</script>

{#if nickname}
  <button
    class="corner"
    class:open={$accountPanelOpen}
    class:rail
    data-account-corner
    title={nickname}
    aria-haspopup="dialog"
    aria-expanded={$accountPanelOpen}
    onclick={toggle}
  >
    <span class="avatar" style="background:{avatarColor(nickname)}">{avatarInitial(nickname)}</span>
    {#if !rail}
      <span class="meta">
        <span class="name">{nickname}</span>
        <span class="points">{pointsLabel}</span>
      </span>
    {/if}
  </button>
{/if}

<style>
  .corner {
    display: flex; align-items: center; gap: 10px;
    min-width: 0; max-width: 100%;
    padding: 4px 8px; border: none; border-radius: 10px;
    background: transparent; cursor: pointer; font-family: inherit;
    text-align: left;
  }
  .corner:hover { background: var(--hover-neutral); }
  .corner.open { background: var(--active-blue-bg); }
  .corner.rail { padding: 4px; border-radius: 9999px; }
  .corner.rail .avatar { width: 32px; height: 32px; font-size: 14px; }
  .avatar {
    width: 30px; height: 30px; border-radius: 50%; flex: 0 0 auto;
    display: flex; align-items: center; justify-content: center;
    color: #fff; font-size: 13px; font-weight: 600;
  }
  .meta { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .name {
    font-size: 13px; font-weight: 600; color: var(--text-heading);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 150px;
  }
  .points { font-size: 11px; color: var(--text-secondary); font-variant-numeric: tabular-nums; }
</style>
