<script lang="ts">
  import { brandName } from '../../lib/brand'

  interface Props {
    size?: number | string
    class?: string
  }
  let { size = 26, class: className = '' }: Props = $props()
</script>

<svg
  class={className}
  width={size}
  height={size}
  viewBox="0 0 128 128"
  fill="none"
  xmlns="http://www.w3.org/2000/svg"
  aria-label={brandName('en-US')}
  role="img"
>
  <defs>
    <linearGradient id="pb-box" x1="16" y1="20" x2="112" y2="112" gradientUnits="userSpaceOnUse">
      <stop stop-color="#F4A261"/><stop offset="1" stop-color="#D97745"/>
    </linearGradient>
    <linearGradient id="pb-pudding" x1="38" y1="24" x2="91" y2="61" gradientUnits="userSpaceOnUse">
      <stop stop-color="#FFE0A8"/><stop offset="1" stop-color="#F4A261"/>
    </linearGradient>
  </defs>
  <rect x="8" y="8" width="112" height="112" rx="30" fill="#211A2E"/>
  <path d="M25 53h78v43c0 8-6 14-14 14H39c-8 0-14-6-14-14V53Z" fill="url(#pb-box)"/>
  <path d="M30 51c0-13 10-24 23-24h22c13 0 23 11 23 24 0 8-7 14-15 14H45c-8 0-15-6-15-14Z" fill="url(#pb-pudding)"/>
  <path d="M57 28c-2-8 7-12 12-5 5-7 14-3 12 5" stroke="#D97745" stroke-width="5" stroke-linecap="round"/>
  <path d="M25 59h78" stroke="#FFF8F0" stroke-opacity=".5" stroke-width="4"/>
  <circle cx="50" cy="80" r="5" fill="#FFF8F0" fill-opacity=".9"/>
  <circle cx="78" cy="80" r="5" fill="#FFF8F0" fill-opacity=".9"/>
  <path d="M48 94c8 7 24 7 32 0" stroke="#FFF8F0" stroke-width="4" stroke-linecap="round"/>
</svg>
