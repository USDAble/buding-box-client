# 阶段 1：L-D5 服务器词库同步

## 目标

安全同步服务器只读敏感词库；无效更新或网络失败时继续使用上一份有效版本，并向用户说明降级与恢复条件。

## 建议分支

`feat/l-d5-sensitive-dict-sync`

## 任务

- 获取服务器词库并校验 `keyId`、签名、版本。
- 固定缓存路径为 `data/sensitive-dict-server.json`，路径只通过 `internal/datapath` 获得。
- 复用 `internal/catalogstore.Store.Put` / `Load` 的版本单调和旧版保留形状。
- 复用 `internal/atomicfile.WriteFile`，不新增第二套原子写。
- 复用 `internal/productclient` 现有验签链；签名字节语义以 `PQ30` 结论为准。
- 把服务器词库接入 `internal/sensitive` 现有三层合并，不复制匹配或规范化实现。
- 落地失败提示、单次知会和恢复状态。
- 完成正向、篡改、未知 key、回滚、断网、畸形响应、写盘失败、无旧版、恢复九类测试。
- 对“跳过验签、允许回滚、失败时清空词库”至少做三项变异验证。

## 退出条件

- 有效新版本可安全生效。
- 相同版本不重写，低版本和坏签名不覆盖旧版。
- 断网或失败时旧版继续生效；无旧版时仍保留内置词和用户词，绝不 fail-open。
- 用户知道当前降级到哪一版以及何时恢复。
- `L-D5` 自动化与端到端验收通过，owner 文档同步。
- `make gate` 通过；适用时 `make test-production` 通过。

## 执行记录

- 2026-09-15：从 `v1` 当前合入点建立 `codex/feat-l-d5-sensitive-dict-sync`；保留并未修改用户原先暂存的 `.gitignore` 与 `web/src/components/chat/Composer.svelte`。
- 2026-09-15：相关包基线通过：`go test ./internal/productclient ./internal/sensitive ./internal/productruntime`。
- 2026-09-15：完成第一轮 TDD：先以缺少 `SensitiveDictionary`、`ServerStore`、`NewWithServer` 得到编译红灯，再实现控制面条件下载、`304` 归一化、服务器缓存的首次不写/版本单调/原子替换，以及内置 ∪ 用户 ∪ 服务器三层热加载。
- 2026-09-15：`go test ./internal/productclient ./internal/sensitive ./internal/productruntime` 与桌面壳独立 module 的 `go test ./...` 通过。
- 2026-09-15：两项变异复核均被测试捕获：移除服务器层合并时两条三层/热加载测试转红；反转版本比较时回滚保护测试转红。源码随后恢复。
- 2026-09-15：按计划推荐的 `PQ30` 取法①完成收口：词库信封与策略目录共用原始字节分离签名；新增 `SensitiveDictionaryEnvelope.Verify`、最终快照摘要、full/delta 应用、失败保旧与 `dictionaryNotice`。未引入 JCS、第三方依赖或第二套验签器。
- 2026-09-15：`npm test`（80 文件/881 测试）、`npm run build`、`svelte-check`（0 errors/0 warnings）、格式与规范守卫通过；完整 `make gate` 在当前沙箱因 IPv6 loopback 和 `sandbox-exec` 系统限制失败，详见同目录 `阶段总结.md`。

阶段完成后，将总结写入本目录的 `阶段总结.md`。
