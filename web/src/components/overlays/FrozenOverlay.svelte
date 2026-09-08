<script lang="ts">
  import { frozen } from '../../lib/stores'
  import { t } from '../../lib/i18n'
  import * as api from '../../lib/api'

  // One-shot guard: once the quit request is in flight, further clicks must not
  // fire duplicate requests (the process is exiting; a second call is noise).
  let quitting = $state(false)

  function quit() {
    if (quitting) return
    quitting = true
    // Best-effort. On failure (native bridge gone) stay frozen so the user can
    // still reach the OS-level quit (tray / task manager) — no silent dead-end.
    api.nativeQuit().catch(() => { quitting = false })
  }
</script>

{#if $frozen}
<div class="frozen-backdrop" role="alertdialog" aria-modal="true" aria-label={$t('frozen.title')}>
  <div class="frozen-card">
    <iconify-icon icon="ant-design:disconnect-outlined" width="28" style="color:var(--error);flex-shrink:0"></iconify-icon>
    <h2 class="frozen-title">{$t('frozen.title')}</h2>
    <p class="frozen-desc">{$t('frozen.desc')}</p>
    <button class="frozen-quit" onclick={quit} disabled={quitting}>{$t('frozen.quit')}</button>
  </div>
</div>
{/if}

<style>
/* A full-screen overlay above every other surface (modals, toasts, command
   palette) so a frozen product truly blocks all input — the user must either
   reconnect the data store or quit. Highest z-index in the app on purpose. */
.frozen-backdrop {
  position: fixed; inset: 0; z-index: 3000;
  background: var(--scrim);
  display: flex; align-items: center; justify-content: center;
  padding: 24px;
}
.frozen-card {
  width: 100%; max-width: 440px;
  background: var(--bg-container);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 24px;
  display: flex; flex-direction: column; align-items: center; gap: 12px;
  text-align: center;
  box-shadow: 0 16px 48px rgba(0,0,0,0.18);
  animation: octo-fadein 0.16s ease;
}
.frozen-title {
  margin: 0;
  font-size: 15px; font-weight: 600; color: var(--text-heading);
}
.frozen-desc {
  margin: 0;
  font-size: 13px; line-height: 1.6; color: var(--text-secondary);
}
.frozen-quit {
  margin-top: 4px;
  height: 32px; padding: 0 20px;
  border: none; background: var(--error);
  border-radius: 6px;
  font-size: 13px; color: #fff;
  cursor: pointer; font-family: inherit;
}
.frozen-quit:hover:not(:disabled) { filter: brightness(1.08); }
.frozen-quit:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
