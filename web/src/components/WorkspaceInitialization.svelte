<script lang="ts">
  import { initializeWorkspace, windowToken, workspaceModules, workspaceDictionaryFallback } from '../lib/product'
  import { t } from '../lib/i18n'

  let loading = $state(false)
  let requestFailed = $state(false)
  const failed = $derived(requestFailed ? ['all'] : Object.entries($workspaceModules).filter(([, status]) => status !== 'ready').map(([name]) => name))
  let controller: AbortController | undefined

  async function refresh() {
    controller?.abort()
    const request = new AbortController()
    controller = request
    loading = true
    try {
      await initializeWorkspace(request.signal)
      if (!request.signal.aborted && controller === request) requestFailed = false
    } catch {
      if (!request.signal.aborted && controller === request) requestFailed = true
    } finally {
      if (!request.signal.aborted && controller === request) loading = false
    }
  }

  $effect(() => {
    if (!windowToken()) return
    void refresh()
    return () => controller?.abort()
  })
</script>

{#if loading || failed.length > 0}
  <div class="workspace-status" role="status">
    {#if loading}
      <span>{$t('product.init.loading')}</span>
    {:else}
      <span>{$t('product.init.failed')}: {failed.map(name => $t(`product.init.${name}`)).join('、')}
        {#if $workspaceDictionaryFallback} · {$t('product.init.dictionary_fallback')}{/if}
      </span>
      <button onclick={refresh}>{$t('common.refresh')}</button>
    {/if}
  </div>
{/if}

<style>
  .workspace-status { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 16px; font-size: 12px; background: var(--bg-secondary); color: var(--text-secondary); }
  button { cursor: pointer; flex-shrink: 0; }
</style>
