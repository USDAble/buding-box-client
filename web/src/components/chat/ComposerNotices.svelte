<script lang="ts">
  // Ordered notice strip that sits above or below the composer input. The
  // array order IS the display order: P8 (sensitive-word hit) and P10 (privacy
  // notice) append their own entries later, so the ordering rule lives in ONE
  // place — the Composer's notices array — instead of three PRs each drawing
  // their own strip (P6-入口隐藏与积分.md §3.6).
  //
  // The component renders the list it is given verbatim; the Composer splits
  // notices by `slot` and mounts one instance above the input card ('above')
  // and one below it ('below'). `slot` is part of the contract so a notice can
  // declare where it belongs without the Composer hardcoding per-entry rules.
  export interface Notice {
    id: string
    level: 'info' | 'warn'
    text: string
    slot: 'above' | 'below'
  }

  let { notices = [] }: { notices?: Notice[] } = $props()
</script>

{#if notices.length > 0}
  <div class="notices" data-composer-notices>
    {#each notices as n (n.id)}
      <div class="notice" class:warn={n.level === 'warn'}>
        <iconify-icon
          icon={n.level === 'warn' ? 'ant-design:exclamation-circle-filled' : 'ant-design:info-circle-filled'}
          width="13"
        ></iconify-icon>
        <span>{n.text}</span>
      </div>
    {/each}
  </div>
{/if}

<style>
  .notices {
    display: flex;
    flex-direction: column;
    gap: 3px;
    max-width: var(--chat-content-max-width, 1080px);
    margin: 0 auto;
    padding: 4px 24px 0;
  }
  .notice {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    line-height: 1.4;
    color: var(--text-secondary);
  }
  .notice.warn {
    color: var(--orange-6, #d46b08);
  }
  .notice iconify-icon {
    flex: 0 0 auto;
  }
</style>
