<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from '../../lib/i18n'
  import { getPersonalInfoRules } from '../../lib/api'
  import SensitiveDictPage from './SensitiveDictPage.svelte'

  // OCTO-FORK: Settings presents the registered personal-information rules
  // without owning a second copy of their identifiers or detection details.
  type Tab = 'personal' | 'dictionary'

  let tab = $state<Tab>('personal')
  let ruleVersion = $state('')
  let rules = $state<string[]>([])
  let loading = $state(true)
  let failed = $state(false)

  onMount(async () => {
    try {
      const result = await getPersonalInfoRules()
      ruleVersion = result.ruleVersion
      rules = result.rules
    } catch {
      failed = true
    } finally {
      loading = false
    }
  })

  function ruleLabel(id: string): string {
    const key = `privacy.category.${id}`
    const label = $t(key)
    return label === key ? id : label
  }
</script>

<div class="safety">
  <div class="tabs" role="tablist" aria-label={$t('settings.safety')}>
    <button
      class:on={tab === 'personal'}
      role="tab"
      aria-selected={tab === 'personal'}
      onclick={() => (tab = 'personal')}
    >{$t('settings.safety.personal_rules')}</button>
    <button
      class:on={tab === 'dictionary'}
      role="tab"
      aria-selected={tab === 'dictionary'}
      onclick={() => (tab = 'dictionary')}
    >{$t('settings.safety.dictionary')}</button>
  </div>

  {#if tab === 'personal'}
    <section class="rules" role="tabpanel">
      <h2>{$t('settings.safety.personal_rules')}</h2>
      <p class="intro">{$t('settings.safety.personal_rules_desc')}</p>
      <p class="scope">{$t('settings.safety.rules_scope')}</p>

      {#if loading}
        <p class="note">{$t('settings.safety.rules_loading')}</p>
      {:else if failed}
        <p class="note error">{$t('settings.safety.rules_unavailable')}</p>
      {:else}
        <div class="rule-meta">
          <span>{$t('settings.safety.rules_version').replace('{version}', ruleVersion)}</span>
          <span>{$t('settings.safety.rules_readonly')}</span>
        </div>
        <ul class="rule-list">
          {#each rules as rule (rule)}
            <li>{ruleLabel(rule)}</li>
          {/each}
        </ul>
      {/if}
    </section>
  {:else}
    <section class="dictionary" role="tabpanel">
      <p class="intro">{$t('settings.safety.dictionary_desc')}</p>
      <SensitiveDictPage />
    </section>
  {/if}
</div>

<style>
  .safety { display: flex; flex-direction: column; min-height: 0; }
  .tabs { display: flex; gap: 6px; padding-bottom: 16px; border-bottom: 1px solid var(--border-secondary); }
  .tabs button {
    height: 32px; padding: 0 12px; border: none; border-radius: 7px;
    background: transparent; color: var(--text-secondary); font: inherit; font-size: 13px; cursor: pointer;
  }
  .tabs button:hover { background: var(--hover-neutral); color: var(--text); }
  .tabs button.on { background: var(--active-blue-bg); color: var(--blue-6); font-weight: 600; }
  .rules, .dictionary { padding-top: 18px; }
  h2 { margin: 0; color: var(--text-heading); font-size: 14px; }
  .intro, .scope, .note { margin: 8px 0 0; color: var(--text-secondary); font-size: 13px; line-height: 1.6; }
  .scope { color: var(--text-tertiary); }
  .note { padding: 18px 0; }
  .error { color: var(--error); }
  .rule-meta { display: flex; flex-direction: column; gap: 4px; margin-top: 18px; font-size: 12px; color: var(--text-tertiary); }
  .rule-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; list-style: none; margin: 14px 0 0; padding: 0; }
  .rule-list li { padding: 9px 10px; border: 1px solid var(--border-secondary); border-radius: 8px; color: var(--text); font-size: 13px; }
  @media (max-width: 600px) { .rule-list { grid-template-columns: 1fr; } }
</style>
