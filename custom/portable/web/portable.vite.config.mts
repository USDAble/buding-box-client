import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'
import upstreamConfig from '../../../web/vite.config.ts'

const portableDir = dirname(fileURLToPath(import.meta.url))

// 仅在便携版构建时覆盖登录组件和全局样式入口；上游引用路径保持不变。
export default {
  ...upstreamConfig,
  define: {
    ...upstreamConfig.define,
    // 测试构建可强制重新显示登录页；正式包默认恢复 U 盘中的登录状态。
    __PORTABLE_FORCE_LOGIN__: process.env.PORTABLE_FORCE_LOGIN === '1',
  },
  resolve: {
    ...upstreamConfig.resolve,
    alias: [
      {
        find: /^\.\/components\/overlays\/AuthGate\.svelte$/,
        replacement: resolve(portableDir, 'PortableAuthGate.svelte'),
      },
      {
        find: /^\.\/components\/overlays\/FirstRunSetup\.svelte$/,
        replacement: resolve(portableDir, 'PortableFirstRunSetup.svelte'),
      },
      {
        find: /^\.\/VersionBadge\.svelte$/,
        replacement: resolve(portableDir, 'PortableSidebarFooter.svelte'),
      },
      {
        find: /^\.\/app\.css$/,
        replacement: resolve(portableDir, 'portable.css'),
      },
      {
        find: /^\.\/lib\/auth$/,
        replacement: resolve(portableDir, 'portable-auth.ts'),
      },
      {
        find: /^\.\/auth$/,
        replacement: resolve(portableDir, 'portable-auth.ts'),
      },
    ],
  },
}
