import './app.css'
import App from './App.svelte'
import { mount } from 'svelte'
import { initTheme } from './lib/theme'
import { initFramelessDrag } from './lib/framelessDrag'
import { installArtifactThemeRefresh } from './lib/artifacts'

// TEMPORARY: the P1-P13 screens call a product backend that is not on this
// branch, so their interactions would 404 and the flow could not be walked at
// all. This installs a local stand-in. The real backend replaces it — when it
// lands, delete src/dev/devBackend.ts and this block (nothing else references
// it). See dev-docs-usdable/需求/2260906/技术方案/开发期假后端说明.md.
//
// Scope: development only.
//
// WHY A DYNAMIC IMPORT (V-47). This used to be a static `import { … }` plus
// `if (import.meta.env.DEV) installDevBackend()`, on the belief that the dead
// branch let Rollup "tree-shake the module out of the bundle". It does not,
// and the claim was never checked against a built artifact: a module that is
// imported at all has its **top-level statements evaluated**, and this module
// has them (`const realFetch = globalThis.fetch.bind(globalThis)`, a `MODES`
// array built by calling a helper, a demo session pushed into an array). The
// production bundle therefore contained the stand-in's fixture data and ran
// eight helper calls, one demo-session build and a `bind` on every start —
// unused results, but executed. A dynamic import inside the dead branch is
// what actually keeps the module out of the production graph.
//
// Verified by a control build (V-47): with the static import, `grep -rl
// 演示会话 webdist/assets/` hits; with this dynamic one it does not. Clean
// webdist first, or emptyOutDir:false leaves the previous build's chunks
// addressable and the answer becomes meaningless.
//
// The `await` is deliberate: installing after mount would let the first state
// read reach the real (usually absent) dev server, which looks like a bug in
// the screens rather than a race in the stand-in.
if (import.meta.env.DEV) {
  const { installDevBackend } = await import('./dev/devBackend')
  installDevBackend()
}

// Apply the persisted theme before first paint so there's no light-mode flash.
initTheme()

// Rebuild baked-theme artifact previews whenever the resolved theme changes.
installArtifactThemeRefresh()

// Desktop shell on Windows/Linux: window drag + edge resize (no-op elsewhere).
initFramelessDrag()

const app = mount(App, { target: document.getElementById('app')! })

export default app
