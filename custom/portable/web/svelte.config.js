import upstreamConfig from '../../../web/svelte.config.js'

// 自定义目录只复用预处理器，避免编辑器版本不支持上游新增的编译选项。
export default {
  preprocess: upstreamConfig.preprocess,
}
