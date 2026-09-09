<script lang="ts">
  import { t } from '../../../lib/i18n'
  import { showToast } from '../../../lib/stores'
  import { confirmDialog } from '../../../lib/confirm'
  import {
    fetchDict, saveDict, importWords, exportDict,
    normalizeWord, isUsableWord, containsWord, parseDictText,
    type SensitiveDict,
  } from '../../../lib/sensitiveDict'

  // Sensitive-word dictionary page (P13). The file data/sensitive-words.txt
  // is the single source of truth — this page is only its editor. Built-in
  // words are read-only; user words are add/delete/edit plus import/export.
  // OCTO-FORK: P13 — see
  // dev-docs-usdable/需求/2260906/技术方案/P13-词库管理界面.md.

  let dict = $state<SensitiveDict>({ builtin: [], user: [] })
  let loading = $state(true)
  let builtinOpen = $state(false)
  let draft = $state('')
  let editingIdx = $state<number | null>(null)
  let editDraft = $state('')
  let saving = $state(false)
  let fileInput = $state<HTMLInputElement | null>(null)

  async function load() {
    loading = true
    try {
      dict = await fetchDict()
    } catch {
      showToast($t('product.send_failed'), 'error')
    } finally {
      loading = false
    }
  }

  async function persist(user: string[]) {
    saving = true
    try {
      const res = await saveDict(user)
      dict = { ...dict, user: res.user }
      showToast($t('product.dict.saved'))
    } catch (e) {
      showToast((e as Error)?.message ?? $t('product.send_failed'), 'error')
    } finally {
      saving = false
    }
  }

  function onAdd() {
    const word = draft.trim()
    if (!word) return
    if (!isUsableWord(word)) {
      showToast($t('product.dict.invalid_word'), 'error')
      return
    }
    if (containsWord(dict, word)) {
      showToast($t('product.dict.duplicate_word'), 'error')
      return
    }
    draft = ''
    void persist([...dict.user, word])
  }

  function onDelete(word: string) {
    void persist(dict.user.filter((w) => w !== word))
  }

  function startEdit(i: number) {
    editingIdx = i
    editDraft = dict.user[i]
  }

  async function commitEdit(i: number) {
    const word = editDraft.trim()
    if (!word || !isUsableWord(word)) {
      showToast($t('product.dict.invalid_word'), 'error')
      return
    }
    const others = dict.user.filter((_, j) => j !== i)
    const dup =
      others.some((w) => normalizeWord(w) === normalizeWord(word)) ||
      dict.builtin.some((w) => normalizeWord(w) === normalizeWord(word))
    if (dup) {
      showToast($t('product.dict.duplicate_word'), 'error')
      return
    }
    editingIdx = null
    await persist(dict.user.map((w, j) => (j === i ? word : w)))
  }

  async function onExport() {
    try {
      await exportDict(dict.user)
      showToast($t('product.dict.exported'))
    } catch {
      showToast($t('product.send_failed'), 'error')
    }
  }

  async function onImportFile(ev: Event) {
    const input = ev.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    input.value = ''
    if (!file) return
    const text = await file.text()
    const words = parseDictText(text)
    if (words.length === 0) return
    try {
      const preview = await importWords(words, true)
      const msg = $t('product.dict.import_preview')
        .replaceAll('{added}', String(preview.added))
        .replaceAll('{skipped}', String(preview.skipped))
      const ok = await confirmDialog(msg, {
        title: $t('product.dict.import_confirm'),
        confirmLabel: $t('product.dict.import_confirm'),
      })
      if (!ok) return
      await importWords(words, false)
      await load()
    } catch {
      showToast($t('product.send_failed'), 'error')
    }
  }

  // Load once on mount. The panel recreates this component per open, so a
  // fresh snapshot each time also satisfies "hand-edited file shows on reopen".
  void load()
</script>

{#if loading}
  <p class="hint">{$t('product.dict.instant_effect')}</p>
{:else}
  <div class="dict">
    <div class="section">
      <button class="section-head" onclick={() => (builtinOpen = !builtinOpen)}>
        <span class="lbl">{$t('product.dict.builtin')}</span>
        <span class="count">{$t('product.dict.count').replaceAll('{n}', String(dict.builtin.length))}</span>
        <iconify-icon icon={builtinOpen ? 'lucide:chevron-down' : 'lucide:chevron-right'} width="13" style="color:var(--text-quaternary)"></iconify-icon>
      </button>
      {#if builtinOpen}
        <ul class="words read-only">
          {#each dict.builtin as w}
            <li class="chip">{w}</li>
          {/each}
        </ul>
      {/if}
    </div>

    <div class="section">
      <div class="section-head plain">
        <span class="lbl">{$t('product.dict.user')}</span>
        <span class="count">{$t('product.dict.count').replaceAll('{n}', String(dict.user.length))}</span>
      </div>
      {#if dict.user.length === 0}
        <p class="empty">{$t('product.dict.empty')}</p>
      {:else}
        <ul class="words">
          {#each dict.user as w, i}
            <li class="row">
              {#if editingIdx === i}
                <input class="edit-input" bind:value={editDraft} maxlength="64" spellcheck="false" />
                <button class="icon-btn" onclick={() => commitEdit(i)} aria-label={$t('common.save')}>
                  <iconify-icon icon="ant-design:check-outlined" width="13"></iconify-icon>
                </button>
                <button class="icon-btn" onclick={() => (editingIdx = null)} aria-label={$t('common.cancel')}>
                  <iconify-icon icon="ant-design:close-outlined" width="13"></iconify-icon>
                </button>
              {:else}
                <button class="word-btn" onclick={() => startEdit(i)} title={$t('common.edit')}>{w}</button>
                <button class="icon-btn" onclick={() => onDelete(w)} aria-label={$t('common.delete')}>
                  <iconify-icon icon="ant-design:close-outlined" width="13"></iconify-icon>
                </button>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </div>

    <div class="add-row">
      <input
        bind:value={draft}
        placeholder={$t('product.dict.add_placeholder')}
        maxlength="64"
        spellcheck="false"
        onkeydown={(e) => { if (e.key === 'Enter') onAdd() }}
      />
      <button class="add-btn" onclick={onAdd} disabled={saving} aria-label={$t('product.dict.add_placeholder')}>
        <iconify-icon icon="ant-design:plus-outlined" width="14"></iconify-icon>
      </button>
    </div>

    <div class="actions">
      <button class="action" onclick={() => fileInput?.click()}>{$t('product.dict.import')}</button>
      <button class="action" onclick={onExport}>{$t('product.dict.export')}</button>
    </div>

    <input type="file" accept=".txt,text/plain" bind:this={fileInput} class="file-input" onchange={onImportFile} />

    <p class="hint">{$t('product.dict.instant_effect')}</p>
  </div>
{/if}

<style>
  .dict { display: flex; flex-direction: column; gap: 14px; padding: 4px 2px 14px; }
  .section { display: flex; flex-direction: column; gap: 6px; }
  .section-head {
    display: flex; align-items: center; gap: 6px;
    width: 100%; padding: 2px 0; border: none; background: transparent;
    font-family: inherit; text-align: left; cursor: pointer;
  }
  .section-head.plain { cursor: default; }
  .lbl { flex: 1; font-size: 13px; font-weight: 600; color: var(--text-heading); }
  .count { font-size: 12px; color: var(--text-tertiary); font-variant-numeric: tabular-nums; }

  .words { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .words.read-only { flex-direction: row; flex-wrap: wrap; }
  .chip {
    font-size: 12px; color: var(--text-secondary);
    padding: 2px 8px; border-radius: 6px; background: var(--hover-neutral, rgba(0,0,0,0.04));
    border: 1px solid var(--border-secondary);
  }

  .row {
    display: flex; align-items: center; gap: 4px;
    border: 1px solid var(--border-secondary); border-radius: 8px; padding: 2px 6px;
    background: var(--bg-container);
  }
  .word-btn {
    flex: 1; min-width: 0; text-align: left; border: none; background: transparent;
    font-family: inherit; font-size: 13px; color: var(--text);
    padding: 4px 2px; cursor: pointer; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .edit-input {
    flex: 1; min-width: 0; height: 26px; padding: 0 8px;
    border: 1px solid var(--border); border-radius: 6px;
    font-size: 13px; font-family: inherit; color: var(--text); background: var(--bg-container); outline: none;
  }
  .edit-input:focus { border-color: var(--blue-6); box-shadow: 0 0 0 2px var(--active-blue-bg); }
  .icon-btn {
    display: flex; align-items: center; justify-content: center;
    width: 24px; height: 24px; border: none; border-radius: 6px;
    background: transparent; color: var(--text-quaternary); cursor: pointer; flex: 0 0 auto;
  }
  .icon-btn:hover { background: var(--hover-neutral); color: var(--text-secondary); }

  .empty { margin: 0; font-size: 12px; color: var(--text-tertiary); padding: 4px 0; }

  .add-row { display: flex; gap: 6px; }
  .add-row input {
    flex: 1; min-width: 0; height: 30px; padding: 0 10px;
    border: 1px solid var(--border); border-radius: 6px;
    font-size: 13px; font-family: inherit; color: var(--text); background: var(--bg-container); outline: none;
  }
  .add-row input:focus { border-color: var(--blue-6); box-shadow: 0 0 0 2px var(--active-blue-bg); }
  .add-btn {
    flex: 0 0 auto; width: 30px; height: 30px; display: flex; align-items: center; justify-content: center;
    border: none; border-radius: 6px; background: var(--blue-6); color: #fff; cursor: pointer;
  }
  .add-btn:hover:not(:disabled) { background: var(--blue-5); }
  .add-btn:disabled { opacity: 0.5; cursor: not-allowed; }

  .actions { display: flex; gap: 8px; }
  .action {
    flex: 1; height: 30px; border: 1px solid var(--border); border-radius: 8px;
    background: transparent; color: var(--text-secondary); font-family: inherit; font-size: 12px; cursor: pointer;
  }
  .action:hover { background: var(--hover-neutral); color: var(--text); }

  .file-input { display: none; }
  .hint { margin: 0; font-size: 12px; color: var(--text-tertiary); line-height: 1.6; }
</style>
