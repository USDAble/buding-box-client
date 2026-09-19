<script lang="ts">
  import { t, locale, setLocale } from '../../../lib/i18n'
  import { productState, updateNickname, updatePrefs, ProductError } from '../../../lib/product'
  import { showToast, settingsModalOpen } from '../../../lib/stores'
  import { validateNickname } from '../../../lib/nickname'
  import { avatarInitial, avatarColor } from '../../../lib/avatar'

  // Settings secondary page (P5). Not a second settings modal: account info
  // (nickname / masked phone) and language live here; everything else routes
  // to the full settings modal via the button at the bottom. Autostart has no row
  // *here*: the full settings
  // modal owns it, gated on the native shell alone, so a portable build does
  // still render that switch — 需求 §5.1.2 第 9 条 requires it hidden there
  // (V-85). Startup rules: P2-启动与生命周期.md §5. This comment used to claim an
  // AutostartAvailable() helper made it impossible; no such function exists.

  const nickname = $derived($productState?.account?.nickname ?? '')
  const currentLocale = $derived($productState?.prefs?.locale || $locale)

  // Page mounts fresh per open (AccountPanel swaps pages in place), so draft
  // starts from the store each time the user enters Settings. Read the store
  // directly rather than the $derived nickname — the seed must be a one-time
  // snapshot at mount, and referencing a $derived here trips Svelte's
  // state_referenced_locally warning.
  let draft = $state($productState?.account?.nickname ?? '')
  let nicknameErr = $state<'' | 'nickname_format' | 'nickname_sensitive'>('')
  let savingNickname = $state(false)

  function onDraft() {
    if (nicknameErr) nicknameErr = ''
  }

  async function saveNickname() {
    const name = draft.trim()
    if (validateNickname(name) !== 'ok') { nicknameErr = 'nickname_format'; return }
    savingNickname = true
    nicknameErr = ''
    try {
      await updateNickname(name)
      draft = name
      showToast($t('product.panel.nickname_saved'))
    } catch (e) {
      if (e instanceof ProductError && e.code) {
        nicknameErr = e.code === 'nickname_sensitive' ? 'nickname_sensitive' : 'nickname_format'
      } else {
        showToast($t('product.send_failed'), 'error')
      }
    } finally {
      savingNickname = false
    }
  }

  function errKey(): string {
    return nicknameErr === 'nickname_sensitive' ? 'product.err_nickname_sensitive' : 'product.err_nickname'
  }

  async function pickLang(l: 'zh' | 'en') {
    if (l === currentLocale) return
    setLocale(l)
    try {
      await updatePrefs({ locale: l })
    } catch {
      // The UI language already flipped locally; a failed persist just means
      // the next boot forgets it. Surface the failure rather than pretending.
      showToast($t('product.send_failed'), 'error')
    }
  }

  function openFullSettings() {
    // Leave the panel open underneath: the full-settings modal is a true modal
    // (its own .backdrop + Esc), so closing it lands the user back on this
    // page rather than dropping them to the main UI (P5 §4.3).
    settingsModalOpen.set(true)
  }

</script>

<div class="settings">
  <section class="block">
    <h3 class="block-title">{$t('product.panel.account_info')}</h3>
    <div class="acct-row">
      <span class="avatar" style="background:{avatarColor(nickname)}">{avatarInitial(nickname)}</span>
      <div class="acct-meta">
        <div class="nick-row">
          <label class="mini" for="nickname-input">{$t('product.panel.nickname')}</label>
          <div class="nick-input-row">
            <input
              id="nickname-input"
              bind:value={draft}
              oninput={onDraft}
              maxlength="16"
              spellcheck="false"
            />
            <button class="save" onclick={saveNickname} disabled={savingNickname || draft.trim() === nickname}>
              {savingNickname ? $t('common.saving') : $t('common.save')}
            </button>
          </div>
        </div>
        {#if nicknameErr}
          <p class="field-err">{$t(errKey())}</p>
        {/if}
      </div>
    </div>
  </section>

  <section class="block">
    <h3 class="block-title">{$t('product.panel.language')}</h3>
    <div class="seg">
      <button class:on={currentLocale === 'zh'} onclick={() => pickLang('zh')}>中文</button>
      <button class:on={currentLocale === 'en'} onclick={() => pickLang('en')}>EN</button>
    </div>
  </section>

  <button class="full-settings" onclick={openFullSettings}>
    <iconify-icon icon="ant-design:setting-outlined" width="14"></iconify-icon>
    {$t('product.panel.full_settings')}
  </button>
</div>

<style>
  .settings { display: flex; flex-direction: column; gap: 18px; padding: 2px 2px 14px; }
  .block { display: flex; flex-direction: column; gap: 10px; }
  .block-title { margin: 0; font-size: 12px; font-weight: 600; color: var(--text-tertiary); text-transform: uppercase; letter-spacing: 0.4px; }
  .acct-row { display: flex; gap: 12px; align-items: flex-start; }
  .avatar {
    width: 40px; height: 40px; border-radius: 50%; flex: 0 0 auto;
    display: flex; align-items: center; justify-content: center;
    color: #fff; font-size: 16px; font-weight: 600;
  }
  .acct-meta { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 8px; }
  .nick-row { display: flex; flex-direction: column; gap: 4px; }
  .mini { font-size: 12px; color: var(--text-secondary); }
  .nick-input-row { display: flex; gap: 6px; }
  .nick-input-row input {
    flex: 1; min-width: 0; height: 30px; padding: 0 10px;
    border: 1px solid var(--border); border-radius: 6px;
    font-size: 13px; font-family: inherit; color: var(--text);
    background: var(--bg-container); outline: none;
  }
  .nick-input-row input:focus { border-color: var(--blue-6); box-shadow: 0 0 0 2px var(--active-blue-bg); }
  .save {
    flex: none; height: 30px; padding: 0 12px;
    border: none; border-radius: 6px; background: var(--blue-6); color: #fff;
    font-size: 12px; font-weight: 500; cursor: pointer; font-family: inherit;
  }
  .save:hover:not(:disabled) { background: var(--blue-5); }
  .save:disabled { opacity: 0.5; cursor: not-allowed; }
  .field-err { margin: 0; font-size: 12px; color: var(--error); }
  .seg { display: inline-flex; border: 1px solid var(--border); border-radius: 8px; overflow: hidden; width: fit-content; }
  .seg button {
    border: none; background: transparent; font-family: inherit;
    font-size: 13px; color: var(--text-secondary); padding: 6px 16px; cursor: pointer;
  }
  .seg button.on { background: var(--active-blue-bg); color: var(--blue-6); font-weight: 600; }
  .full-settings {
    display: flex; align-items: center; justify-content: center; gap: 6px;
    height: 34px; border: 1px solid var(--border); border-radius: 8px;
    background: transparent; color: var(--text-secondary); font-family: inherit;
    font-size: 13px; cursor: pointer;
  }
  .full-settings:hover { background: var(--hover-neutral); color: var(--text); }
</style>
