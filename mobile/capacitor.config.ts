import type { CapacitorConfig } from '@capacitor/cli'
import { brandName } from './src/brand'

// webDir is the bundled octo web frontend, produced by `npm run bundle-web`,
// which copies internal/server/webdist into www/. It is a build artifact (kept
// out of git). The app serves it from capacitor://localhost so the frontend
// keeps its same-origin assumptions; the local shim (src/shim.ts) tunnels its
// /api + /ws out to the remote octo serve.
const config: CapacitorConfig = {
  appId: 'dev.octo.mobile',
  // OCTO-FORK: 移动端壳的品牌插值 — see the fork engineering norms §3.2
  // appName lands in the generated native projects' Info.plist / strings.xml:
  // OS metadata that is never re-rendered when the UI language changes, so it
  // takes the fixed English name — the same choice brand.json's
  // display.windows.productName makes for the Windows installer.
  appName: brandName('en-US'),
  webDir: 'www',
}

export default config
