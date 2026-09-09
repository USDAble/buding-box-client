# packaging/portable 模板说明 / README for the portable template

本目录是 `make desktop-portable`（`scripts/package-portable.mjs`）的模板来源。
脚本把 `data/` 原样复制进产物，并把 `使用说明.txt` 里的 `{nameZh}` / `{nameEn}` /
`{exeName}` 占位符替换成 `branding/brand.json` 的取值后写入产物。

This directory is the template source for `make desktop-portable`
(`scripts/package-portable.mjs`). The script copies `data/` verbatim into the
artifact and renders `使用说明.txt`, replacing the `{nameZh}` / `{nameEn}` /
`{exeName}` placeholders with values from `branding/brand.json`.

## data/ 预置内容 / pre-filled data/

| 文件 / file | 归属 / owner | 说明 / note |
|---|---|---|
| `sensitive-words.txt` | P7（已合并） | 空词库 + 头部注释；内置词编译进二进制，不落文件 |
| `chat-modes.json` | P9（待开工） | 占位 `{}`；P9 落地后替换为模式分组 schema |
| `config.yml` | P11（可延后） | 占位空配置（等价于无文件 → onboarding）；P11 落地后填入 buding 端点 |
| `workspace/.gitkeep` | P1 | 会话默认工作区占位目录 |

> P9 / P11 未落地前，`chat-modes.json` 与 `config.yml` 是占位内容。它们在打包
> 脚本里作为「data/ 含四个预置文件」自检项存在，正式内容由对应 PR 替换，**打包
> 脚本不改**（与占位图标同一原则，见 `技术方案/P12-便携打包.md` §3.6、§9）。
