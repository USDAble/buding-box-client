import './app.css'
import App from './App.svelte'
import { mount } from 'svelte'
import { initTheme } from './lib/theme'
import { initFramelessDrag } from './lib/framelessDrag'
import { installArtifactThemeRefresh } from './lib/artifacts'

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

// Apply the persisted theme before first paint so there's no light-mode flash.
initTheme()

// Rebuild baked-theme artifact previews whenever the resolved theme changes.
installArtifactThemeRefresh()

// Desktop shell on Windows/Linux: window drag + edge resize (no-op elsewhere).
initFramelessDrag()

const app = mount(App, { target: document.getElementById('app')! })

export default app
