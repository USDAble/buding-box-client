// Which global window shortcut a keydown means. Pure logic so the modifier
// interpretation is testable on its own, mirroring composerKeys.ts.

export interface GlobalKeyEvent {
  key: string
  metaKey?: boolean
  ctrlKey?: boolean
  altKey?: boolean
  shiftKey?: boolean
}

export interface GlobalKeyOpts {
  // Whether the page runs in the native desktop shell. The Cmd/Ctrl+N
  // "new session" bind is desktop-only: under a plain browser Cmd+N is a
  // reserved browser shortcut the page never sees. Pass isDesktopShell.
  shell: boolean
}

// 'palette' toggles the command palette (⌘K); 'new-session' starts a new
// session (⌘N, desktop shell only); null means the caller should keep
// handling the keypress normally.
export function globalKeyIntent(e: GlobalKeyEvent, opts: GlobalKeyOpts): 'palette' | 'new-session' | null {
  const mod = (e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey
  if (!mod) return null
  const k = e.key.toLowerCase()
  if (k === 'k') return 'palette'
  if (k === 'n' && opts.shell) return 'new-session'
  return null
}

// Plain-Escape dismissal (P5 account panel). Kept OUT of globalKeyIntent on
// purpose: that one only answers MODIFIED shortcuts, while Esc carries no
// modifier and must stay claimable by the true modals that own their own Esc
// (command palette, settings, confirms) — the account panel is non-modal and
// must not steal their key. Callers decide when an 'overlay' answer applies.
//
// OCTO-FORK: P5 account panel — see
// dev-docs-usdable/需求/2260906/技术方案/P5-个人中心.md §4.2.
export function dismissIntent(e: GlobalKeyEvent): 'overlay' | null {
  const mod = e.metaKey || e.ctrlKey || e.altKey || e.shiftKey
  if (mod) return null
  return e.key === 'Escape' ? 'overlay' : null
}
