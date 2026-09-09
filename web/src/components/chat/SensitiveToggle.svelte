<script lang="ts">
  // The input sensitive-word check toggle (P8 §5.5.1). Sits under the input
  // box; turning it OFF requires a confirm, turning it back ON does not. Only
  // affects input — model output is always filtered regardless of this switch.
  import { t, tr } from '../../lib/i18n'
  import { productState, updatePrefs } from '../../lib/product'
  import { confirmDialog } from '../../lib/confirm'
  import { showToast } from '../../lib/stores'

  const enabled = $derived($productState?.prefs.inputSensitiveCheck ?? true)

  async function toggle() {
    if (enabled) {
      const ok = await confirmDialog(tr('sensitive.toggle_off_confirm'))
      if (!ok) return
    }
    try {
      await updatePrefs({ inputSensitiveCheck: !enabled })
    } catch {
      showToast(tr('product.send_failed'), 'error')
    }
  }
</script>

<div class="sensitive-toggle">
  <button class="row" onclick={toggle} role="switch" aria-checked={enabled}>
    <span class="lbl">{$t('sensitive.toggle')}</span>
    <span class="toggle" class:on={enabled}>
      <span class="toggle-knob"></span>
    </span>
  </button>
</div>

<style>
  .sensitive-toggle {
    display: flex;
    justify-content: flex-end;
    padding: 0 8px 4px;
  }
  .row {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    border: none;
    background: transparent;
    padding: 2px 4px;
    border-radius: 6px;
    cursor: pointer;
    font-family: inherit;
  }
  .row:hover { background: var(--hover-neutral); }
  .lbl { font-size: 12px; color: var(--text-secondary); }
  .toggle {
    width: 30px;
    height: 16px;
    border-radius: 9999px;
    background: var(--border);
    position: relative;
    transition: background 0.15s ease;
  }
  .toggle.on { background: var(--success); }
  .toggle-knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: #fff;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.15);
    transition: transform 0.15s ease;
  }
  .toggle.on .toggle-knob { transform: translateX(14px); }
</style>
