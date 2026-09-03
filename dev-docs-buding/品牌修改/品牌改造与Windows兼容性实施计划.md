# 布丁盒子品牌改造与 Windows 兼容性实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `dev-zxy` 分支完成布丁盒子（Pudding Box）品牌展示改造，并修复当前 Windows 本地 Go 验证链路中的 32 位原子对齐、跨平台 Shell 测试和绝对路径 Glob 问题，最终以 `dev-zxy -> buding` 提交远程 PR。

**Architecture:** 使用 `branding/brand.json` 作为唯一品牌资料源，Web、Mobile、Desktop、Relay、Landing 通过同步副本或读取层消费配置；兼容 CLI、协议、配置目录和 Go module 等技术身份不在本次展示品牌改造中强行替换。Windows 兼容性修复与品牌代码同一开发分支但保持独立提交，便于审查和回滚。

**Tech Stack:** Go 1.25、Svelte/Vite、TypeScript、Node.js、PowerShell、GitHub Pull Request。

**Spec:** `E:\zxy\work-code\buding-box-client\dev-docs-buding\品牌修改\品牌改造与Windows兼容性实施计划.md`

## Global Constraints

- 只允许在 `dev-zxy` 分支开发，不合并 `main`，PR 目标为远程 `buding`。
- 不使用额外 Git worktree；所有开发和验证均在 `E:\zxy\work-code\buding-box-client` 完成。
- 用户可见品牌资料必须配置化，主要编辑入口为 `branding/brand.json`。
- 中文、英文及预留国际化语言必须有回退策略，不得在组件中散落硬编码产品名称。
- CLI、旧协议、旧配置目录和 Go module 等兼容标识暂时保留，避免破坏已有用户和协议。
- 临时 Logo 必须明确标记为临时资源；正式发布前不得把 Mock 邮箱、域名和仓库占位符当作真实资料。
- Go 验证必须显式记录 GOOS、GOARCH 和 Go 路径；当前机器默认是 Windows 386，不能假设为 amd64。
- 每个行为修复先添加能够复现问题的测试，再修改生产代码。

---

### Task 1: 在 dev-zxy 重新接入品牌展示改造

**Files:**
- Create/modify: `branding/brand.json`
- Create: `branding/assets/pudding-box-mark.svg`
- Create: `internal/brand/brand.go`
- Create: `internal/brand/brand_test.go`
- Create/modify: `web/src/lib/brand.ts`, `web/src/lib/brand.config.json`, `web/src/lib/brand.test.ts`
- Create/modify: `mobile/src/brand.ts`, `mobile/src/brand.config.json`
- Create: `scripts/sync-branding.mjs`
- Modify: Desktop、Relay、Landing、Packaging、README 和 About 相关展示文件

**Implementation notes:**
- 以中文名“布丁盒子”和英文名“Pudding Box”为主品牌名。
- 配置包含产品名称、团队名称、版权、联系邮箱、About 文案、网站链接、Logo、颜色、多语言文案和未来身份迁移字段。
- 使用同步脚本将配置同步到 Go embed、Web、Mobile、Landing 和 Relay 所需位置。
- 旧 CLI 命令、路径、协议和 Go module 只放在 compatibility 配置中，不做危险的全局替换。

**Verification:**
- `go test ./internal/brand ./cmd/octo`
- Web 和 Mobile 的现有测试及构建命令
- `node scripts/sync-branding.mjs` 后检查同步副本内容一致
- `git diff --check`

### Task 2: 修复 Windows 386 下 CDP 的 int64 原子计数器

**Files:**
- Modify: `internal/browser/cdp.go`
- Test: `internal/browser/cdp_test.go` 或现有 Browser 测试范围

**Red test:**
- 在当前默认 `windows/386` 环境运行 `go test ./internal/browser -run TestSearchThenDownload -count=1`，确认现有 `atomic.AddInt64(&c.nextID, 1)` 触发 `unaligned 64-bit atomic operation`。

**Implementation:**
- 选择能够在 32 位和 64 位稳定工作的实现；优先保证字段对齐和并发语义不变。
- 如果采用结构体字段重排，必须添加针对 386 运行时的回归验证；如果采用锁保护计数器，必须确保请求 ID 单调递增且不会产生数据竞争。

**Verification:**
- 默认 Go 环境下运行相关 Browser 测试。
- `go test -race ./internal/browser` 在可用架构下运行。

### Task 3: 修复 Windows 下 Terminal 测试依赖 Unix printf 的问题

**Files:**
- Modify: `internal/tools/terminal_output_test.go`
- Create/modify: `internal/tools/test_helpers_windows.go`, `internal/tools/test_helpers_unix.go`（如需要）

**Red test:**
- 运行 `go test ./internal/tools -run TestTerminalOutputTool_LinesSnapshot -count=1`，确认 PowerShell 报告 `printf` 不存在。

**Implementation:**
- 测试必须在 Windows PowerShell 和 POSIX shell 下都生成相同的五行输出。
- 优先使用平台无关的 Go helper；不得修改生产 Shell 执行语义来迁就测试。

**Verification:**
- Windows 当前环境运行该定向测试。
- 在目标可用的 POSIX 环境运行同一测试或静态验证平台分支。

### Task 4: 修复 Windows 绝对路径 Glob 匹配

**Files:**
- Modify: `internal/tools/glob.go`
- Modify: `internal/tools/glob_test.go`

**Red tests:**
- `TestGlob_AbsolutePattern`
- `TestGlob_AbsolutePatternRecursive`
- 新增混合分隔符、盘符和相对模式回归用例。

**Implementation:**
- 不再用不同分隔符形式的字符串直接执行 `strings.TrimPrefix`。
- 对绝对模式、搜索根、盘符和分隔符进行统一规范化；使用 filepath 语义计算相对匹配模式。
- 保留 Linux/macOS 的大小写和 glob 语义，不用全局替换路径分隔符破坏其他平台。

**Verification:**
- `go test ./internal/tools -run 'TestGlob_AbsolutePattern|TestGlob_AbsolutePatternRecursive' -count=1`
- `go test ./internal/tools -count=1`

### Task 5: 完整验证、最终审计与 PR 准备

**Files:**
- Create/modify: `dev-docs-buding/品牌修改/品牌最终审计报告.md`
- Create/modify: `dev-docs-buding/品牌修改/品牌与Windows兼容性验收清单.md`

**Verification:**
- 检查当前分支为 `dev-zxy`，不包含 `main` 合并提交。
- 检查 `git status`、`git diff --check`、提交记录和改动文件范围。
- 执行 Go、Web、Mobile、品牌同步和必要的构建验证。
- 扫描用户可见旧品牌残留，并区分兼容标识与应迁移展示文案。
- 确认临时 Logo、Mock 邮箱/域名、macOS ICNS 和 Docs/Blog 迁移状态均有记录。
- 提交到 `dev-zxy`，推送 `origin/dev-zxy`，准备 PR 目标 `origin/buding`；不触碰 `main`。

---

## 提交拆分建议

1. `feat: rebrand product as Pudding Box`
2. `fix: support Windows 386 CDP counter alignment`
3. `test: make terminal output test cross-platform`
4. `fix: normalize Windows absolute glob patterns`
5. `docs: add branding and compatibility audit`

如果某一项修复无法在当前环境完成，必须保留失败日志和根因，不得用跳过测试、强制架构变量或修改断言的方式伪造通过。
