# 布丁盒子品牌与 Windows 兼容性验收清单

> 本清单对应 `dev-zxy` 分支本轮“品牌修改”主题。完成项必须有命令或文件证据；未完成项不能用“跳过”替代。

## 分支与工作区

- [x] 当前开发分支为 `dev-zxy`。
- [x] 未合并 `main`，未在 `buding` 分支直接开发。
- [x] 推送 `origin/dev-zxy`。
- [ ] 创建 PR：源分支 `dev-zxy`，目标分支 `buding`。

## 品牌资料与资源

- [x] `branding/brand.json` 作为唯一主配置。
- [x] 中文名称：布丁盒子；英文名称：Pudding Box。
- [x] 配置包含中文、英文和未来多语言名称/回退所需字段。
- [x] About、团队、版权、联系方式、链接、常用文案均配置化。
- [x] 临时 Logo 位于 `branding/assets/pudding-box-mark.svg`，并标记 `temporary: true`。
- [x] 同步脚本可重复执行：`node scripts/sync-branding.mjs`。
- [x] 同步副本 SHA-256 与主配置一致。
- [ ] 发布前替换 `.example` 域名/邮箱和 `<your-org>` 仓库占位符。
- [ ] 发布前替换正式 Logo，生成 ICO/ICNS/移动端图标和分享图。

## Go / Windows

- [x] `go version` 已记录为 Go 1.25.0 `windows/386`。
- [x] `go test ./... -run '^$' -count=1` 通过。
- [x] `go build ./...` 通过。
- [x] `go test ./internal/brand ./cmd/octo` 通过。
- [x] `go test ./internal/tools -count=1` 通过。
- [x] CDP 64 位计数器对齐回归测试通过。
- [x] Desktop 使用 `GOARCH=amd64` 构建和测试通过。
- [x] Relay 嵌套模块编译通过。
- [ ] Browser 完整集成测试需在可用 Chrome/DevTools 环境重新运行；当前 2 分钟超时已记录。
- [ ] `evals` hidden fixture 不作为普通产品模块测试；需由专用评测流程验证。

## 前端

- [x] Web `npm ci` 完成。
- [x] Web `npm run build` 通过。
- [x] Web `npm test`：50 个测试文件、666 个测试通过。
- [x] Mobile `npm ci` 完成。
- [x] Mobile `npm run typecheck` 通过。
- [x] Mobile `npm test`：4 个测试文件、19 个测试通过。
- [ ] 各平台安装包人工 UI/安装/升级验收。

## 最终审计与提交

- [x] 最终审计报告已更新为 `dev-zxy` 和当前工作区。
- [x] 审计报告区分“品牌展示完成”“技术身份迁移未完成”“Browser 环境阻断”。
- [ ] `git diff --check` 通过。
- [ ] 审计文档提交到 `dev-zxy`。
- [x] 推送后确认远程分支存在。
- [ ] PR 创建后确认 base 为 `buding`，不是 `main`。

## 后续可修改入口

| 需求 | 修改位置 | 修改后动作 |
|---|---|---|
| 中文/英文名称、About、邮箱、域名、色板 | `branding/brand.json` | 运行 `node scripts/sync-branding.mjs` |
| Logo 源文件 | `branding/assets/pudding-box-mark.svg` | 替换并重新生成各平台资源 |
| 新增语言 | `branding/brand.json` 的语言字段 | 补充回退测试并重新同步 |
| CLI/目录/协议迁移 | `branding/brand.json` 的 `futureMigration` + 独立迁移方案 | 先做兼容、迁移、回滚设计 |
| 文档站/博客品牌 | `docs/`、`blog/`、`shared/octo-nav.js` | 单独建立主题文档和验收清单 |

详细证据见：

`E:\zxy\work-code\buding-box-client\dev-docs-buding\品牌修改\品牌最终审计报告.md`

