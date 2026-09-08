<script lang="ts">
  import OctoLogo from './layout/OctoLogo.svelte'

  // OCTO-FORK: P4 placeholder brand mark. The real BrandMark (品牌 PR4) renders
  // brandAsset('mark') — branding/assets/logo-mark.svg, which is not shipped
  // yet — so this stands in with the upstream octopus mark rather than leaving
  // the login page empty. The {size}/{class} props are the FINAL component API
  // (接口已定死), so when the asset lands this file is deleted and the real
  // BrandMark drops in with no rework to the login page.
  // See dev-docs-usdable/需求/2260906/技术方案/P4-拦截页.md §5.
  interface Props {
    size?: number | string
    class?: string
  }
  let { size = 40, class: className = '' }: Props = $props()
</script>

<OctoLogo {size} class={className} />
