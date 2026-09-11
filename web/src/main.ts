import './app.css'
import App from './App.svelte'
import { mount } from 'svelte'
import { initTheme } from './lib/theme'
import { initFramelessDrag } from './lib/framelessDrag'
import { installArtifactThemeRefresh } from './lib/artifacts'
import { installDevBackend } from './dev/devBackend'

// TEMPORARY: the P1-P13 screens call a product backend that is not on this
// branch, so their interactions would 404 and the flow could not be walked at
// all. This installs a local stand-in. The real backend replaces it — when it
// lands, delete src/dev/devBackend.ts and these two lines (nothing else
// references them). See
// dev-docs-usdable/需求/2260906/技术方案/开发期假后端说明.md.
//
// Scope: development only. A production build sets import.meta.env.DEV to
// false, which drops the call and tree-shakes the module out of the bundle.
if (import.meta.env.DEV) installDevBackend()

// Apply the persisted theme before first paint so there's no light-mode flash.
initTheme()

// Rebuild baked-theme artifact previews whenever the resolved theme changes.
installArtifactThemeRefresh()

// Desktop shell on Windows/Linux: window drag + edge resize (no-op elsewhere).
initFramelessDrag()

const app = mount(App, { target: document.getElementById('app')! })

export default app
