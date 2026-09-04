<script>
  import { onboardPhase } from '../../../web/src/lib/stores'
  import OctoLogo from '../../../web/src/components/layout/OctoLogo.svelte'
  import Segment from '../../../web/src/components/ui/Segment.svelte'
  import { countryCodes } from './data/country-codes'
  import {
    PORTABLE_ACTIVATION_CODE,
    PORTABLE_PASSWORD,
    PORTABLE_VERIFICATION_CODE,
    authPrompt,
    submitAuthKey,
  } from './portable-auth'
  import {
    initializePortableI18n,
    portableLanguages,
    portableLocale,
    portableT,
    setPortableLanguage,
  } from './portable-i18n'
  const PROFILE_STORAGE_KEY = 'buding_box_portable_activation_profile'
  const authModes = ['activation', 'login']
  /** @type {any} 兼容旧版编辑器，运行时仍复用上游组件。 */
  const Logo = OctoLogo
  /** @type {any} 兼容旧版编辑器，运行时仍复用上游组件。 */
  const ModeSegment = Segment

  initializePortableI18n()

  let showAuth = false
  let mode = 'activation'
  let activationCode = ''
  let nickname = ''
  let dialCode = '+86'
  let phone = ''
  let loginDialCode = '+86'
  let loginPhone = ''
  let verificationCode = ''
  let submitting = false
  let errorKey = null
  let firstInputEl = null
  $: modeLabels = {
    activation: $portableT('auth.activation.tab'),
    login: $portableT('auth.login.tab'),
  }

  $: if (!$authPrompt && submitting && $onboardPhase !== 'unknown') submitting = false

  function beginAuth() {
    showAuth = true
    mode = 'activation'
    errorKey = null
    requestAnimationFrame(() => firstInputEl?.focus())
  }

  function switchMode(next) {
    if (next !== 'activation' && next !== 'login') return
    mode = next
    errorKey = null
    submitting = false
    requestAnimationFrame(() => firstInputEl?.focus())
  }

  function changeLanguage(next) {
    setPortableLanguage(next)
  }

  function handleLanguageChange(event) {
    const target = event.currentTarget
    if (!(target instanceof HTMLSelectElement)) return
    if (target.value === 'en' || target.value === 'zh') changeLanguage(target.value)
  }

  async function completeAuth() {
    submitting = true
    await submitAuthKey(PORTABLE_PASSWORD, $portableLocale)
  }

  async function activate() {
    if (submitting) return
    if (activationCode.trim() !== PORTABLE_ACTIVATION_CODE) {
      errorKey = 'auth.activation.invalidCode'
      return
    }
    if (!nickname.trim() || nickname.trim().length > 32) {
      errorKey = 'auth.activation.invalidNickname'
      return
    }
    const phoneDigits = phone.replace(/\D/g, '')
    if (phoneDigits.length < 6 || phoneDigits.length > 15) {
      errorKey = 'auth.activation.invalidPhone'
      return
    }

    localStorage.setItem(PROFILE_STORAGE_KEY, JSON.stringify({
      nickname: nickname.trim(),
      phone: `${dialCode}${phoneDigits}`,
      activatedAt: new Date().toISOString(),
    }))
    errorKey = null
    await completeAuth()
  }

  async function login() {
    if (submitting) return
    const phoneDigits = loginPhone.replace(/\D/g, '')
    if (phoneDigits.length < 6 || phoneDigits.length > 15) {
      errorKey = 'auth.login.invalidPhone'
      return
    }
    if (verificationCode.trim() !== PORTABLE_VERIFICATION_CODE) {
      errorKey = 'auth.login.invalidCode'
      return
    }
    errorKey = null
    await completeAuth()
  }
</script>

{#if $authPrompt || submitting}
  <main class="auth-page" aria-labelledby="portable-auth-title">
    <section class="auth-content" class:welcome={!showAuth}>
      <div class="mascot" aria-hidden="true">
        <span class="orbit orbit-one"></span>
        <span class="orbit orbit-two"></span>
        <div class="brand-mark"><Logo size={72} /></div>
      </div>

      <div class="brand-copy">
        <h1 id="portable-auth-title">{$portableT('auth.brand.title')}</h1>
        <p>{$portableT('auth.brand.subtitle')}</p>
      </div>

      {#if showAuth}
        <div class="auth-panel">
          {#if mode === 'activation'}
          <div class="panel-heading">
            <h2>{$portableT('auth.activation.title')}</h2>
            <p>{$portableT('auth.activation.subtitle')}</p>
          </div>
          <!-- svelte-ignore event_directive_deprecated -->
          <form class="auth-form" on:submit|preventDefault={activate}>
            <label class="field" for="portable-activation-code">
              <span>{$portableT('auth.activation.code')}</span>
              <input
                id="portable-activation-code"
                bind:this={firstInputEl}
                bind:value={activationCode}
                type="text"
                autocomplete="off"
                placeholder={$portableT('auth.activation.codePlaceholder')}
                disabled={submitting}
                aria-invalid={errorKey === 'auth.activation.invalidCode'}
              />
            </label>

            <label class="field" for="portable-nickname">
              <span>{$portableT('auth.activation.nickname')}</span>
              <input
                id="portable-nickname"
                bind:value={nickname}
                type="text"
                maxlength="32"
                autocomplete="nickname"
                placeholder={$portableT('auth.activation.nicknamePlaceholder')}
                disabled={submitting}
                aria-invalid={errorKey === 'auth.activation.invalidNickname'}
              />
            </label>

            <fieldset class="field phone-field">
              <legend>{$portableT('auth.activation.phone')}</legend>
              <div class="phone-control">
                <select bind:value={dialCode} disabled={submitting} aria-label={$portableT('auth.activation.phone')}>
                  {#each countryCodes as country}
                    <option value={country.dialCode}>{country.dialCode}</option>
                  {/each}
                </select>
                <input
                  bind:value={phone}
                  type="tel"
                  inputmode="tel"
                  maxlength="18"
                  autocomplete="tel-national"
                  placeholder={$portableT('auth.activation.phonePlaceholder')}
                  disabled={submitting}
                  aria-invalid={errorKey === 'auth.activation.invalidPhone'}
                />
              </div>
            </fieldset>

            {#if errorKey}
              <p class="error" role="alert">{$portableT(errorKey)}</p>
            {/if}
            <p class="help">{$portableT('auth.activation.hint')}</p>
            <button class="primary" type="submit" disabled={submitting}>
              {submitting ? $portableT('auth.activation.submitting') : $portableT('auth.activation.submit')}
            </button>
          </form>
          {:else}
          <div class="panel-heading">
            <h2>{$portableT('auth.login.title')}</h2>
            <p>{$portableT('auth.login.subtitle')}</p>
          </div>
          <!-- svelte-ignore event_directive_deprecated -->
          <form class="auth-form" on:submit|preventDefault={login}>
            <fieldset class="field phone-field">
              <legend>{$portableT('auth.activation.phone')}</legend>
              <div class="phone-control">
                <select
                  bind:value={loginDialCode}
                  disabled={submitting}
                  aria-label={$portableT('auth.activation.phone')}
                >
                  {#each countryCodes as country}
                    <option value={country.dialCode}>{country.dialCode}</option>
                  {/each}
                </select>
                <input
                  bind:this={firstInputEl}
                  bind:value={loginPhone}
                  type="tel"
                  inputmode="tel"
                  maxlength="18"
                  autocomplete="tel-national"
                  placeholder={$portableT('auth.activation.phonePlaceholder')}
                  disabled={submitting}
                  aria-invalid={errorKey === 'auth.login.invalidPhone'}
                />
              </div>
            </fieldset>

            <label class="field" for="portable-verification-code">
              <span>{$portableT('auth.login.code')}</span>
              <input
                id="portable-verification-code"
                bind:value={verificationCode}
                type="text"
                inputmode="numeric"
                maxlength="6"
                autocomplete="one-time-code"
                placeholder={$portableT('auth.login.codePlaceholder')}
                disabled={submitting}
                aria-invalid={errorKey === 'auth.login.invalidCode'}
              />
            </label>
            {#if errorKey}
              <p class="error" role="alert">{$portableT(errorKey)}</p>
            {/if}
            <p class="help">{$portableT('auth.login.hint')}</p>
            <button class="primary" type="submit" disabled={submitting}>
              {submitting ? $portableT('auth.login.submitting') : $portableT('auth.login.submit')}
            </button>
          </form>
          {/if}

          <div class="mode-switch" aria-label={$portableT('auth.modeLabel')}>
            <ModeSegment
              options={authModes}
              value={mode}
              labels={modeLabels}
              onchange={switchMode}
            />
          </div>
        </div>
      {:else}
        <!-- svelte-ignore event_directive_deprecated -->
        <button class="primary welcome-login" type="button" on:click={beginAuth}>
          {$portableT('auth.welcome.login')}
        </button>
      {/if}
    </section>

    <footer aria-label={$portableT('common.legal')}>
      <div class="legal-links">
        <span>{$portableT('common.privacy')}</span>
        <span>{$portableT('common.terms')}</span>
      </div>
      <label class="language-switcher" for="portable-language">
        <span>{$portableT('common.language')}</span>
        <select
          id="portable-language"
          value={$portableLocale}
          on:change={handleLanguageChange}
        >
          {#each portableLanguages as option}
            <option value={option.value}>{option.label}</option>
          {/each}
        </select>
      </label>
    </footer>
  </main>
{/if}

<style>
  .auth-page {
    position: fixed;
    inset: 0;
    z-index: 1200;
    min-height: 100%;
    display: grid;
    grid-template-rows: 1fr auto;
    overflow-y: auto;
    background: var(--bg-container, #fff);
    color: var(--text-heading, #18181b);
  }

  .auth-content {
    width: min(440px, calc(100vw - 40px));
    margin: auto;
    padding: 36px 0 28px;
    display: flex;
    flex-direction: column;
    align-items: center;
  }

  .auth-content.welcome { padding: 64px 0 36px; }

  .welcome .mascot {
    width: 172px;
    height: 172px;
    margin-bottom: 28px;
  }

  .welcome .brand-mark { transform: scale(1.3); }
  .welcome .orbit-one { width: 146px; height: 146px; }
  .welcome .orbit-two { width: 172px; height: 92px; }

  .mascot {
    position: relative;
    width: 124px;
    height: 124px;
    display: grid;
    place-items: center;
    margin-bottom: 18px;
  }

  .brand-mark {
    position: relative;
    z-index: 2;
    color: var(--text-heading, #18181b);
    filter: grayscale(1);
  }

  .orbit {
    position: absolute;
    border: 1px solid var(--border, rgba(0, 0, 0, 0.08));
    border-radius: 50%;
  }

  .orbit-one { width: 108px; height: 108px; }
  .orbit-two { width: 124px; height: 68px; transform: rotate(-24deg); }
  .brand-copy, .panel-heading { text-align: center; }

  h1 {
    margin: 0;
    font-size: clamp(28px, 4vw, 38px);
    line-height: 1.18;
    letter-spacing: -0.035em;
    font-weight: 720;
  }

  .brand-copy > p, .panel-heading > p {
    margin: 8px 0 0;
    color: var(--text-secondary, #52525b);
    font-size: 14px;
    line-height: 1.55;
  }

  .auth-panel {
    width: min(380px, 100%);
    margin-top: 24px;
  }

  .panel-heading h2 {
    margin: 0;
    font-size: 20px;
    line-height: 1.35;
  }

  .auth-form {
    margin-top: 18px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .field {
    min-width: 0;
    margin: 0;
    padding: 0;
    border: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .field > span, .field > legend {
    padding: 0;
    color: var(--text-heading, #18181b);
    font-size: 13px;
    font-weight: 620;
  }

  input, select {
    width: 100%;
    min-width: 0;
    height: 46px;
    padding: 0 13px;
    border: 1px solid var(--border, rgba(0, 0, 0, 0.14));
    border-radius: 10px;
    background: var(--bg-layout, #fafafa);
    color: var(--text, #18181b);
    font: inherit;
    font-size: 15px;
  }

  input:focus, select:focus {
    outline: none;
    border-color: var(--blue-6, #2563eb);
    box-shadow: 0 0 0 3px var(--focus-ring, rgba(37, 99, 235, 0.2));
  }

  input[aria-invalid='true'] { border-color: var(--error, #dc2626); }
  input:disabled, select:disabled { opacity: 0.58; cursor: not-allowed; }

  .phone-control {
    display: grid;
    grid-template-columns: minmax(132px, 0.9fr) minmax(150px, 1.4fr);
    gap: 8px;
  }

  .phone-control select {
    padding: 0 46px 0 14px;
    appearance: none;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='14' height='14' viewBox='0 0 24 24' fill='none' stroke='%2371717a' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right 18px center;
    cursor: pointer;
  }

  .help, .error {
    margin: 0;
    font-size: 12px;
    line-height: 1.55;
  }

  .help { color: var(--text-secondary, #52525b); }
  .error { color: var(--error, #dc2626); }

  .primary {
    width: 100%;
    min-height: 50px;
    margin-top: 2px;
    border: 0;
    border-radius: 999px;
    background: var(--text-heading, #18181b);
    color: var(--bg-container, #fff);
    font: inherit;
    font-size: 15px;
    font-weight: 650;
    cursor: pointer;
    transition: opacity 180ms ease, background-color 180ms ease;
  }

  .primary:hover:not(:disabled) { opacity: 0.82; }
  .primary:disabled { opacity: 0.42; cursor: not-allowed; }
  .primary:focus-visible {
    outline: 3px solid var(--focus-ring, rgba(37, 99, 235, 0.3));
    outline-offset: 3px;
  }

  .welcome-login {
    width: min(288px, 100%);
    margin-top: 38px;
  }

  .mode-switch {
    margin-top: 18px;
    padding-top: 16px;
    border-top: 1px solid var(--border, rgba(0, 0, 0, 0.08));
    display: flex;
    justify-content: center;
  }

  .mode-switch :global(.opt) { min-height: 36px; }

  footer {
    position: relative;
    min-height: 76px;
    padding: 16px 20px;
    display: flex;
    justify-content: center;
    align-items: center;
    color: var(--text-secondary, #52525b);
    font-size: 13px;
  }

  .legal-links { display: flex; gap: 28px; }

  .language-switcher {
    position: absolute;
    right: 20px;
    bottom: 16px;
    display: flex;
    align-items: center;
    gap: 12px;
    white-space: nowrap;
  }

  .language-switcher > span { flex: 0 0 auto; }

  .language-switcher select {
    width: auto;
    min-width: 124px;
    min-height: 44px;
    padding: 0 38px 0 14px;
    border: 1px solid var(--border, rgba(0, 0, 0, 0.12));
    border-radius: 12px;
    appearance: none;
    background-color: var(--bg-container, #fff);
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='14' height='14' viewBox='0 0 24 24' fill='none' stroke='%2371717a' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right 14px center;
    color: var(--text-heading, #18181b);
    font: inherit;
    cursor: pointer;
  }

  @media (max-width: 680px) {
    .auth-content { padding-top: 24px; }
    .auth-content.welcome { padding-top: 36px; }
    .phone-control { grid-template-columns: 1fr; }
    footer { min-height: 126px; padding-bottom: 72px; }
    .language-switcher { right: 16px; bottom: 12px; }
  }

  @media (max-height: 760px) {
    .mascot { width: 88px; height: 88px; margin-bottom: 10px; }
    .brand-mark { transform: scale(0.72); }
    .orbit-one { width: 78px; height: 78px; }
    .orbit-two { width: 88px; height: 50px; }
    .auth-content { padding-top: 20px; }
    .auth-panel { margin-top: 18px; }
    .auth-content.welcome { padding-top: 32px; }
    .welcome .mascot { width: 132px; height: 132px; margin-bottom: 18px; }
    .welcome .brand-mark { transform: scale(1); }
    .welcome .orbit-one { width: 116px; height: 116px; }
    .welcome .orbit-two { width: 136px; height: 74px; }
    .welcome-login { margin-top: 26px; }
  }

  @media (prefers-reduced-motion: reduce) {
    .primary { transition: none; }
  }
</style>
