<script lang="ts">
  import { confirmModals, activeSessionId } from '../../lib/stores'
  import { get } from 'svelte/store'
  import { ws } from '../../lib/ws'
  import { t } from '../../lib/i18n'
  import StatusTag from '../ui/StatusTag.svelte'

  // OCTO-FORK: approval belongs to the active chat flow; it never steals focus.
  const current = $derived($activeSessionId ? $confirmModals[$activeSessionId] : undefined)

  function isCurrent(request: NonNullable<typeof current>, element: HTMLElement) {
    if (!request) return false
    if (element.closest<HTMLElement>('[data-confirm-id]')?.dataset.confirmId !== request.id) return false
    return get(activeSessionId) === request.sessionId && get(confirmModals)[request.sessionId]?.id === request.id
  }
  function clearCurrent(request: NonNullable<typeof current>) {
    confirmModals.update(m => {
      if (m[request.sessionId]?.id !== request.id) return m
      const next = { ...m }; delete next[request.sessionId]; return next
    })
  }
  function answer(request: NonNullable<typeof current>, result: string, element: HTMLElement) {
    if (!isCurrent(request, element)) return
    ws.answerConfirmation(request.id, result)
    clearCurrent(request)
  }
  function stopTask(request: NonNullable<typeof current>, element: HTMLElement) {
    if (!isCurrent(request, element)) return
    ws.interrupt(request.sessionId)
    clearCurrent(request)
  }

  // #1105: diff line classification, same rule ToolGroup.svelte uses for
  // the post-execution edit card — kept in sync so the preview and the
  // result look identical.
  function diffLineClass(line: string): string {
    if (line.startsWith('@@')) return 'diff-hdr'
    if (line.startsWith('-')) return 'diff-line rm'
    if (line.startsWith('+')) return 'diff-line add'
    return 'diff-line plain'
  }
</script>

{#if current}
{@const request = current}
<section data-confirm-id={request.id} class="approval-card" aria-label={$t('perm.title')}>
    <div class="modal-header">
      <iconify-icon icon="ant-design:safety-outlined" width="16" style="color:var(--warning);flex-shrink:0"></iconify-icon>
      <span class="modal-title">{$t('perm.title')}</span>
      <span class="header-tag">
        <StatusTag status="warning">{$t('perm.awaiting')}</StatusTag>
      </span>
    </div>

    <div class="modal-body">
      {#if current.message}
        <p class="desc">{current.message}</p>
      {/if}
      {#if current.command}
        <pre class="terminal"><span class="prompt">$</span> {current.command}</pre>
      {:else if current.diff}
        <div class="diff-block">
          {#each current.diff.split('\n') as line}
            <div class={diffLineClass(line)}>{line}</div>
          {/each}
        </div>
      {:else if current.input}
        <pre class="terminal">{current.input}</pre>
      {:else if current.content}
        <pre class="terminal">{current.content}</pre>
      {/if}
    </div>

    <div class="modal-footer">
      {#if current.kind === 'ok'}
        <button class="btn-stop" onclick={(event) => stopTask(request, event.currentTarget)}>{$t('perm.stop_task')}</button>
        <span class="spacer"></span>
        <button class="btn-primary" onclick={(event) => answer(request, 'ok', event.currentTarget)}>{$t('common.ok')}</button>
      {:else}
        <button class="btn-deny" onclick={(event) => answer(request, 'deny', event.currentTarget)}>
          <iconify-icon icon="ant-design:close-outlined" width="12"></iconify-icon>
          {$t('perm.deny')}
        </button>
        <button class="btn-stop" onclick={(event) => stopTask(request, event.currentTarget)}>{$t('perm.stop_task')}</button>
        <span class="spacer"></span>
        <!-- Result strings are the wire contract with the server's mapConfirmResult:
             'yes' = allow once, 'always' = allow + remember this command/file signature for the session.
             Anything else (incl. 'deny') denies. Don't rename without updating the server.
             #1105: "allow once" is now the primary (blue) button — the most
             prominent action should be the least permissive one, not
             "allow for session". -->
        <button class="btn-primary" onclick={(event) => answer(request, 'yes', event.currentTarget)}>
          <iconify-icon icon="ant-design:check-outlined" width="12"></iconify-icon>
          {$t('perm.allow_once')}
        </button>
        <button class="btn-secondary" onclick={(event) => answer(request, 'always', event.currentTarget)}>{$t('perm.allow_session')}</button>
      {/if}
    </div>
</section>
{/if}

<style>
.approval-card {
  margin: 12px 0;
  background: linear-gradient(var(--warning-bg), var(--warning-bg)), var(--bg-container);
  border: 1px solid var(--warning-border);
  border-radius: 12px;
  overflow: hidden;
}
.modal-header {
  display: flex; flex-wrap: wrap; align-items: center; gap: 8px;
  padding: 12px 18px;
  border-bottom: 1px solid var(--warning-border);
}
.modal-title {
  font-size: 14px; font-weight: 600; color: var(--text-heading); flex: 1;
}
.header-tag {
  margin-left: auto; flex-shrink: 0;
}
.modal-body {
  padding: 16px 18px;
  display: flex; flex-direction: column; gap: 12px;
}
.desc {
  margin: 0;
  /* #2103: full-strength text, not --text-secondary. This line says what is
     about to be approved, and against the tinted panel the secondary grey
     lands at ~3.2:1 (light) / ~3.6:1 (dark) — under the 4.5:1 AA floor for
     13px. The rest of the modal's greys are chrome; this one is content. */
  font-size: 13px; line-height: 1.6; color: var(--text);
}
.terminal {
  margin: 0; padding: 10px 12px;
  background: var(--terminal-bg); color: var(--terminal-text);
  border-radius: 6px;
  font-size: 12px; line-height: 1.6;
  font-family: var(--font-mono);
  overflow-x: auto; overflow-y: auto; max-height: 220px;
  white-space: pre-wrap; word-break: break-all;
}
.prompt { color: var(--success); }
/* #1105: edit_file preview — same classification/coloring ToolGroup.svelte
   uses for the post-execution diff card, scoped to this modal's spacing. */
.diff-block {
  /* #2103: its own surface, so the half-opaque add/rm line fills blend with
     the neutral container the way they do in ToolGroup's diff card, not with
     the modal's orange tint. */
  background: var(--bg-container);
  border: 1px solid var(--border); border-radius: 6px;
  overflow: hidden; overflow-y: auto; max-height: 220px;
  font-size: 12px; line-height: 1.6;
  font-family: var(--font-mono);
}
.diff-hdr { padding: 4px 10px; color: var(--text-tertiary); }
.diff-line { padding: 1px 10px; white-space: pre-wrap; word-break: break-all; }
.diff-line.rm { background: var(--error-bg); color: var(--error-dark); border-left: 2px solid var(--error); }
.diff-line.add { background: var(--success-bg); color: var(--success-text); border-left: 2px solid var(--success); }
.diff-line.plain { color: var(--text-secondary); }
.modal-footer {
  padding: 12px 18px;
  border-top: 1px solid var(--warning-border);
  display: flex; flex-wrap: wrap; align-items: center; gap: 8px;
}
.spacer { flex: 1; }
.btn-deny {
  height: 32px; padding: 0 12px;
  border: 1px solid var(--border); background: var(--bg-container);
  border-radius: 6px;
  display: flex; align-items: center; gap: 6px;
  font-size: 13px; color: var(--text-secondary);
  cursor: pointer; font-family: inherit;
}
.btn-deny:hover { border-color: var(--error); color: var(--error); }
.btn-stop {
  height: 32px; padding: 0 12px;
  border: 1px solid var(--error-border); background: var(--error-bg);
  border-radius: 6px; font-size: 13px; color: var(--error);
  cursor: pointer; font-family: inherit;
  white-space: nowrap; flex-shrink: 0;
}
.btn-stop:hover { border-color: var(--error); }
.btn-secondary {
  height: 32px; padding: 0 12px;
  border: 1px solid var(--border); background: var(--bg-container);
  border-radius: 6px;
  font-size: 13px; color: var(--text-secondary);
  cursor: pointer; font-family: inherit;
}
.btn-secondary:hover { border-color: var(--blue-5); color: var(--blue-5); }
.btn-primary {
  height: 32px; padding: 0 14px;
  border: none; background: var(--blue-6);
  border-radius: 6px;
  display: flex; align-items: center; gap: 6px;
  font-size: 13px; color: var(--on-accent);
  cursor: pointer; font-family: inherit;
}
.btn-primary:hover { background: var(--blue-5); }
</style>
