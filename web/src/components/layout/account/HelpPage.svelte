<script lang="ts">
  import { t } from '../../../lib/i18n'

  // Help & feedback placeholder (P5). A demo-friendly cheat sheet per 需求
  // §9.10: the fixed test credentials and the operational notes a tester
  // needs (change phone, unplug order, drive loss). No real support channel
  // exists this phase, so there is no external link and no network call.
  const items = [
    'product.panel.help.test_code',
    'product.panel.help.test_activation',
    'product.panel.help.demo_points',
    'product.panel.help.unplug_first',
    'product.panel.help.change_phone',
    'product.panel.help.drive_loss',
    'product.panel.help.smartscreen',
  ]
</script>

<div class="help">
  <ul class="list">
    {#each items as key}
      <li>{$t(key)}</li>
    {/each}
  </ul>
</div>

<style>
  .help { padding: 2px 2px 14px; }
  .list { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 4px; }
  .list li {
    padding: 8px 2px; font-size: 13px; color: var(--text);
    line-height: 1.6; border-bottom: 1px solid var(--border-secondary);
  }
  .list li:last-child { border-bottom: none; }
</style>
