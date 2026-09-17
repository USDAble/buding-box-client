import './app.css'
import App from './App.svelte'
import { mount } from 'svelte'
import { initIcons } from './lib/icons'
import { initTheme } from './lib/theme'
import { initFramelessDrag } from './lib/framelessDrag'
import { installArtifactThemeRefresh } from './lib/artifacts'
import { locale } from './lib/i18n'
import { brandName } from './lib/brand'
import { applyTitlebarLift } from './lib/nativeWindow'

// OCTO-FORK: a comment-only deviation recording where the development stand-in
// used to be installed, and the two rules learned from it — see
// dev-docs-usdable/需求/20260911/开发计划.md PR-3 and 需求基线 §5.6 G5/V-47.
//
// The stand-in that used to be installed here is gone (PR-3, 2026-09-13):
// /api/* now always reaches the local Go service, in dev builds too (vite
// proxies it, see vite.config.ts's server.proxy). It answered every /api path
// with 200 - including paths no server had registered - which is how V-24 and
// V-46 stayed invisible while the UI looked healthy.
//
// If one is ever reintroduced, the import must be DYNAMIC and inside the
// `import.meta.env.DEV` branch. A static import does not get tree-shaken: the
// module's top-level statements land in the shipped bundle and run at every
// start (V-47, verified by a control build).

// Register the <iconify-icon> element and its bundled icon data before first
// paint — and shut the door on Iconify's API, which the CDN build used to call
// per icon. Nothing about the UI reaches a third party any more.
initIcons()

// Apply the persisted theme before first paint so there's no light-mode flash.
initTheme()

// Desktop shell on macOS: pin the titlebar rows' axis to the traffic lights
// before first paint — the inset and padding would otherwise wait on
// /api/version and flash the rows crammed under the lights at startup.
// No-op elsewhere.
applyTitlebarLift()

// Rebuild baked-theme artifact previews whenever the resolved theme changes.
installArtifactThemeRefresh()

// Desktop shell on Windows/Linux: window drag + edge resize (no-op elsewhere).
initFramelessDrag()

// The window/tab title is product copy, so it is rendered from
// branding/brand.json instead of being typed into index.html (硬规则 2, guarded
// by scripts/brand-guard.mjs). Following the locale store means switching the UI
// language retitles the window too, and the store's own default is what shows
// before a preference loads — nothing here reads navigator.language
// (see lib/localeSource.test.ts).
locale.subscribe(($locale) => {
  document.title = brandName($locale)
})

const app = mount(App, { target: document.getElementById('app')! })

export default app
