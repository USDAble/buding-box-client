<script lang="ts">
  // Brand master mark — renders branding/logo-mark.png through
  // brandAsset('mark'), the single coloured logo the designer delivers.
  // Every product surface (sidebar, chat landing, first-run, blocked page)
  // shares this component so the UI logo and the packaged icons stay in sync.
  //
  // OCTO-FORK: upstream's OctoLogo drew the octopus in inline SVG; this fork
  // renders the Pudding Box master image instead (品牌升级方案 §5.7 — the file
  // is renamed to BrandMark.svelte by brand PR4; the import sites stay the
  // same, so until then the old name is kept to avoid a 5-site churn).
  import { brandAsset } from '../../lib/brand'

  interface Props {
    size?: number | string
    class?: string
  }
  let { size = 26, class: className = '' }: Props = $props()

  // brandAsset('mark') is a consumer-relative path ('assets/logo-mark.png'
  // under web/public), so prefix with the static root.
  const src = `/${brandAsset('mark')}`
</script>

<img src={src} class={className} width={size} height={size} alt="" draggable="false" />
