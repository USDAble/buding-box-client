<script lang="ts">
  import { onboardPhase } from '../../../web/src/lib/stores'
  import OctoLogo from '../../../web/src/components/layout/OctoLogo.svelte'
  import { authPrompt, PORTABLE_PASSWORD, submitAuthKey } from './portable-auth'

  let showForm = $state(false)
  let value = $state('')
  let submitting = $state(false)
  let inputEl = $state<HTMLInputElement | null>(null)

  $effect(() => {
    const prompt = $authPrompt
    const phase = $onboardPhase
    if (!prompt) {
      if (submitting && phase !== 'unknown') submitting = false
      return
    }
    submitting = false
    if (prompt.retry) showForm = true
    if (showForm && inputEl) inputEl.focus()
  })

  function beginLogin() {
    showForm = true
    requestAnimationFrame(() => inputEl?.focus())
  }

  function submit() {
    const key = value.trim()
    if (!key || submitting) return
    if (key !== PORTABLE_PASSWORD) {
      value = ''
      submitAuthKey(key)
      requestAnimationFrame(() => inputEl?.focus())
      return
    }
    submitting = true
    submitAuthKey(key)
  }
</script>

{#if $authPrompt || submitting}
  <main class="login-page" aria-labelledby="portable-login-title">
    <section class="login-content">
      <div class="mascot" aria-hidden="true">
        <span class="orbit orbit-one"></span>
        <span class="orbit orbit-two"></span>
        <div class="brand-mark"><OctoLogo size={94} /></div>
      </div>

      <div class="copy">
        <h1 id="portable-login-title">Buding Box，我帮你</h1>
        <p>你的便携 AI 工作伙伴</p>
      </div>

      {#if showForm}
        <form class="login-form" onsubmit={(event) => { event.preventDefault(); submit() }}>
          <label for="portable-access-key">访问密钥</label>
          <input
            id="portable-access-key"
            bind:this={inputEl}
            bind:value
            type="password"
            placeholder="输入 U 盘中的登录密钥"
            autocomplete="current-password"
            disabled={submitting}
            aria-describedby={$authPrompt?.retry ? 'portable-login-error portable-login-help' : 'portable-login-help'}
          />
          {#if $authPrompt?.retry}
            <p id="portable-login-error" class="error" role="alert">密钥不正确，请重新输入。</p>
          {/if}
          <p id="portable-login-help" class="help">当前测试口令为 123456。</p>
          <button class="primary" type="submit" disabled={!value.trim() || submitting}>
            {submitting ? '正在进入…' : '进入 Buding Box'}
          </button>
          <button class="back" type="button" onclick={() => (showForm = false)}>返回</button>
        </form>
      {:else}
        <button class="primary login-button" type="button" onclick={beginLogin}>登录</button>
      {/if}
    </section>

    <footer aria-label="法律信息">
      <span>隐私政策</span>
      <span>服务条款</span>
    </footer>
  </main>
{/if}

<style>
  .login-page {
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

  .login-content {
    width: min(440px, calc(100vw - 40px));
    margin: auto;
    padding: 64px 0 36px;
    display: flex;
    flex-direction: column;
    align-items: center;
  }

  .mascot {
    position: relative;
    width: 172px;
    height: 172px;
    display: grid;
    place-items: center;
    margin-bottom: 28px;
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

  .orbit-one { width: 146px; height: 146px; }
  .orbit-two { width: 172px; height: 92px; transform: rotate(-24deg); }

  .copy { text-align: center; }

  h1 {
    margin: 0;
    font-size: clamp(30px, 4vw, 42px);
    line-height: 1.18;
    letter-spacing: -0.035em;
    font-weight: 720;
  }

  .copy p {
    margin: 12px 0 0;
    color: var(--text-secondary, #71717a);
    font-size: 15px;
    line-height: 1.6;
  }

  .primary {
    width: 100%;
    min-height: 52px;
    border: 0;
    border-radius: 999px;
    background: var(--text-heading, #18181b);
    color: var(--bg-container, #fff);
    font: inherit;
    font-size: 16px;
    font-weight: 650;
    cursor: pointer;
    transition: opacity 180ms ease, background-color 180ms ease;
  }

  .login-button { width: 288px; margin-top: 38px; }
  .primary:hover:not(:disabled) { opacity: 0.82; }
  .primary:disabled { opacity: 0.42; cursor: not-allowed; }
  .primary:focus-visible, input:focus-visible, .back:focus-visible {
    outline: 3px solid var(--focus-ring, rgba(37, 99, 235, 0.3));
    outline-offset: 3px;
  }

  .login-form {
    width: min(360px, 100%);
    margin-top: 32px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  label {
    font-size: 14px;
    font-weight: 620;
  }

  input {
    width: 100%;
    height: 50px;
    padding: 0 16px;
    border: 1px solid var(--border, rgba(0, 0, 0, 0.12));
    border-radius: 14px;
    background: var(--bg-layout, #fafafa);
    color: var(--text, #18181b);
    font: 15px var(--font-mono, ui-monospace, monospace);
  }

  input:focus { border-color: var(--text-secondary, #71717a); }
  .help, .error { margin: 0; font-size: 12px; line-height: 1.55; }
  .help { color: var(--text-secondary, #71717a); }
  .error { color: var(--error, #dc2626); }

  .back {
    align-self: center;
    min-width: 64px;
    min-height: 44px;
    border: 0;
    background: transparent;
    color: var(--text-secondary, #71717a);
    font: inherit;
    cursor: pointer;
  }

  footer {
    min-height: 76px;
    padding: 20px;
    display: flex;
    justify-content: center;
    align-items: center;
    gap: 28px;
    color: var(--text-secondary, #71717a);
    font-size: 13px;
  }

  @media (max-height: 680px) {
    .login-content { padding-top: 32px; }
    .mascot { width: 132px; height: 132px; margin-bottom: 18px; }
    .brand-mark { transform: scale(0.8); }
    .orbit-one { width: 116px; height: 116px; }
    .orbit-two { width: 136px; height: 74px; }
    .login-button { margin-top: 26px; }
  }

  @media (prefers-reduced-motion: reduce) {
    .primary { transition: none; }
  }
</style>
