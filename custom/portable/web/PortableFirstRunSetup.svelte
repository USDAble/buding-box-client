<script lang="ts">
  import { onMount } from 'svelte'
  import { onboardPhase } from '../../../web/src/lib/stores'

  // 便携版已经由 U 盘启动器预置受限模型端点和 onboarding 标记。
  // 这里仅作为旧数据或宿主机残留配置触发 key_setup 时的兼容兜底：
  // 直接放行到桌面，不调用上游配置接口，不修改上游首次运行组件。
  // AI 请求仍必须经过 custom/portable/gateway 的策略校验与失败关闭链路。
  onMount(() => {
    onboardPhase.set('')
  })
</script>

<!-- 组件只在状态切换的同一帧短暂存在，不显示模型配置表单。 -->
