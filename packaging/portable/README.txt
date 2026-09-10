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
| `config.yml` | P11（可延后） | 占位空配置（等价于无文件 → onboarding）；不在 production 种入本地模型 endpoint |
| `workspace/.gitkeep` | P1 | 会话默认工作区占位目录 |

> **`chat-modes.json` 不再预置（2026-09-11）**：它的出厂内容原本是 4 个 `buding-*` 假模型的分组，
> 而这些 id 依赖已删除的 `ensureLocalEndpoint()` 种入；模型列表现在由中台签名目录下发（P0-04）。
> 缺失该文件与出厂分组**等价**（`chatmode.Load` 直接返回 `Builtin()` 且不写盘），所以预置它
> 只会把已删除的假数据重新塞回交付物。`DATA_FILES` 只保留上面三项。
