<script lang="ts">
  // Brand mark for login / blocked / about pages. Renders the same master
  // logo as the rest of the UI through OctoLogo (品牌升级方案 §5.7): one
  // component, one asset — UI logo and packaged icons can never drift.
  import OctoLogo from './layout/OctoLogo.svelte'

  interface Props {
    size?: number | string
    class?: string
  }
  let { size = 40, class: className = '' }: Props = $props()
</script>

<OctoLogo {size} class={className} />
