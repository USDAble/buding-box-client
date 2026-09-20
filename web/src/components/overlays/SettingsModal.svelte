<script lang="ts">
  import { onDestroy, untrack } from 'svelte'
  import Segment from '../ui/Segment.svelte'
  import ThemePackPicker from '../ui/ThemePackPicker.svelte'
  import Switch from '../ui/Switch.svelte'
  import EndpointsSection from '../settings/EndpointsSection.svelte'
  import SafetyPrivacySection from '../settings/SafetyPrivacySection.svelte'
  // OCTO-FORK: wallet and recharge use published platform configuration.
  import WalletSection from '../settings/WalletSection.svelte'
  import QrCode from '../ui/QrCode.svelte'
  import FileRecallView from '../../views/FileRecallView.svelte'
  import ProfileView from '../../views/ProfileView.svelte'
  import { get } from 'svelte/store'
  import { showToast, nativeShell, settingsModalOpen, settingsTarget, onboardPhase, sessions, sessionGroups, collapsedSessions, activeSessionId, view, clearPendingSessionOpts } from '../../lib/stores'
  import type { Session, SessionGroup } from '../../lib/types'
  import { setLocale, t, tr } from '../../lib/i18n'
  import { getMode, setMode, type ThemeMode } from '../../lib/theme'
  import { notificationsEnabled, setNotificationsEnabled } from '../../lib/notifications'
  import { openUrl } from '../../lib/externalLinks'
  import { brandLink } from '../../lib/brand'
  import { confirmDialog } from '../../lib/confirm'
  import { ago, clockTick } from '../../lib/relTime'
  import * as api from '../../lib/api'
  import { allowEnvironmentModelSource, productState, updateNickname, ProductError, logout, submitFeedback, getBox, type BoxDTO } from '../../lib/product'
  // OCTO-FORK: account and safety controls are product-owned settings, kept
  // out of the compact account popup so it stays single-level.
  import { validateNickname } from '../../lib/nickname'

  const LICENSE_URL = 'https://github.com/open-octo/octo-agent/blob/main/LICENSE.txt'

  const fontZoomMap: Record<string, string> = { Small: '0.9', Medium: '1', Large: '1.1' }
  const modeToThemeLabel: Record<string, string> = { light: 'Light', dark: 'Dark', system: 'System' }

  // This modal is mounted unconditionally at app root, so the applying
  // $effects at the bottom run at boot, not on open. Seeding fontSize/theme
  // from the persisted prefs (not hardcoded defaults) is what keeps a page
  // reload from overwriting the stored choice before Settings is ever opened
  // (#2087) — and since nothing else reads octo.fontSize, this seed is also
  // the font-size restore path.
  function storedFontSize(): string {
    const v = localStorage.getItem('octo.fontSize')
    return v === 'Small' || v === 'Medium' || v === 'Large' ? v : 'Medium'
  }

  // --- local state ---
  let language      = $state('en')
  let fontSize      = $state(storedFontSize())
  let theme         = $state(modeToThemeLabel[getMode()] ?? 'Light')
  let autostart     = $state(false) // desktop shell only
  let serverOs      = $state('')    // /api/version's os — gates the experimental tab
  let computerUse   = $state(false) // tools.computer.enabled toggle (experimental tab)
  const computerPlatform = $derived(serverOs === 'darwin' || serverOs === 'windows')
  // macOS prompts for two system grants; Windows has none but blocks input
  // into elevated apps — the hint under the toggle says which applies.
  const computerHintKey = $derived(serverOs === 'windows'
    ? 'settings.experimental.computer_use_hint_windows'
    : 'settings.experimental.computer_use_hint')
  let versionStr    = $state('')
  let latestStr     = $state('')
  let updateAvail   = $state(false)
  let downloadUrl   = $state('')
  let checkingUpdate = $state(false)
  // 'installer' (desktop build) downloads the release page; 'cli' (plain
  // `octo serve`) can swap itself in place — same distinction VersionBadge
  // uses, which stays mounted (Sidebar.svelte) and picks up this modal's
  // triggered upgrade via the same global upgrade_log/upgrade_complete WS
  // broadcasts, so its badge reflects progress after this modal closes.
  let upgradeMode   = $state<'cli' | 'installer'>('cli')
  let loading       = $state(true)

  let cat = $state<'general' | 'account' | 'wallet' | 'safety' | 'box' | 'help' | 'endpoints' | 'agent' | 'mobile' | 'experimental' | 'data' | 'about'>('general')
  let modalEl = $state<HTMLDivElement | null>(null)
  const accountNickname = $derived($productState?.account?.nickname ?? '')
  const accountPhone = $derived($productState?.account?.phoneMasked ?? '—')
  const accountLicense = $derived($productState?.activation?.expiresAt || ($productState?.activated && $productState.activation?.activatedAt ? $t('product.panel.license_permanent') : ''))
  let nicknameDraft = $state('')
  let nicknameErr = $state<'' | 'nickname_format' | 'nickname_sensitive' | 'save_failed'>('')
  let savingNickname = $state(false)
  let feedbackCategory = $state<'bug' | 'suggestion' | 'other'>('suggestion')
  let feedbackTitle = $state('')
  let feedbackContent = $state('')
  let feedbackReproduction = $state('')
  let feedbackExpected = $state('')
  let feedbackImpact = $state<'low' | 'normal' | 'high'>('normal')
  let feedbackContact = $state('')
  let helpTab = $state<'guides' | 'feedback'>('guides')
  let feedbackSubmitting = $state(false)
  let feedbackReceipt = $state('')
  let feedbackError = $state('')
  let feedbackKey = $state('')
  let feedbackPayload = ''
  let feedbackCooldownSec = $state(0)
  let feedbackCooldownTimer: ReturnType<typeof setInterval> | undefined
  let box = $state<BoxDTO | null>(null)
  let boxLoading = $state(false)
  let boxError = $state(false)

  // 数据管理 has its own two-level nav — a list of managed things, and one
  // sub-view per thing — because unlike every other category here it isn't a
  // handful of settings but potentially long lists (archived sessions, trashed
  // files). Reset whenever the category (or the whole modal) changes, so
  // leaving and returning to 数据管理 always lands on the list, not wherever
  // you left off.
  let dataSubView = $state<'none' | 'archived' | 'trash' | 'memory'>('none')
  let archiveSearch = $state('')
  // '' = every project, 'none' = no project, else a project's group id.
  let archiveProjectFilter = $state('')
  let archiveSel = $state<Record<string, true>>({})

  function resetDataView() {
    dataSubView = 'none'
    archiveSearch = ''
    archiveProjectFilter = ''
    archiveSel = {}
  }

  const archivedSessions = $derived($sessions.filter(s => $collapsedSessions.includes(s.id)))

  // Archiving keeps a session's project membership (see handleSetSessionCollapsed
  // on the server), so an archived session's project is looked up the same way
  // Sidebar.svelte does it for a live one: by scanning which project's
  // session_ids includes it. Needed both to group the list and to offer the
  // project filter.
  const projectBySession = $derived.by(() => {
    const m = new Map<string, SessionGroup>()
    for (const g of $sessionGroups) {
      if (!g.working_dir) continue
      for (const id of g.session_ids) m.set(id, g)
    }
    return m
  })

  const archivedFiltered = $derived.by(() => {
    const q = archiveSearch.trim().toLowerCase()
    return archivedSessions.filter(s => {
      if (q && !nameOf(s).toLowerCase().includes(q)) return false
      if (archiveProjectFilter === 'none') return !projectBySession.get(s.id)
      if (archiveProjectFilter) return projectBySession.get(s.id)?.id === archiveProjectFilter
      return true
    })
  })

  // One bucket per project, in the sidebar's own project order, then a final
  // "no project" bucket — same shape as the sidebar's own Tasks-after-Projects
  // ordering, just projects first here since a filtered-to-one-project view is
  // the common case this list exists for.
  const archivedGroups = $derived.by(() => {
    const buckets = new Map<string, { group: SessionGroup | null; items: Session[] }>()
    for (const s of archivedFiltered) {
      const g = projectBySession.get(s.id) ?? null
      const key = g ? g.id : ''
      if (!buckets.has(key)) buckets.set(key, { group: g, items: [] })
      buckets.get(key)!.items.push(s)
    }
    const ordered: { group: SessionGroup | null; items: Session[] }[] = []
    for (const g of $sessionGroups) {
      if (buckets.has(g.id)) ordered.push(buckets.get(g.id)!)
    }
    if (buckets.has('')) ordered.push(buckets.get('')!)
    return ordered
  })

  // Only projects that actually have an archived session offer a filter for —
  // an empty option nobody could ever pick is not a choice, it's clutter.
  const archiveProjectOptions = $derived(
    $sessionGroups.filter(g => !!g.working_dir && archivedSessions.some(s => projectBySession.get(s.id)?.id === g.id)),
  )

  function nameOf(s: Session): string {
    return (s as any).name || (s as any).title || s.id
  }

  function toggleArchiveSel(id: string) {
    const n = { ...archiveSel }
    if (n[id]) delete n[id]
    else n[id] = true
    archiveSel = n
  }

  function toggleArchiveSelAll() {
    const ids = archivedFiltered.map(s => s.id)
    const allSelected = ids.length > 0 && ids.every(id => archiveSel[id])
    if (allSelected) {
      archiveSel = {}
    } else {
      const n: Record<string, true> = {}
      for (const id of ids) n[id] = true
      archiveSel = n
    }
  }

  async function unarchiveSession(id: string) {
    const before = get(collapsedSessions)
    collapsedSessions.set(before.filter(x => x !== id))
    try {
      await api.setSessionCollapsed(id, false)
    } catch (e: any) {
      collapsedSessions.set(before)
      showToast(e?.message ?? tr('sidebar.collapse_failed'), 'error')
    }
  }

  async function deleteArchivedSession(id: string) {
    if (!(await confirmDialog(tr('sidebar.confirm_delete')))) return
    try {
      await api.deleteSession(id)
      sessions.update(ss => ss.filter(s => s.id !== id))
      collapsedSessions.update(ids => ids.filter(x => x !== id))
      const { [id]: _drop, ...rest } = archiveSel
      archiveSel = rest
      if (get(activeSessionId) === id) {
        activeSessionId.set(null)
        clearPendingSessionOpts()
        view.set('chat')
      }
    } catch (e: any) {
      showToast(e.message, 'error')
    }
  }

  // Deletes every selected session in one confirmation rather than one
  // per-row — the whole point of selecting several at once. Sessions that
  // fail to delete are reported and kept selected, so retrying is just
  // pressing the button again on what's left.
  async function deleteSelectedArchived() {
    const ids = Object.keys(archiveSel)
    if (ids.length === 0) return
    if (!(await confirmDialog(tr('settings.data.confirm_delete_selected').replace('{n}', String(ids.length))))) return
    const failed: string[] = []
    for (const id of ids) {
      try {
        await api.deleteSession(id)
        sessions.update(ss => ss.filter(s => s.id !== id))
        collapsedSessions.update(cs => cs.filter(x => x !== id))
        if (get(activeSessionId) === id) {
          activeSessionId.set(null)
          clearPendingSessionOpts()
          view.set('chat')
        }
      } catch {
        failed.push(id)
      }
    }
    if (failed.length) {
      const n: Record<string, true> = {}
      for (const id of failed) n[id] = true
      archiveSel = n
      showToast(tr('settings.data.delete_selected_partial').replace('{n}', String(failed.length)), 'error')
    } else {
      archiveSel = {}
    }
  }

  // Managed-tunnel pairing material (null until fetched; .enabled false when
  // the server was not started with --tunnel).
  let tunnelPairing = $state<api.TunnelPairing | null>(null)

  // ── Agent defaults — each control saves immediately on change (see the
  // save* functions below), no separate Save button.
  let reasoningEffort  = $state('off')
  let permissionMode   = $state('interactive')
  let showReasoningVal = $state(true)
  let coauthorVal      = $state(true)
  let updateCheckVal   = $state(true)
  let workspaceDir        = $state('')
  // OCTO-FORK: 前端适配（webview 路由/构建/入口隐藏） — see the approved navigation and credits boundary
  // Resolved default new sessions get when workspaceDir is empty (data/workspace/,
  // expanded server-side) — shown as the input's placeholder instead of a
  // bare "auto" that's easy to mistake for an actually-saved value.
  let workspaceDirDefault = $state('')

  const langOptions = [
    { value: 'en', label: 'English' },
    { value: 'zh', label: '简体中文' },
  ]

  const categories: { key: typeof cat, icon: string, label: string }[] = $derived([
    { key: 'general',   icon: 'ant-design:sliders-outlined',       label: 'settings.general' },
    { key: 'account',   icon: 'ant-design:user-outlined',          label: 'settings.account' },
    { key: 'safety',    icon: 'ant-design:safety-outlined',        label: 'settings.safety' },
    { key: 'wallet', icon: 'ant-design:wallet-outlined', label: 'wallet.title' },
    { key: 'box',       icon: 'lucide:box',                         label: 'settings.box' },
    { key: 'help',      icon: 'ant-design:question-circle-outlined', label: 'settings.help' },
    // OCTO-FORK: product profiles hide local model management from the
    // server-projected capability; null keeps plain octo serve behavior.
    ...($allowEnvironmentModelSource === false ? [] : [{ key: 'endpoints' as const, icon: 'ant-design:api-outlined', label: 'settings.endpoints.title' }]),
    { key: 'agent',     icon: 'ant-design:robot-outlined',         label: 'settings.agent' },
    { key: 'mobile',    icon: 'ant-design:mobile-outlined',        label: 'settings.mobile' },
    // Experimental features (computer-use) need the desktop shell AND a
    // platform with a substrate — macOS (AX/CGEvent) or Windows (UI
    // Automation/SendInput). On macOS only the desktop app can hold the
    // Screen Recording / Accessibility grants.
    ...($nativeShell && computerPlatform
      ? [{ key: 'experimental' as const, icon: 'ant-design:experiment-outlined', label: 'settings.experimental' }]
      : []),
    { key: 'data',      icon: 'ant-design:database-outlined',       label: 'settings.data' },
    { key: 'about',     icon: 'ant-design:info-circle-outlined',   label: 'settings.about' },
  ])

  $effect(() => {
    if ($allowEnvironmentModelSource === false && cat === 'endpoints') cat = 'general'
  })

  // Re-seed on every open, same as the other global modals — reflects
  // whatever config was saved elsewhere (agent chat, another window) since
  // the modal last closed.
  $effect(() => {
    if ($settingsModalOpen) {
      // OCTO-FORK: wallet balance refreshes must not re-run modal navigation initialization.
      untrack(() => {
      // A deep link (command palette, in-app shortcut) picks the category and
      // 数据管理 sub-view; a plain open falls back to the default landing.
      const target = get(settingsTarget)
      cat = (target?.cat as typeof cat) ?? 'general'
      resetDataView()
      if (target?.dataSubView) dataSubView = target.dataSubView as typeof dataSubView
      if (target) settingsTarget.set(null)
      loadConfig()
      loadVersion()
      if (get(nativeShell)) api.getAutostart().then(v => (autostart = v)).catch(() => {})
      api.getTunnelPairing().then(p => { tunnelPairing = p }).catch(() => {})
      theme = modeToThemeLabel[getMode()] ?? 'Light'
      fontSize = storedFontSize()
      nicknameDraft = $productState?.account?.nickname ?? ''
      nicknameErr = ''
      modalEl?.focus()
      })
    }
  })

  // A box read is deliberately tied to its own top-level page. Opening an
  // unrelated setting must not create an invisible control-plane request.
  $effect(() => {
    if ($settingsModalOpen && cat === 'box') void loadBox()
  })

  async function loadConfig() {
    loading = true
    try {
      const cfg = await api.getConfig() as any
      // Absent = old server dropping "" through omitempty = reasoning off.
      reasoningEffort  = cfg.reasoning_effort ?? 'off'
      permissionMode   = cfg.permission_mode ?? 'interactive'
      showReasoningVal = cfg.show_reasoning ?? true
      coauthorVal      = cfg.coauthor ?? true
      updateCheckVal   = cfg.update_check ?? true
      computerUse      = (cfg.computer_enabled ?? '') === 'on'
      // Legacy installer-seeded "auto" resolves to the same default as ""
      // (see tools.ResolveWorkspaceDir) — show it as the empty input with
      // the resolved-default placeholder, not as a literal "auto" the user
      // would mistake for a real path.
      const wd = cfg.workspace_dir ?? ''
      workspaceDir        = wd.trim().toLowerCase() === 'auto' ? '' : wd
      workspaceDirDefault = cfg.workspace_dir_default ?? ''
      if (cfg.language) language = cfg.language
      setLocale(cfg.language === 'zh' || cfg.language === 'zh-TW' ? 'zh' : 'en')
    } catch (e: any) {
      showToast(`Failed to load config: ${e.message}`, 'error')
    } finally {
      loading = false
    }
  }

  async function loadVersion() {
    try {
      const v = await api.getVersion() as any
      versionStr = v.current ?? v.version ?? ''
      serverOs = v.os ?? ''
      latestStr = v.latest ?? ''
      updateAvail = !!v.needs_update
      downloadUrl = v.download_url ?? ''
      upgradeMode = v.upgrade_mode === 'installer' ? 'installer' : 'cli'
    } catch { /* non-critical */ }
  }

  // Unreachable in this fork: the update row in the About card is now a
  // "coming soon" placeholder (需求 §5.1.2 第 13 条 / PQ12), so nothing calls
  // this. Left in place rather than deleted (硬规则 3) — see the marker on that
  // row. Its comment below used to claim the badge is "always mounted"; it is
  // not mounted anywhere in this fork, only referenced by its own test.
  async function checkUpdate() {
    if (checkingUpdate) return
    checkingUpdate = true
    try {
      await loadVersion()
      if (!updateAvail) {
        showToast($t('settings.update.uptodate'), 'success')
        return
      }
      if (upgradeMode === 'installer') {
        if (downloadUrl) {
          try { await api.openExternal(downloadUrl) }
          catch { window.open(downloadUrl, '_blank', 'noopener') }
        }
      } else {
        // Same endpoint VersionBadge's badge uses (POST /api/version/upgrade)
        // — fire it here too and let the badge (always mounted) show live
        // progress via the WS broadcasts it already listens for. Close this
        // modal so the badge's progress popover isn't hidden behind it (#2120).
        // 409 = an upgrade is already in flight; its broadcasts drive the
        // badge, so treat it as started. Any other failure sends no
        // broadcasts — keep the modal open instead of claiming success.
        const res = await fetch('/api/version/upgrade', { method: 'POST' })
        if (res.ok || res.status === 409) {
          showToast($t('settings.update.started'), 'success')
          settingsModalOpen.set(false)
        }
      }
    } finally {
      checkingUpdate = false
    }
  }

  async function copyPairURL() {
    if (!tunnelPairing?.pair_url) return
    try {
      await navigator.clipboard.writeText(tunnelPairing.pair_url)
      showToast($t('settings.mobile.copied'), 'success')
    } catch {
      showToast('Copy failed', 'error')
    }
  }

  async function toggleAutostart(v: boolean) {
    try {
      await api.setAutostart(v)
      autostart = v
    } catch (e: any) {
      showToast(e.message ?? 'Failed to change autostart', 'error')
      autostart = !v
    }
  }

  // ── agent defaults — each saves immediately via its own PUT endpoint
  // (internal/skills/defaults/config-setup/SKILL.md documents the same 5
  // endpoints for the conversational path; this is the identical contract,
  // just called directly instead of through the agent).
  async function saveReasoningEffort(v: string) {
    try {
      await api.updateReasoningEffort(v)
      reasoningEffort = v
    } catch (e: any) {
      showToast(e.message ?? 'Failed to update reasoning effort', 'error')
    }
  }

  async function savePermissionMode(v: string) {
    try {
      await api.updatePermissionMode(v)
      permissionMode = v
    } catch (e: any) {
      showToast(e.message ?? 'Failed to update permission mode', 'error')
    }
  }

  async function saveShowReasoning(v: boolean) {
    try {
      await api.updateShowReasoning(v)
      showReasoningVal = v
    } catch (e: any) {
      showToast(e.message ?? 'Failed to update show-reasoning', 'error')
    }
  }

  async function saveCoauthor(v: boolean) {
    try {
      await api.updateCoauthor(v)
      coauthorVal = v
    } catch (e: any) {
      showToast(e.message ?? 'Failed to update coauthor', 'error')
    }
  }

  async function saveUpdateCheck(v: boolean) {
    try {
      await api.updateUpdateCheck(v)
      updateCheckVal = v
    } catch (e: any) {
      showToast(e.message ?? 'Failed to update update-check', 'error')
    }
  }

  async function saveComputerUse(v: boolean) {
    try {
      await api.updateComputerEnabled(v)
      computerUse = v
    } catch (e: any) {
      showToast(e.message ?? 'Failed to update computer-use', 'error')
    }
  }

  async function saveWorkspaceDir(v: string) {
    try {
      await api.updateWorkspaceDir(v)
      workspaceDir = v
    } catch (e: any) {
      showToast(e.message ?? 'Failed to update workspace directory', 'error')
    }
  }

  // Persist the language like every other config-backed field in this modal.
  // The $effect below already applies it to the live locale store; without
  // this PUT the choice only lived in memory and a refresh reverted to the
  // server's stored language (#2076). FirstRunSetup persists through the same
  // endpoint.
  async function saveLanguage(v: string) {
    try {
      await api.updateLanguage(v)
    } catch (e: any) {
      showToast(e.message ?? 'Failed to update language', 'error')
    }
  }

  async function saveNickname() {
    const name = nicknameDraft.trim()
    if (validateNickname(name) !== 'ok') {
      nicknameErr = 'nickname_format'
      return
    }
    savingNickname = true
    nicknameErr = ''
    try {
      await updateNickname(name)
      nicknameDraft = name
      showToast($t('product.panel.nickname_saved'))
    } catch (e) {
      nicknameErr = e instanceof ProductError && e.code === 'nickname_sensitive'
        ? 'nickname_sensitive'
        : 'nickname_format'
    } finally {
      savingNickname = false
    }
  }

  async function signOut() {
    const ok = await confirmDialog($t('product.panel.logout_confirm'), {
      title: $t('product.panel.logout'), danger: true, confirmLabel: $t('product.panel.logout'),
    })
    if (!ok) return
    try {
      const result = await logout()
      settingsModalOpen.set(false)
      if (!result.revoked) showToast($t('product.panel.logout_not_revoked'), 'error')
    } catch {
      showToast($t('product.send_failed'), 'error')
    }
  }

  async function sendFeedback() {
    // OCTO-FORK: feedback rate limits are a server-owned retry window, surfaced
    // here without discarding the idempotency key or the user's form contents.
    if (feedbackSubmitting || feedbackCooldownSec > 0) return
    const title = feedbackTitle.trim()
    const content = feedbackContent.trim()
    feedbackError = ''
    feedbackReceipt = ''
    if (
      Array.from(title).length < 1 || Array.from(title).length > 120 ||
      Array.from(content).length < 1 || Array.from(content).length > 4000 ||
      Array.from(feedbackReproduction.trim()).length > 2000 ||
      Array.from(feedbackExpected.trim()).length > 2000 ||
      Array.from(feedbackContact.trim()).length > 200
    ) {
      feedbackError = $t('settings.help.feedback_length')
      return
    }
    feedbackSubmitting = true
    try {
      const payload = { category: feedbackCategory,
        title,
        content,
        reproduction: feedbackReproduction.trim(),
        expected: feedbackExpected.trim(),
        impact: feedbackImpact,
        contact: feedbackContact.trim(),
       }
      const fingerprint = JSON.stringify(payload)
      if (!feedbackKey || feedbackPayload !== fingerprint) { feedbackKey = crypto.randomUUID(); feedbackPayload = fingerprint }
      const receipt = await submitFeedback(payload, feedbackKey)
      feedbackReceipt = receipt.feedbackId
      feedbackTitle = ''
      feedbackContent = ''
      feedbackReproduction = ''
      feedbackExpected = ''
      feedbackContact = ''
      feedbackKey = ''
    } catch (e: any) {
      const retryAfterSec = e instanceof ProductError && e.code === 'rate_limited'
        ? e.retryAfterSec
        : null
      if (retryAfterSec !== null && retryAfterSec > 0) {
        startFeedbackCooldown(retryAfterSec)
        feedbackError = $t('settings.help.feedback_rate_limited').replace('{seconds}', String(feedbackCooldownSec))
      } else {
        feedbackError = e instanceof ProductError && Object.keys(e.fieldErrors).length > 0
          ? $t('settings.help.feedback_length')
          : $t('settings.help.feedback_failed')
      }
    } finally {
      feedbackSubmitting = false
    }
  }

  function startFeedbackCooldown(seconds: number) {
    const until = Date.now() + seconds * 1000
    if (feedbackCooldownTimer) clearInterval(feedbackCooldownTimer)
    const tick = () => {
      feedbackCooldownSec = Math.max(0, Math.ceil((until - Date.now()) / 1000))
      if (feedbackCooldownSec === 0 && feedbackCooldownTimer) {
        clearInterval(feedbackCooldownTimer)
        feedbackCooldownTimer = undefined
      }
    }
    tick()
    feedbackCooldownTimer = setInterval(tick, 250)
  }

  onDestroy(() => {
    if (feedbackCooldownTimer) clearInterval(feedbackCooldownTimer)
  })

  async function loadBox() {
    boxLoading = true
    boxError = false
    try {
      box = await getBox()
    } catch {
      box = null
      boxError = true
    } finally {
      boxLoading = false
    }
  }

  function openHelpCenter() {
    const url = brandLink('external', 'helpCenter')
    if (url) openUrl(url)
  }

  function formatBoxTime(value: string | undefined): string {
    if (!value) return '—'
    const parsed = new Date(value)
    if (Number.isNaN(parsed.getTime())) return '—'
    return parsed.toLocaleString(language === 'zh' ? 'zh-CN' : 'en-US')
  }

  function close() {
    settingsModalOpen.set(false)
  }

  // Re-runs the same blocking first-run panel a fresh install shows (App.svelte
  // gates <FirstRunSetup /> on this store) — lets a configured user redo the
  // model/browser setup + personalization chat without wiping any config first.
  function rerunFirstRun() {
    settingsModalOpen.set(false)
    onboardPhase.set('key_setup')
  }

  $effect(() => {
    const zoom = fontZoomMap[fontSize] ?? '1'
    ;(document.documentElement.style as any).zoom = zoom
    // Viewport units are not compensated for zoom — publish the factor so
    // vh/vw-sized boxes can divide themselves back onto the real viewport.
    document.documentElement.style.setProperty('--font-zoom', zoom)
    localStorage.setItem('octo.fontSize', fontSize)
  })

  const themeLabelToMode: Record<string, ThemeMode> = { Light: 'light', Dark: 'dark', System: 'system' }
  $effect(() => {
    setMode(themeLabelToMode[theme] ?? 'light')
  })

  $effect(() => {
    setLocale(language === 'zh' || language === 'zh-TW' ? 'zh' : 'en')
  })
</script>

{#if $settingsModalOpen}
<div class="backdrop" role="presentation" onclick={close}>
  <div
    class="modal"
    bind:this={modalEl}
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => { if (e.key === 'Escape') { e.preventDefault(); close() } }}
  >
    <div class="modal-header">
      <span class="modal-title">{$t('settings.title')}</span>
      <button class="modal-close" onclick={close} aria-label={$t('common.close')}>
        <iconify-icon icon="ant-design:close-outlined" width="14"></iconify-icon>
      </button>
    </div>

    <div class="modal-body">
      <div class="rail">
        {#each categories as c (c.key)}
          <button type="button" class="scat" class:on={cat === c.key} aria-current={cat === c.key ? 'page' : undefined} onclick={() => { cat = c.key; resetDataView() }}>
            <iconify-icon icon={c.icon} width="15"></iconify-icon>
            <span>{$t(c.label)}</span>
          </button>
        {/each}
      </div>

      <div class="pane">
        {#if loading}
          <div class="loading-state">{$t('settings.loading')}</div>
        {:else if cat === 'general'}
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.language')}</span>
              <span class="setd">{$t('settings.language_desc')}</span>
            </div>
            <select class="sinput" bind:value={language} onchange={() => saveLanguage(language)}>
              {#each langOptions as o}
                <option value={o.value}>{o.label}</option>
              {/each}
            </select>
          </div>
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.font_size')}</span>
              <span class="setd">{$t('settings.font_size_desc')}</span>
            </div>
            <Segment options={['Small', 'Medium', 'Large']} labels={{ Small: $t('settings.fs_small'), Medium: $t('settings.fs_medium'), Large: $t('settings.fs_large') }} bind:value={fontSize} />
          </div>
          <!-- Theme before Appearance: the pack is the bigger choice, and
               appearance reads as a modifier of it rather than the reverse. -->
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.pack')}</span>
              <span class="setd">{$t('settings.pack_desc')}</span>
            </div>
            <ThemePackPicker />
          </div>
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.theme')}</span>
              <span class="setd">{$t('settings.theme_desc')}</span>
            </div>
            <Segment options={['Light', 'Dark', 'System']} labels={{ Light: $t('settings.theme_light'), Dark: $t('settings.theme_dark'), System: $t('settings.theme_system') }} bind:value={theme} />
          </div>
          {#if $nativeShell}
            <div class="setrow">
              <div class="seti">
                <span class="setl">{$t('settings.autostart')}</span>
                <span class="setd">{$t('settings.autostart_desc')}</span>
              </div>
              <Switch checked={autostart} onchange={(v) => toggleAutostart(v)} />
            </div>
          {/if}
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.notifications')}</span>
              <span class="setd">{$t('settings.notifications_desc')}</span>
            </div>
            <Switch checked={$notificationsEnabled} onchange={(v) => setNotificationsEnabled(v)} />
          </div>

        {:else if cat === 'account'}
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('product.panel.nickname')}</span>
              <span class="setd">{$t('settings.account.nickname_desc')}</span>
              {#if nicknameErr}
                <span class="account-error">{$t(nicknameErr === 'nickname_sensitive' ? 'product.err_nickname_sensitive' : nicknameErr === 'save_failed' ? 'product.send_failed' : 'product.err_nickname')}</span>
              {/if}
            </div>
            <div class="account-edit">
              <input class="sinput" bind:value={nicknameDraft} maxlength="16" spellcheck="false" />
              <button class="btns" onclick={saveNickname} disabled={savingNickname || nicknameDraft.trim() === accountNickname}>
                {savingNickname ? $t('common.saving') : $t('common.save')}
              </button>
            </div>
          </div>
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('product.panel.phone')}</span>
              <span class="setd">{$t('settings.account.phone_desc')}</span>
            </div>
            <span class="setver">{accountPhone}</span>
          </div>
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('product.panel.license')}</span>
              <span class="setd">{$t('settings.account.license_desc')}</span>
            </div>
            <span class="setver">{accountLicense || '—'}</span>
          </div>
          <div class="danger-zone">
            <span class="danger-title">{$t('settings.account.security')}</span>
            <p>{$t('settings.account.signout_desc')}</p>
            <button class="btns danger" onclick={signOut}>{$t('product.panel.logout')}</button>
          </div>

        {:else if cat === 'wallet'}
          <WalletSection />
        {:else if cat === 'safety'}
          <SafetyPrivacySection />

        {:else if cat === 'box'}
          <section class="box-center" aria-label={$t('settings.box')}>
            <div class="center-hero">
              <div><h2>{$t('settings.box.title')}</h2><p>{$t('settings.box.desc')}</p></div>
              <button class="btns secondary" onclick={loadBox} disabled={boxLoading}>{boxLoading ? $t('settings.box.refreshing') : $t('settings.box.refresh')}</button>
            </div>
            {#if boxLoading && !box}
              <div class="center-empty">{$t('settings.box.loading')}</div>
            {:else if boxError}
              <div class="center-empty"><strong>{$t('settings.box.unavailable_title')}</strong><p>{$t('settings.box.unavailable_desc')}</p></div>
            {:else if box}
              <div class="box-summary">
                <div class="box-mark"><iconify-icon icon="lucide:box" width="28"></iconify-icon></div>
                <div class="box-title"><h3>{box.displayName}</h3><span class:online={box.state === 'online'} class="box-state">{$t(`settings.box.state.${box.state}`)}</span></div>
                <span class="box-id">{box.id}</span>
              </div>
              <div class="box-info-grid">
                <div class="info-card"><span>{$t('settings.box.bound_at')}</span><strong>{formatBoxTime(box.boundAt)}</strong></div>
                <div class="info-card"><span>{$t('settings.box.last_seen')}</span><strong>{formatBoxTime(box.lastSeenAt)}</strong></div>
                <div class="info-card"><span>{$t('settings.box.version')}</span><strong>{box.softwareVersion || '—'}</strong></div>
              </div>
              <h3 class="section-heading">{$t('settings.box.capabilities')}</h3>
              <div class="capability-grid">
                {#each [
                  ['privateModels', 'lucide:shield-check'], ['tools', 'lucide:wrench'], ['knowledge', 'lucide:book-open'], ['automation', 'lucide:workflow']
                ] as [key, icon]}
                  {@const capability = box.capabilities[key as keyof typeof box.capabilities]}
                  <div class="capability-card">
                    <iconify-icon icon={icon} width="18"></iconify-icon>
                    <div><strong>{$t(`settings.box.capability.${key}`)}</strong><span>{$t(`settings.box.capability_state.${capability.state}`).replace('{n}', String(capability.count ?? 0))}</span></div>
                  </div>
                {/each}
              </div>
              <p class="box-notice">{$t('settings.box.notice')}</p>
            {/if}
          </section>

        {:else if cat === 'help'}
          <section class="help-center" aria-label={$t('settings.help')}>
            <div class="center-hero"><div><h2>{$t('settings.help.title')}</h2><p>{$t('settings.help.subtitle')}</p></div>{#if brandLink('external', 'helpCenter')}<button class="btns secondary" onclick={openHelpCenter}>{$t('settings.help.full')}</button>{/if}</div>
            <div class="center-tabs" role="tablist"><button class:active={helpTab === 'guides'} onclick={() => helpTab = 'guides'} role="tab">{$t('settings.help.guides')}</button><button class:active={helpTab === 'feedback'} onclick={() => helpTab = 'feedback'} role="tab">{$t('settings.help.feedback_title')}</button></div>
            {#if helpTab === 'guides'}
              <div class="help-guide-grid">
                <details open><summary>{$t('settings.help.models_q')}</summary><p>{$t('settings.help.models_a')}</p></details>
                <details><summary>{$t('settings.help.personal_q')}</summary><p>{$t('settings.help.personal_a')}</p></details>
                <details><summary>{$t('settings.help.private_q')}</summary><p>{$t('settings.help.private_a')}</p></details>
                <details><summary>{$t('settings.help.box_q')}</summary><p>{$t('settings.help.box_a')}</p></details>
                <details><summary>{$t('settings.help.unavailable_q')}</summary><p>{$t('settings.help.unavailable_a')}</p></details>
              </div>
            {:else}
              <div class="feedback-form">
                <div class="feedback-intro"><h3>{$t('settings.help.feedback_title')}</h3><p>{$t('settings.help.feedback_notice')}</p></div>
                <label><span class="feedback-label">{$t('settings.help.feedback_title_label')}<small>{$t('settings.help.feedback_count').replace('{count}', String(Array.from(feedbackTitle).length)).replace('{limit}', '120')}</small></span><input class="sinput" bind:value={feedbackTitle} maxlength="120" placeholder={$t('settings.help.feedback_title_placeholder')} disabled={feedbackSubmitting} /></label>
                <label><span class="feedback-label">{$t('settings.help.feedback_content')}<small>{$t('settings.help.feedback_count').replace('{count}', String(Array.from(feedbackContent).length)).replace('{limit}', '4000')}</small></span><textarea class="sinput feedback-content" bind:value={feedbackContent} maxlength="4000" placeholder={$t('settings.help.feedback_content_placeholder')} disabled={feedbackSubmitting}></textarea></label>
                <div class="feedback-grid"><label><span>{$t('settings.help.feedback_category')}</span><select class="sinput" bind:value={feedbackCategory} disabled={feedbackSubmitting}><option value="bug">{$t('settings.help.feedback_bug')}</option><option value="suggestion">{$t('settings.help.feedback_suggestion')}</option><option value="other">{$t('settings.help.feedback_other')}</option></select></label><label><span>{$t('settings.help.feedback_impact')}</span><select class="sinput" bind:value={feedbackImpact} disabled={feedbackSubmitting}><option value="low">{$t('settings.help.feedback_impact_low')}</option><option value="normal">{$t('settings.help.feedback_impact_normal')}</option><option value="high">{$t('settings.help.feedback_impact_high')}</option></select></label></div>
                <details class="feedback-optional"><summary>{$t('settings.help.feedback_optional')}</summary><div class="feedback-extra">
                  <label><span>{$t('settings.help.feedback_reproduction')}</span><textarea class="sinput feedback-short" bind:value={feedbackReproduction} maxlength="2000" placeholder={$t('settings.help.feedback_reproduction_placeholder')} disabled={feedbackSubmitting}></textarea></label>
                  <label><span>{$t('settings.help.feedback_expected')}</span><textarea class="sinput feedback-short" bind:value={feedbackExpected} maxlength="2000" placeholder={$t('settings.help.feedback_expected_placeholder')} disabled={feedbackSubmitting}></textarea></label>
                  <label><span>{$t('settings.help.feedback_contact')}</span><input class="sinput" bind:value={feedbackContact} maxlength="200" placeholder={$t('settings.help.feedback_contact_placeholder')} disabled={feedbackSubmitting} /></label>
                </div></details>
                {#if feedbackError}<p class="feedback-error" role="alert">{feedbackError}</p>{/if}{#if feedbackReceipt}<p class="feedback-success" role="status">{$t('settings.help.feedback_sent').replace('{id}', feedbackReceipt)}</p>{/if}
                <div class="feedback-actions"><span>{$t('settings.help.feedback_required')}</span><button class="feedback-submit" onclick={sendFeedback} disabled={feedbackSubmitting || feedbackCooldownSec > 0 || !feedbackTitle.trim() || !feedbackContent.trim()}>{feedbackSubmitting ? $t('settings.help.feedback_submitting') : feedbackCooldownSec > 0 ? $t('settings.help.feedback_retry').replace('{seconds}', String(feedbackCooldownSec)) : $t('settings.help.feedback_submit')}</button></div>
              </div>
            {/if}
          </section>

        {:else if cat === 'endpoints'}
          <EndpointsSection />

        {:else if cat === 'agent'}
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.reasoning')}</span>
              <!-- OCTO-FORK: product gateway reasoning is selected per signed catalog model. -->
              <span class="setd">{$t($productState ? 'settings.reasoning_per_model' : 'settings.reasoning_desc')}</span>
            </div>
            {#if !$productState}
            <Segment
              options={['low', 'medium', 'high', 'xhigh', 'max']}
              labels={{
                low: $t('models.reasoning.low'), medium: $t('models.reasoning.medium'), high: $t('models.reasoning.high'),
                xhigh: $t('models.reasoning.xhigh'), max: $t('models.reasoning.max'),
              }}
              value={reasoningEffort}
              onchange={(v) => saveReasoningEffort(v)}
            />
            {/if}
          </div>
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.perm_mode')}</span>
              <span class="setd">{$t('settings.perm_mode_desc')}</span>
            </div>
            <Segment
              options={['interactive', 'auto', 'strict']}
              labels={{
                interactive: $t('settings.perm_mode.interactive'), auto: $t('settings.perm_mode.auto'), strict: $t('settings.perm_mode.strict'),
              }}
              value={permissionMode}
              onchange={(v) => savePermissionMode(v)}
            />
          </div>
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.show_reasoning')}</span>
              <span class="setd">{$t('settings.show_reasoning_desc')}</span>
            </div>
            <Switch checked={showReasoningVal} onchange={(v) => saveShowReasoning(v)} />
          </div>
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.coauthor')}</span>
              <span class="setd">{$t('settings.coauthor_desc')}</span>
            </div>
            <Switch checked={coauthorVal} onchange={(v) => saveCoauthor(v)} />
          </div>
          <!-- OCTO-FORK: 便携交付物不做更新 —— 上游 f7ba0793 这一行是"自动检查更新"的
               实时开关（PATCH /api/config/update_check），而 server 的 UpdateCheck 在本壳
               恒为 false（cmd/octo-desktop/main.go），且这层偏好自身的默认值是开
               （internal/config.Config.UpdateCheckEnabled 缺省返回 true）—— 也就是说
               它是一枚承诺了做不到之事的开关：用户按下去不会发生任何事（§3.9：回落必须
               说得出落到哪，不能静默撒谎）。换成与 about 分类同一形状的常驻占位，文案复用
               同一个键，将来接入不用改布局（需求 §5.1.2 第 13 条 / PQ12）。
               上游 updateCheckVal / saveUpdateCheck 的脚本半边留在原地不动（硬规则 3：
               宁可到不了，也不删）；钉子见 web/src/lib/updateEntry.test.ts 的
               LIVE_UPDATE_MARKERS。
               — see the desktop startup and lifecycle boundary §5（V-86） -->
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.update')}</span>
              <span class="setd">{$t('product.panel.soon')}</span>
            </div>
          </div>
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.workspace_dir')}</span>
              <span class="setd">{$t('settings.workspace_dir_desc')}</span>
            </div>
            <input
              class="sinput mono"
              type="text"
              placeholder={workspaceDirDefault}
              value={workspaceDir}
              onchange={(e) => saveWorkspaceDir(e.currentTarget.value)}
            />
          </div>

        {:else if cat === 'experimental'}
          <div class="setrow">
            <div class="seti">
              <span class="setl">{$t('settings.experimental.computer_use')}</span>
              <span class="setd">{$t('settings.experimental.computer_use_desc')}</span>
            </div>
            <Switch checked={computerUse} onchange={(v) => saveComputerUse(v)} />
          </div>
          <div class="setrow">
            <div class="seti">
              <span class="setd">{$t(computerHintKey)}</span>
            </div>
          </div>

        {:else if cat === 'mobile'}
          {#if tunnelPairing?.enabled && tunnelPairing.pair_url}
            <div class="mobile-pair">
              <QrCode text={tunnelPairing.pair_url} />
              <div class="mobile-info">
                <p class="mobile-scan">{$t('settings.mobile.scan')}</p>
                <div class="mobile-meta mono">
                  <div>{$t('settings.mobile.relay')}: {tunnelPairing.relay}</div>
                  <div>{$t('settings.mobile.tunnel_id')}: {tunnelPairing.tunnel_id}</div>
                </div>
                <button class="btns" onclick={copyPairURL}>{$t('settings.mobile.copy_url')}</button>
              </div>
            </div>
          {:else}
            <div class="mobile-disabled">{$t('settings.mobile.disabled')}</div>
          {/if}

        {:else if cat === 'data'}
          {#if dataSubView === 'none'}
            <!-- Memory leads: it's the row users come here to edit, while the
                 archive and the recycle bin are visited only when something
                 needs recovering. -->
            <div class="data-row">
              <div class="data-row-main">
                <iconify-icon icon="ant-design:user-outlined" width="15"></iconify-icon>
                <span class="setl">{$t('nav.memory')}</span>
              </div>
              <button class="link-btn" onclick={() => (dataSubView = 'memory')}>{$t('settings.data.manage')}</button>
            </div>
            <div class="data-row">
              <div class="data-row-main">
                <iconify-icon icon="lucide:archive" width="15"></iconify-icon>
                <span class="setl">{$t('settings.data.archived')}</span>
                <span class="data-count">{archivedSessions.length}</span>
              </div>
              <button class="link-btn" onclick={() => (dataSubView = 'archived')}>{$t('settings.data.manage')}</button>
            </div>
            <div class="data-row">
              <div class="data-row-main">
                <iconify-icon icon="ant-design:delete-outlined" width="15"></iconify-icon>
                <span class="setl">{$t('nav.file_recall')}</span>
              </div>
              <button class="link-btn" onclick={() => (dataSubView = 'trash')}>{$t('settings.data.manage')}</button>
            </div>
          {:else}
            <div class="data-subhead">
              <button class="back-btn" onclick={resetDataView}>
                <iconify-icon icon="ant-design:left-outlined" width="14"></iconify-icon>
              </button>
              <span class="data-subtitle">
                {dataSubView === 'archived' ? $t('settings.data.archived') : dataSubView === 'memory' ? $t('nav.memory') : $t('nav.file_recall')}
              </span>
            </div>
            {#if dataSubView === 'archived'}
              {#if archivedSessions.length === 0}
                <div class="data-empty">{$t('settings.data.archived_empty')}</div>
              {:else}
                {@const selCount = Object.keys(archiveSel).length}
                {@const visibleIds = archivedFiltered.map(s => s.id)}
                {@const allVisibleSelected = visibleIds.length > 0 && visibleIds.every(id => archiveSel[id])}
                <div class="archive-toolbar">
                  <div class="archive-search">
                    <iconify-icon icon="ant-design:search-outlined" width="14"></iconify-icon>
                    <input type="text" placeholder={$t('settings.data.search_placeholder')} bind:value={archiveSearch} />
                  </div>
                  <select class="sinput archive-project-select" bind:value={archiveProjectFilter}>
                    <option value="">{$t('settings.data.all_projects')}</option>
                    <option value="none">{$t('settings.data.no_project')}</option>
                    {#each archiveProjectOptions as g (g.id)}
                      <option value={g.id}>{g.name}</option>
                    {/each}
                  </select>
                </div>
                <div class="archive-selectbar">
                  <label class="archive-selectall">
                    <input type="checkbox" checked={allVisibleSelected} disabled={visibleIds.length === 0} onchange={toggleArchiveSelAll} />
                    <span>{$t('settings.data.select_all')}</span>
                  </label>
                  {#if selCount > 0}
                    <span class="archive-selcount">{$t('settings.data.n_selected').replace('{n}', String(selCount))}</span>
                    <button class="btns danger" onclick={deleteSelectedArchived}>{$t('settings.data.delete_selected')}</button>
                  {/if}
                </div>
                {#if archivedFiltered.length === 0}
                  <div class="data-empty">{$t('settings.data.archived_no_match')}</div>
                {:else}
                  {#each archivedGroups as bucket (bucket.group?.id ?? '')}
                    <div class="archive-bucket">
                      <div class="archive-bucket-head">
                        <iconify-icon icon="ant-design:folder-outlined" width="13"></iconify-icon>
                        <span>{bucket.group ? bucket.group.name : $t('settings.data.no_project')}</span>
                        <span class="archive-bucket-count">{bucket.items.length}</span>
                      </div>
                      <div class="archived-list">
                        {#each bucket.items as s (s.id)}
                          <div class="archived-row">
                            <input type="checkbox" checked={!!archiveSel[s.id]} onchange={() => toggleArchiveSel(s.id)} />
                            <div class="archived-info">
                              <span class="archived-name">{nameOf(s)}</span>
                              <span class="archived-meta mono">{s.id} · {ago((s as any).updated_at, $t, $clockTick)}</span>
                            </div>
                            <div class="archived-actions">
                              <button class="btns danger" onclick={() => deleteArchivedSession(s.id)}>{$t('common.delete')}</button>
                              <button class="btns" onclick={() => unarchiveSession(s.id)}>{$t('sidebar.uncollapse')}</button>
                            </div>
                          </div>
                        {/each}
                      </div>
                    </div>
                  {/each}
                {/if}
              {/if}
            {:else if dataSubView === 'memory'}
              <ProfileView embedded />
            {:else}
              <FileRecallView embedded />
            {/if}
          {/if}

        {:else if cat === 'about'}
          <div class="card2">
            <div class="setrow">
              <div class="seti">
                <span class="setl">{$t('common.version')}</span>
                <span class="setd">{$t('settings.about.version_desc')}</span>
              </div>
              <span class="setver mono">v{versionStr}</span>
            </div>
            <!-- OCTO-FORK: 便携交付物不做更新 —— 更新入口常驻但不可用（需求 §5.1.2 第 13 条；
                 PQ12 规定"界面上更新入口常驻但不可用，点击给「即将支持」，这样将来接入不用改
                 布局"）。上游这个 checkUpdate 按钮会去查最新发布，而本壳把 server 的
                 UpdateCheck 关掉之后它只会回答"已是最新版本" —— 那是假话，server 根本没查
                 （§3.9：回落必须说得出落到哪，不能静默撒一个看不出来的谎）。所以整个活入口
                 换成占位，文案与个人中心那一处共用同一个键。
                 设置的关于分类已承载同一意图，此处是那笔账的收尾。
                 上游的 checkUpdate/loadVersion 脚本留在原地不动（硬规则 3：宁可到不了，也不删）。
                 — see the desktop startup and lifecycle boundary §5（V-86） -->
            <div class="setrow">
              <div class="seti">
                <span class="setl">{$t('settings.update')}</span>
                <span class="setd">{$t('product.panel.soon')}</span>
              </div>
            </div>
            <div class="setrow">
              <div class="seti">
                <span class="setl">{$t('settings.about.firstrun')}</span>
                <span class="setd">{$t('settings.about.firstrun_desc')}</span>
              </div>
              <button class="btns" onclick={rerunFirstRun}>{$t('settings.about.firstrun_btn')}</button>
            </div>
            <div class="setrow">
              <div class="seti">
                <span class="setl">{$t('settings.about.license')}</span>
                <span class="setd">{$t('settings.about.license_desc')}</span>
              </div>
              <button class="link-btn" onclick={() => openUrl(LICENSE_URL)}>{$t('settings.about.license_view')}</button>
            </div>
          </div>
          <div class="about-footer">
            {$t('settings.about.footer').replace('{tagline}', $t('nav.workbench')).replace('{year}', String(new Date().getFullYear()))}
          </div>
        {/if}
      </div>
    </div>
  </div>
</div>
{/if}

<style>
.backdrop {
  position: fixed; inset: 0; z-index: 1000; background: var(--scrim);
  display: flex; align-items: center; justify-content: center; padding: 24px;
}
.modal {
  /* Fixed height (not max-height) so switching between categories with very
     different content lengths (关于 vs 端点) never resizes the modal itself
     — each pane scrolls internally instead. */
  width: 100%; max-width: 900px; height: calc(78vh / var(--font-zoom));
  background: var(--bg-container); border: 1px solid var(--border); border-radius: var(--radius-card);
  box-shadow: 0 24px 48px rgba(15,23,42,0.18);
  display: flex; flex-direction: column; overflow: hidden;
  animation: octo-fadein 0.16s ease;
}
.modal:focus { outline: none; }
.modal-header {
  display: flex; align-items: center; justify-content: space-between; flex: 0 0 auto;
  padding: 16px 20px; border-bottom: 1px solid var(--border);
}
.modal-title { font-size: 16px; font-weight: 700; letter-spacing: -0.01em; color: var(--text-heading); }
.modal-close {
  width: 28px; height: 28px; border: none; background: transparent; border-radius: 7px;
  display: flex; align-items: center; justify-content: center; cursor: pointer; color: var(--text-secondary);
}
.modal-close:hover { background: var(--hover-neutral); color: var(--text); }
.modal-body { flex: 1; min-height: 0; display: flex; }

/* ── category rail ─────────────────────────────────────────────────────────── */
.rail {
  width: 190px; flex: 0 0 190px; border-right: 1px solid var(--border);
  background: var(--bg-layout); padding: 10px; display: flex; flex-direction: column; gap: 2px;
  overflow-y: auto;
}
.scat {
  display: flex; align-items: center; gap: 9px; padding: 7px 10px;
  border-radius: 7px; cursor: pointer; color: var(--text-tertiary);
  border: none; background: transparent; text-align: left; font: inherit;
}
.scat:focus-visible { outline: 2px solid var(--blue-6); outline-offset: -2px; }
.scat span { font-size: 13px; color: var(--text-secondary); }
.scat:hover { background: var(--hover-neutral); }
.scat.on { background: var(--active-blue-bg); }
.scat.on span { color: var(--blue-6); font-weight: 600; }
.scat.on { color: var(--blue-6); }

/* ── content pane ──────────────────────────────────────────────────────────── */
.pane { flex: 1; min-width: 0; overflow-y: auto; padding: 20px 24px; display: flex; flex-direction: column; gap: 0; }
.loading-state { padding: 40px; text-align: center; color: var(--text-tertiary); font-size: 14px; }

.setrow {
  display: flex; align-items: center; justify-content: space-between;
  gap: 20px; padding: 14px 2px; border-bottom: 1px solid var(--border-secondary);
}
.setrow:last-child { border-bottom: none; }
/* Rows nested in a card (About) need the card's own inset — the flat rows
   above sit directly in .pane, which already provides that inset itself. */
.card2 .setrow { padding: 16px 20px; }
.seti { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.setl { font-size: 13px; color: var(--text); }
.setd { font-size: 12px; color: var(--text-secondary); margin-top: 2px; line-height: 1.45; max-width: 42ch; }
.setver { font-size: 13px; color: var(--text-tertiary); flex: 0 0 auto; }
.link-btn {
  border: none; background: transparent; color: var(--blue-6); font-size: 13px;
  font-weight: 500; cursor: pointer; font-family: inherit; padding: 0; flex: 0 0 auto;
}
.link-btn:hover { text-decoration: underline; }
.about-footer { padding: 28px 2px 4px; text-align: center; font-size: 12px; color: var(--text-tertiary); }
.sinput {
  width: 220px; flex: 0 0 auto; height: 32px; padding: 0 10px;
  border: 1px solid var(--border); border-radius: 8px; font-size: 13px;
  color: var(--text); font-family: inherit; background: var(--bg-container); outline: none;
}
select.sinput { cursor: pointer; }
.sinput:focus { border-color: var(--blue-6); box-shadow: 0 0 0 2px var(--focus-ring); }
.account-edit { display: flex; align-items: center; gap: 8px; flex: 0 0 auto; }
.account-edit .sinput { width: 150px; }
.account-error { color: var(--error); font-size: 12px; margin-top: 3px; }
.danger-zone { margin-top: 24px; padding: 16px; border: 1px solid var(--error-border, var(--border)); border-radius: 9px; display: flex; flex-direction: column; align-items: flex-start; gap: 10px; }
.danger-title { color: var(--error); font-size: 13px; font-weight: 600; }
.danger-zone p { margin: 0; color: var(--text-secondary); font-size: 12px; line-height: 1.5; }

/* ── buttons ───────────────────────────────────────────────────────────────── */
.btns {
  height: 30px; padding: 0 12px; border: 1px solid var(--border); background: var(--bg-container);
  border-radius: 8px; font-size: 13px; font-weight: 500; color: var(--text); cursor: pointer;
  font-family: inherit; display: flex; align-items: center; gap: 6px;
}
.btns:hover:not(:disabled) { background: var(--hover-neutral); border-color: var(--text-quaternary); }
.btns:disabled { opacity: 0.5; cursor: not-allowed; }

/* ── about card ────────────────────────────────────────────────────────────── */
.card2 { background: var(--bg-container); border: 1px solid var(--border); border-radius: var(--radius-card); overflow: hidden; }

/* ── mobile ─────────────────────────────────────────────────────────────────── */
.mobile-pair { display: flex; gap: 20px; padding: 6px 2px; align-items: flex-start; flex-wrap: wrap; }
.mobile-info { display: flex; flex-direction: column; gap: 12px; min-width: 0; flex: 1; }
.mobile-scan { margin: 0; font-size: 14px; color: var(--text); }
.mobile-meta { font-size: 12px; color: var(--text-tertiary); display: flex; flex-direction: column; gap: 4px; word-break: break-all; }
.mobile-meta > div { min-width: 0; }
.mobile-info .btns { align-self: flex-start; }
.mobile-disabled { padding: 28px 16px; text-align: center; font-size: 13px; color: var(--text-tertiary); }
.mono { font-family: var(--font-mono); }

/* ── data management ─────────────────────────────────────────────────────── */
.data-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: 16px; padding: 12px 10px; margin: 0 -10px; border-radius: 8px;
}
.data-row:hover { background: var(--hover-neutral); }
.data-row-main { display: flex; align-items: center; gap: 9px; min-width: 0; color: var(--text-tertiary); }
.data-count { font-size: 12px; color: var(--text-tertiary); }
.data-subhead { display: flex; align-items: center; gap: 8px; padding: 2px 0 14px; }
.back-btn {
  width: 26px; height: 26px; border: none; background: transparent; border-radius: 7px;
  display: flex; align-items: center; justify-content: center; cursor: pointer; color: var(--text-secondary);
}
.back-btn:hover { background: var(--hover-neutral); color: var(--text); }
.data-subtitle { font-size: 14px; font-weight: 600; color: var(--text-heading); }
.data-empty { padding: 32px 4px; text-align: center; font-size: 13px; color: var(--text-tertiary); }

/* ── archived tasks: search/filter + select-all bar + project buckets ────── */
.archive-toolbar { display: flex; gap: 10px; padding-bottom: 12px; }
.archive-search {
  flex: 1; min-width: 0; display: flex; align-items: center; gap: 7px;
  height: 32px; padding: 0 10px; border: 1px solid var(--border); border-radius: 8px;
  background: var(--bg-container); color: var(--text-tertiary);
}
.archive-search input {
  flex: 1; min-width: 0; border: none; outline: none; background: transparent;
  font-size: 13px; color: var(--text); font-family: inherit;
}
.archive-project-select { width: 160px; flex: 0 0 auto; }
.archive-selectbar {
  display: flex; align-items: center; gap: 10px; padding: 4px 2px 10px;
  border-bottom: 1px solid var(--border-secondary); margin-bottom: 4px;
}
.archive-selectall {
  display: flex; align-items: center; gap: 7px; font-size: 12px; color: var(--text-secondary);
  cursor: pointer; user-select: none;
}
.archive-selectall input { cursor: pointer; }
.archive-selcount { font-size: 12px; color: var(--text-tertiary); flex: 1; }
.archive-bucket { padding: 10px 0 4px; }
.archive-bucket-head {
  display: flex; align-items: center; gap: 7px; padding: 0 2px 6px;
  font-size: 12px; font-weight: 600; color: var(--text-tertiary);
}
.archive-bucket-count { color: var(--text-quaternary); font-weight: 400; }
.archived-list { display: flex; flex-direction: column; }
.archived-row {
  display: flex; align-items: center; gap: 12px; padding: 10px 2px;
  border-bottom: 1px solid var(--border-secondary);
}
.archived-row:last-child { border-bottom: none; }
.archived-row > input[type="checkbox"] { flex: 0 0 auto; cursor: pointer; }
.archived-info { flex: 1; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.archived-name { font-size: 13px; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.archived-meta { font-size: 11px; color: var(--text-tertiary); }
.archived-actions { display: flex; align-items: center; gap: 8px; flex: 0 0 auto; }
  .btns.danger { color: var(--error); }
  .btns.danger:hover:not(:disabled) { background: var(--error-bg); border-color: var(--error-border); }
  .btns.secondary { background: var(--active-blue-bg); border-color: transparent; color: var(--blue-6); }
  .center-hero { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding-bottom: 18px; }
  .center-hero h2 { margin: 0; color: var(--text-heading); font-size: 19px; letter-spacing: -.02em; }
  .center-hero p { margin: 6px 0 0; max-width: 48ch; color: var(--text-secondary); font-size: 13px; line-height: 1.55; }
  .center-empty { padding: 42px 18px; border: 1px dashed var(--border); border-radius: 12px; text-align: center; color: var(--text-secondary); }
  .center-empty strong { display: block; color: var(--text); margin-bottom: 6px; }
  .center-empty p { margin: 0; font-size: 13px; }
  .box-summary { display: flex; align-items: center; gap: 12px; padding: 16px; border: 1px solid var(--border); border-radius: 12px; background: linear-gradient(135deg, var(--active-blue-bg), var(--bg-container)); }
  .box-mark { width: 46px; height: 46px; border-radius: 12px; color: var(--blue-6); background: var(--bg-container); display: grid; place-items: center; border: 1px solid var(--border); }
  .box-title { min-width: 0; flex: 1; }
  .box-title h3 { margin: 0 0 4px; font-size: 14px; color: var(--text); }
  .box-state { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; color: var(--text-secondary); }
  .box-state.online { color: var(--success); }
  .box-id { max-width: 130px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-tertiary); font: 11px var(--font-mono); }
  .box-info-grid, .capability-grid, .feedback-grid, .help-guide-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .box-info-grid { margin-top: 10px; grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .info-card, .capability-card { padding: 12px; border: 1px solid var(--border-secondary); border-radius: 10px; background: var(--bg-container); }
  .info-card { display: flex; flex-direction: column; gap: 5px; min-width: 0; }
  .info-card span { color: var(--text-tertiary); font-size: 11px; }
  .info-card strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); font-size: 12px; font-weight: 500; }
  .section-heading { margin: 22px 0 10px; color: var(--text); font-size: 14px; }
  .capability-card { display: flex; align-items: flex-start; gap: 9px; color: var(--blue-6); }
  .capability-card div { display: flex; flex-direction: column; gap: 3px; color: var(--text); }
  .capability-card strong { font-size: 12px; }
  .capability-card span { color: var(--text-tertiary); font-size: 11px; }
  .box-notice { margin: 14px 0 0; color: var(--text-tertiary); font-size: 12px; line-height: 1.5; }
  .center-tabs { display: flex; gap: 4px; padding: 4px; margin-bottom: 16px; background: var(--bg-layout); border-radius: 9px; width: fit-content; }
  .center-tabs button { border: none; background: transparent; color: var(--text-secondary); border-radius: 6px; padding: 6px 12px; font: 13px inherit; cursor: pointer; }
  .center-tabs button.active { background: var(--bg-container); color: var(--text); box-shadow: 0 1px 2px rgba(0,0,0,.08); }
  .help-guide-grid details { padding: 14px; border: 1px solid var(--border-secondary); border-radius: 10px; }
  .help-guide-grid summary { cursor: pointer; color: var(--text); font-size: 13px; font-weight: 600; }
  .help-guide-grid p { margin: 9px 0 0; color: var(--text-secondary); font-size: 12px; line-height: 1.55; }
  .feedback-form { display: flex; flex-direction: column; gap: 16px; padding: 4px 0; }
  .feedback-intro h3 { margin: 0; color: var(--text); font-size: 15px; }
  .feedback-intro p { margin: 5px 0 0; color: var(--text-secondary); font-size: 12px; line-height: 1.5; }
  .feedback-form label { display: flex; flex-direction: column; gap: 6px; color: var(--text-secondary); font-size: 12px; }
  .feedback-form .sinput { width: 100%; box-sizing: border-box; }
  .feedback-content { min-height: 144px; padding: 10px 12px; resize: vertical; line-height:1.6; }
  .feedback-short { height: 76px; padding: 9px 10px; resize: vertical; }
  .feedback-form .feedback-grid {max-width:360px;gap:12px;}
  .feedback-label {display:flex;justify-content:space-between;align-items:center;gap:10px;}
  .feedback-label small {font-size:11px;font-weight:400;color:var(--text-tertiary);}
  .feedback-optional {border-top:1px solid var(--border);border-bottom:1px solid var(--border);padding:12px 0;}
  .feedback-optional summary {cursor:pointer;color:var(--text-secondary);font-size:12px;}
  .feedback-extra {display:flex;flex-direction:column;gap:12px;padding-top:14px;}
  .feedback-actions {display:flex;align-items:center;justify-content:space-between;gap:12px;}
  .feedback-actions>span {font-size:11px;color:var(--text-tertiary);}
  .feedback-submit {flex-shrink:0;border:1px solid var(--blue-6);background:var(--blue-6);color:var(--on-accent);border-radius:7px;padding:9px 20px;font-size:12px;font-weight:500;cursor:pointer;}
  .feedback-submit:hover:not(:disabled) {filter:brightness(1.08);}.feedback-submit:disabled {opacity:.5;cursor:default;}
  .feedback-error,.feedback-success {margin:0;padding:10px 12px;border-radius:8px;font-size:12px;line-height:1.6;overflow-wrap:anywhere;background:var(--bg-layout);}
  .feedback-error {color:var(--error);}.feedback-success {color:var(--success);}
  @media (max-width: 640px) { .box-info-grid, .capability-grid, .feedback-grid, .help-guide-grid { grid-template-columns: 1fr; } .center-hero { flex-direction: column; } }
</style>
