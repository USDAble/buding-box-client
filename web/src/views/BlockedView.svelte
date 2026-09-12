<script lang="ts">
  import { onMount } from 'svelte'
  import { t, locale, setLocale } from '../lib/i18n'
  import { productState, blockedPage, sendCode, login, setProductLocale, ProductError, failureTier, tierRetryable } from '../lib/product'
  import { normalizePhone } from '../lib/phone'
  import { randomNickname, validateNickname } from '../lib/nickname'
  import { brandName, brandTagline, brandTermsTitle, brandPrivacyTitle, brandText } from '../lib/brand'
  import { showToast } from '../lib/stores'
  import BrandMark from '../components/BrandMark.svelte'
  import LegalModal from '../components/overlays/LegalModal.svelte'

  // The product gate's login page (P4). Two shapes share this one form: first
  // activation (with an activation-code field) and second login (nickname
  // prefilled). All validation is two-round per 需求 §5.3.3 — every format error
  // shows at once, then the business checks short-circuit server-side.

  let phone = $state('')
  let code = $state('')
  let nickname = $state('')
  let activationCode = $state('')
  let boxCode = $state('')
  let fieldErrors = $state<Record<string, string>>({})
  let formError = $state<string | null>(null)
  let phoneMasked = $state<string | null>(null)
  let sending = $state(false)
  let submitting = $state(false)
  let countdown = $state(0)
  let nicknameEdited = $state(false)
  let legalModal = $state<null | 'terms' | 'privacy'>(null)
  let timer: ReturnType<typeof setInterval> | null = null

  // First activation vs second login comes from the server's activation flag,
  // never guessed client-side (需求 §5.3.4).
  const activated = $derived($productState?.activated ?? false)

  // L-B2: the two misconfiguration pages. They are not variants of the login
  // form - on a build with no control plane there is no request to make, so the
  // form is not rendered at all rather than rendered dead (P4-拦截页 §2.1).
  const misconfigured = $derived($blockedPage !== 'login')

  // L-B3: which control-plane failure tier the banner shows. `unauthorized` is
  // deliberately absent: that tier is L-A6's path - the credential is already
  // gone and returning to the login form IS the recovery - so a banner on top of
  // it would narrate a state the user has already left (P4-拦截页 §3.4).
  const tier = $derived(failureTier(formError))
  const tierNotice = $derived(tier === 'unauthorized' ? null : tier)

  // Which action the retry button repeats. Without it, "retry" after a failed
  // send-code would submit the whole form, i.e. do something the user did not
  // ask for and which fails differently.
  let lastFailed = $state<'login' | 'sendCode' | null>(null)

  onMount(() => {
    // Open in the persisted (or system-derived) language; the state handler
    // already filled prefs.locale with the system language when unset.
    const loc = $productState?.prefs?.locale
    if (loc === 'zh' || loc === 'en') setLocale(loc)
    // Second login prefills the last nickname; first activation seeds a fresh
    // random default in the current UI language (需求 §5.3.3 / §5.3.4).
    nickname = $productState?.account?.nickname || randomNickname($locale === 'zh' ? 'zh' : 'en')
    return () => { if (timer) clearInterval(timer) }
  })

  function pickLang(l: 'zh' | 'en') {
    setLocale(l)
    setProductLocale(l).catch(() => {})
    // Regenerate the default nickname only when there is no bound account and
    // the user hasn't edited it — a language switch must not wipe a typed name
    // (需求 §5.3.3) nor replace a prefilled last nickname (§5.3.4).
    if (!nicknameEdited && !$productState?.account?.nickname) nickname = randomNickname(l)
  }

  function startCountdown(secs: number) {
    countdown = secs
    if (timer) clearInterval(timer)
    timer = setInterval(() => {
      countdown -= 1
      if (countdown <= 0 && timer) { clearInterval(timer); timer = null }
    }, 1000)
  }

  async function onSendCode() {
    if (!normalizePhone(phone).ok) {
      fieldErrors = { ...fieldErrors, phone: 'invalid_phone' }
      return
    }
    fieldErrors = { ...fieldErrors, phone: '' }
    sending = true
    try {
      const secs = await sendCode(phone)
      startCountdown(secs)
      showToast($t('product.code_sent'))
    } catch (e) {
      if (e instanceof ProductError && e.retryAfterSec != null) {
        startCountdown(e.retryAfterSec)
      } else if (e instanceof ProductError && failureTier(e.code)) {
        // A control-plane tier is not a phone problem. Filing it under the field
        // showed an empty message under a number that was never wrong, because
        // the field-error switch has no case for it (L-B3).
        formError = e.code
        lastFailed = 'sendCode'
      } else if (e instanceof ProductError && e.fieldErrors.phone) {
        fieldErrors = { ...fieldErrors, phone: e.fieldErrors.phone }
      } else {
        showToast($t('product.send_failed'), 'error')
      }
    } finally {
      sending = false
    }
  }

  /** Repeats the action that just failed, not a generic resubmit (L-B3). */
  async function onRetry() {
    formError = null
    if (lastFailed === 'sendCode') await onSendCode()
    else await doSubmit()
  }

  async function onSubmit(e: SubmitEvent) {
    e.preventDefault()
    await doSubmit()
  }

  async function doSubmit() {
    // Round one — format. Every failure is collected and shown at once.
    const errs: Record<string, string> = {}
    if (!normalizePhone(phone).ok) errs.phone = 'invalid_phone'
    if (!/^\d{6}$/.test(code)) errs.code = 'invalid_code'
    if (validateNickname(nickname) !== 'ok') errs.nickname = 'nickname_format'
    if (!activated && !activationCode.trim()) errs.activationCode = 'invalid_activation'
    // Box code: non-empty only. Its length/charset are the server's call
    // (需求基线 E1 rule 4) — the client must not pre-judge validity.
    if (!activated && !boxCode.trim()) errs.boxCode = 'invalid_box_code'
    if (Object.keys(errs).length > 0) { fieldErrors = errs; return }
    fieldErrors = {}
    formError = null
    phoneMasked = null
    submitting = true
    try {
      await login({
        phone,
        code,
        nickname,
        activationCode: activated ? undefined : activationCode.trim(),
        boxCode: activated ? undefined : boxCode.trim(),
      })
      // On success login() flips productPhase to 'ready', so App.svelte boots
      // the main UI and this view unmounts.
    } catch (e) {
      if (e instanceof ProductError) {
        if (Object.keys(e.fieldErrors).length > 0) fieldErrors = e.fieldErrors
        else {
          formError = e.code
          phoneMasked = e.phoneMasked
          // Only a retryable tier is worth re-running; for the others the
          // button is not rendered at all.
          lastFailed = failureTier(e.code) ? 'login' : null
        }
      } else {
        formError = 'generic'
      }
    } finally {
      submitting = false
    }
  }

  // Map a field error machine code to its i18n key (rendered via $t in the
  // template so a language switch re-renders the message).
  function fieldErrorKey(field: string): string {
    switch (fieldErrors[field]) {
      case 'invalid_phone': return 'product.err_phone'
      case 'invalid_code': return 'product.err_code'
      case 'nickname_format': return 'product.err_nickname'
      case 'nickname_sensitive': return 'product.err_nickname_sensitive'
      case 'invalid_activation': return 'product.err_activation'
      case 'invalid_box_code': return 'product.err_box_code'
      default: return ''
    }
  }

  // Machine code -> copy for the three tiers that get a banner. `unauthorized`
  // has no case on purpose: it renders nothing (see tierNotice above).
  function tierNoticeKey(t: 'network_unavailable' | 'upstream_unavailable' | 'account_restricted'): string {
    switch (t) {
      case 'network_unavailable': return 'product.tier.network_unavailable'
      case 'upstream_unavailable': return 'product.tier.upstream_unavailable'
      case 'account_restricted': return 'product.tier.account_restricted'
    }
  }

  function businessErrorKey(): string {
    switch (formError) {
      case 'code_not_sent': return 'product.err_code_not_sent'
      case 'invalid_code': return 'product.err_invalid_code'
      // The activation family. Each code renders its own copy: the whole point
      // is that a user who mistyped one character can tell which of the two
      // credentials failed (需求基线 E1 rule 2 / PQ9).
      case 'activation_invalid': return 'product.err_activation_invalid'
      case 'activation_code_used': return 'product.err_activation_used'
      case 'box_code_unknown': return 'product.err_box_code_unknown'
      case 'box_code_mismatch': return 'product.err_box_code_mismatch'
      case 'phone_mismatch': return 'product.err_phone_mismatch'
      case 'generic': return 'product.submit_failed'
      default: return ''
    }
  }
</script>

<div class="blocked">
  <div class="lang-switch">
    <button class:on={$locale === 'zh'} onclick={() => pickLang('zh')}>中文</button>
    <span class="sep">|</span>
    <button class:on={$locale === 'en'} onclick={() => pickLang('en')}>EN</button>
  </div>

  <div class="card">
    <div class="brand">
      <BrandMark size={56} />
      <h1 class="brand-name">{brandName($locale)}</h1>
      {#if !misconfigured}<p class="tagline">{brandTagline($locale)}</p>{/if}
    </div>

    {#if misconfigured}
      <!-- L-B2. No form, no host, no file name: neither page offers an action
           the user could take on this machine, so the copy's job is to name
           which package is wrong and who to ask (P4-拦截页 §2.1). -->
      <h2 class="notice-title">{$t($blockedPage === 'no_keys' ? 'product.blocked.no_keys_title' : 'product.blocked.unconfigured_title')}</h2>
      <p class="notice-body">{$t($blockedPage === 'no_keys' ? 'product.blocked.no_keys_body' : 'product.blocked.unconfigured_body')}</p>
    {:else}
      {#if tierNotice}
        <!-- L-B3. The tier's own copy, and a retry only where retrying can
             actually succeed - an outage heals, a refused session does not. -->
        <div class="form-err tier">
          <p class="tier-msg">{$t(tierNoticeKey(tierNotice))}</p>
          {#if tierRetryable(tierNotice)}
            <button type="button" class="retry-btn" data-testid="tier-retry" onclick={onRetry}>
              {$t('product.tier.retry')}
            </button>
          {:else}
            <!-- P4-拦截页 §3.3: every tier must end in something the user can
                 actually do. This one is not retryable and must not clear the
                 credential, and the customer-service channel is still undecided
                 (V-4 / TODO-06) - so the action is to dismiss the banner, and
                 the copy never names a channel that does not exist. -->
            <button type="button" class="retry-btn" data-testid="tier-dismiss" onclick={() => (formError = null)}>
              {$t('product.tier.dismiss')}
            </button>
          {/if}
        </div>
      {:else if formError}
        <div class="form-err">
          {$t(businessErrorKey()).replaceAll('{masked}', phoneMasked ?? '')}
        </div>
      {/if}

    <form class="form" onsubmit={onSubmit} novalidate>
      <div class="field">
        <label for="phone">{$t('product.phone_label')}</label>
        <input id="phone" type="tel" bind:value={phone} placeholder={$t('product.phone_placeholder')} autocomplete="tel" />
        {#if fieldErrors.phone}<p class="field-err">{$t(fieldErrorKey('phone'))}</p>{/if}
      </div>

      <div class="field">
        <label for="code">{$t('product.code_label')}</label>
        <div class="code-row">
          <input id="code" type="text" inputmode="numeric" maxlength="6" bind:value={code} placeholder={$t('product.code_placeholder')} autocomplete="one-time-code" />
          <button type="button" class="send-btn" onclick={onSendCode} disabled={sending || countdown > 0}>
            {countdown > 0
              ? $t('product.resend_in').replaceAll('{s}', String(countdown))
              : $t('product.send_code')}
          </button>
        </div>
        {#if fieldErrors.code}<p class="field-err">{$t(fieldErrorKey('code'))}</p>{/if}
      </div>

      {#if !activated}
        <div class="field">
          <label for="activationCode">{$t('product.activation_label')}</label>
          <input id="activationCode" type="text" bind:value={activationCode} placeholder={$t('product.activation_placeholder')} />
          <p class="field-hint">{$t('product.activation_hint')}</p>
          {#if fieldErrors.activationCode}<p class="field-err">{$t(fieldErrorKey('activationCode'))}</p>{/if}
        </div>

        <div class="field">
          <label for="boxCode">{$t('product.box_code_label')}</label>
          <input id="boxCode" type="text" bind:value={boxCode} placeholder={$t('product.box_code_placeholder')} />
          <p class="field-hint">{$t('product.box_code_hint')}</p>
          {#if fieldErrors.boxCode}<p class="field-err">{$t(fieldErrorKey('boxCode'))}</p>{/if}
        </div>
      {/if}

      <div class="field">
        <label for="nickname">{$t('product.nickname_label')}</label>
        <input id="nickname" type="text" bind:value={nickname} oninput={() => (nicknameEdited = true)} placeholder={$t('product.nickname_placeholder')} />
        {#if fieldErrors.nickname}<p class="field-err">{$t(fieldErrorKey('nickname'))}</p>{/if}
      </div>

      <button type="submit" class="submit-btn" disabled={submitting}>
        {submitting
          ? $t('product.submitting')
          : (activated ? $t('product.submit_login') : $t('product.submit_activate'))}
      </button>
    </form>
    {/if}

    <footer class="footer">
      <button class="link" onclick={() => (legalModal = 'terms')}>{brandTermsTitle($locale)}</button>
      <span class="dot">·</span>
      <button class="link" onclick={() => (legalModal = 'privacy')}>{brandPrivacyTitle($locale)}</button>
    </footer>
  </div>
</div>

<LegalModal
  title={legalModal === 'terms' ? brandTermsTitle($locale) : brandPrivacyTitle($locale)}
  body={legalModal === 'terms' ? brandText('termsBody', $locale) : brandText('privacyBody', $locale)}
  open={legalModal !== null}
  onClose={() => (legalModal = null)}
/>

<style>
.blocked {
  position: fixed; inset: 0; z-index: 1000;
  display: flex; align-items: center; justify-content: center;
  background: var(--bg-layout); padding: 24px; overflow-y: auto;
}
.lang-switch {
  position: fixed; top: 18px; right: 22px;
  display: flex; align-items: center; gap: 8px; z-index: 1001;
}
.lang-switch button {
  border: none; background: transparent; font-family: inherit;
  font-size: 13px; color: var(--text-secondary); cursor: pointer; padding: 4px 8px;
  border-radius: 6px;
}
.lang-switch button:hover { color: var(--text); }
.lang-switch button.on { color: var(--blue-6); font-weight: 600; background: var(--active-blue-bg); }
.lang-switch .sep { color: var(--text-quaternary); font-size: 12px; }

.card {
  width: 100%; max-width: 440px;
  background: var(--bg-container); border: 1px solid var(--border);
  border-radius: var(--radius-card); box-shadow: var(--card-shadow);
  padding: 36px 32px 24px;
  display: flex; flex-direction: column; gap: 22px;
}
.brand { display: flex; flex-direction: column; align-items: center; gap: 8px; text-align: center; }
.brand-name { margin: 4px 0 0; font-size: 22px; font-weight: 600; color: var(--text-heading); }
.tagline { margin: 0; font-size: 13px; color: var(--text-secondary); line-height: 1.6; }

.form-err {
  padding: 10px 14px; border-radius: 8px;
  background: var(--error-bg, rgba(255,59,48,0.08)); color: var(--error);
  font-size: 13px; line-height: 1.5;
}
/* The tier banner is a message plus, where it applies, the one action that can
   help. Laid out as a column so the button does not wrap beside long copy. */
.form-err.tier { display: flex; flex-direction: column; gap: 10px; align-items: flex-start; }
.tier-msg { margin: 0; }
.retry-btn {
  height: 30px; padding: 0 14px;
  border: 1px solid currentColor; border-radius: 6px;
  background: transparent; color: inherit;
  font-size: 13px; font-weight: 500; font-family: inherit; cursor: pointer;
}
.retry-btn:hover { background: rgba(255,59,48,0.10); }

/* The two misconfiguration pages (L-B2). Deliberately plain: a heading that
   names the problem and a paragraph that names who to ask, and nothing that
   looks like a setting the user could change. */
.notice-title { margin: 0; font-size: 16px; font-weight: 600; color: var(--text-heading); text-align: center; }
.notice-body { margin: 0; font-size: 13px; line-height: 1.7; color: var(--text-secondary); text-align: center; }

.form { display: flex; flex-direction: column; gap: 16px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.field label { font-size: 13px; font-weight: 500; color: var(--text); }
.field input {
  height: 38px; padding: 0 12px;
  border: 1px solid var(--border); border-radius: 8px;
  font-size: 14px; font-family: inherit; color: var(--text);
  background: var(--bg-container); outline: none;
}
.field input:focus { border-color: var(--blue-6); box-shadow: 0 0 0 2px var(--active-blue-bg); }
.field input::placeholder { color: var(--text-quaternary); }
.field-err { margin: 0; font-size: 12px; color: var(--error); }
.field-hint { margin: 0; font-size: 12px; color: var(--text-tertiary); }

.code-row { display: flex; gap: 8px; }
.code-row input { flex: 1; min-width: 0; }
.send-btn {
  flex: none; height: 38px; padding: 0 14px;
  border: 1px solid var(--blue-6); border-radius: 8px;
  background: transparent; color: var(--blue-6);
  font-size: 13px; font-weight: 500; cursor: pointer; font-family: inherit;
  white-space: nowrap;
}
.send-btn:hover:not(:disabled) { background: var(--active-blue-bg); }
.send-btn:disabled { opacity: 0.5; cursor: not-allowed; }

.submit-btn {
  height: 40px; margin-top: 4px;
  border: none; border-radius: 8px;
  background: var(--blue-6); color: #fff;
  font-size: 14px; font-weight: 600; cursor: pointer; font-family: inherit;
  box-shadow: 0 1px 2px rgba(0,122,255,0.35);
}
.submit-btn:hover:not(:disabled) { background: var(--blue-5); }
.submit-btn:disabled { opacity: 0.6; cursor: not-allowed; }

.footer { display: flex; align-items: center; justify-content: center; gap: 8px; }
.footer .link {
  border: none; background: transparent; font-family: inherit;
  font-size: 12px; color: var(--text-tertiary); cursor: pointer; padding: 0;
}
.footer .link:hover { color: var(--blue-6); }
.footer .dot { color: var(--text-quaternary); font-size: 12px; }
</style>
