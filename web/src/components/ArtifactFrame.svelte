<script lang="ts">
  // The preview frame for a text artifact, in its three shapes:
  //
  //   - an HTML artifact with a grant renders by src from the artifact origin
  //     (lib/artifacts.ts header, internal/server/artifact_origin.go);
  //   - an HTML artifact this browser cannot reach that origin from shows the
  //     local-only notice — the code view beside it still works;
  //   - a Markdown preview renders by srcdoc, as before.
  import type { Artifact } from '../lib/types'
  // OCTO-FORK: fixed-width posters must fit the panel while keeping origin isolation.
  import { fitArtifactLayout, readArtifactLayout, type ArtifactLayout } from '../lib/artifact-layout'
  import { t } from '../lib/i18n'
  import { ARTIFACT_SANDBOX, ARTIFACT_ORIGIN_SANDBOX, probeArtifactOrigin, themeRev } from '../lib/artifacts'

  let { artifact }: { artifact: Artifact } = $props()
  let viewport = $state<HTMLDivElement | null>(null)
  let frame = $state<HTMLIFrameElement | null>(null)
  let width = $state(0)
  let height = $state(0)
  let layout = $state<ArtifactLayout | null>(null)
  const fitted = $derived(fitArtifactLayout(width, height, layout))

  // The banner the origin bakes into a gated page carries theme colours, and
  // the origin has no other way to learn the app's theme than its URL. A
  // theme switch changes src, which reloads the frame — the same thing the
  // Markdown path does by dropping its preview and rebuilding.
  const theme = $derived.by(() => {
    void $themeRev
    return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light'
  })
  // `v` is the observation count: a re-write of the same path yields a
  // different URL, so the frame reloads even though the origin did not change.
  const src = $derived(artifact.originURL ? `${artifact.originURL}?theme=${theme}&v=${artifact.rev ?? 0}` : '')

  // OCTO-FORK: a fresh container size or document starts at its real viewport,
  // allowing responsive pages to reflow before measuring any fixed overflow.
  $effect(() => { void src; layout = null })
  function requestLayout() {
    if (frame && src) frame.contentWindow?.postMessage({ type: 'octo:artifact-measure' }, new URL(src).origin)
  }
  $effect(() => { void width; void height; requestLayout() })
  $effect(() => {
    const element = viewport
    if (!element) return
    const observer = new ResizeObserver(([entry]) => {
      width = entry.contentRect.width
      height = entry.contentRect.height
      layout = null
    })
    observer.observe(element)
    return () => observer.disconnect()
  })
  $effect(() => {
    if (!frame || !src) return
    const element = frame
    const origin = new URL(src).origin
    function onLayout(event: MessageEvent) {
      const next = readArtifactLayout(event, element, origin, fitted.width)
      if (next) layout = next
    }
    window.addEventListener('message', onLayout)
    return () => window.removeEventListener('message', onLayout)
  })

  // Only the browser knows whether the origin's hostname resolves here; the
  // probe (once per URL) flips the entry to the local-only notice when it
  // does not. See probeArtifactOrigin.
  $effect(() => {
    probeArtifactOrigin(artifact)
  })
</script>

{#if artifact.originURL}
  <!-- OCTO-FORK: scaling the frame preserves pointer coordinates and original file bytes. -->
  <div class="viewport" bind:this={viewport}>
    <iframe bind:this={frame} {src} onload={requestLayout} style:width={width > 0 ? `${fitted.width}px` : '100%'} style:height={height > 0 ? `${fitted.height}px` : '100%'} style:transform={`scale(${fitted.scale})`} sandbox={ARTIFACT_ORIGIN_SANDBOX} allow="fullscreen; clipboard-write" title={artifact.name}></iframe>
  </div>
{:else if artifact.originUnavailable}
  <div class="local-only">
    <iconify-icon icon="ant-design:desktop-outlined" width="28"></iconify-icon>
    <span>{$t('artifacts.local_only')}</span>
  </div>
{:else}
  <iframe srcdoc={artifact.preview} sandbox={ARTIFACT_SANDBOX} allow="fullscreen; clipboard-write" title={artifact.name}></iframe>
{/if}

<style>
iframe { border: 0; width: 100%; height: 100%; display: block; }
/* OCTO-FORK: center the fitted canvas instead of clipping its right-hand content. */
.viewport { width: 100%; height: 100%; overflow: hidden; position: relative; }
.viewport iframe { position: absolute; left: 50%; translate: -50% 0; transform-origin: top center; }
.local-only {
  width: 100%; height: 100%; box-sizing: border-box; padding: 16px;
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px;
  text-align: center; color: var(--text-tertiary); font-size: 13px; background: var(--bg-layout);
}
.local-only span { max-width: 85%; }
</style>
