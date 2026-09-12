import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../internal/server/webdist',
    // Must stay false: webdist is gitignored except a tracked .gitkeep (which
    // keeps go:embed satisfied on fresh clones). Emptying the dir deletes that
    // .gitkeep and leaves the git tree dirty — goreleaser refuses to release
    // from a dirty tree (broke the v1.12.22 tag build). Stale hashed assets
    // left behind are inert: index.html only references the fresh ones.
    //
    // The inert half of that reasoning only covers hashed names. A fixed-name
    // file stays addressable and keeps getting embedded, and nothing here could
    // delete it — see V-34, where two deleted favicon.svg files stayed in the
    // binary. `make web-build` therefore runs scripts/webdist-clean.mjs first,
    // which empties the dir except .gitkeep. This setting still must not become
    // true: that script is what preserves the sentinel, and vite would not.
    emptyOutDir: false,
    // The UI ships as one embedded bundle served from localhost; code-splitting buys nothing here.
    chunkSizeWarningLimit: 1000,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8088',
      '/ws':  { target: 'ws://localhost:8088', ws: true },
    },
  },
})
