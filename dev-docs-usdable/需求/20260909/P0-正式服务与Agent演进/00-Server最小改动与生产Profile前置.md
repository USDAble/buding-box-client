# P0-00：Server 最小改动与 Production Profile 前置

| 项 | 内容 |
| --- | --- |
| 主责建议 | 客户端架构/发布负责人；需要仓库管理员配合 |
| 优先级 | **P0 开发前置阻塞项**；在任一真实中台接入 PR 前完成 |
| 当前仓库工作 | 确认 `buding` 已接收仓库负责人批准的 `main`、冻结最小 server 挂点、审计 production 启动面与绕过；不开发中台服务，也不处理上游 remote |
| 依据 | [P0 正式上线需求基线](../P0-正式上线需求基线.md) R1、R4、R5、R6、R8、R12；[运行时 Profile 配置](../../../运行时Profile配置.md) |

## 为什么必须先做

`internal/server/server.go` 于 2026-06-01 已在上游存在，桌面启动代码也来自上游。布丁的既有下游改动是在 2026-09-08 至 09 引入的：P4 登录/激活（`7b8c8aae`）、P6 积分未批准（`640ad9d8`）、P8 敏感词接入（`e4d0c3c9`）和 P11 本地确定性模型端点（`aa7b596f`）。这是一份需要控制的历史清单，**不是继续在 `internal/server` 堆产品逻辑的理由**。

P0 的默认约束是：**不新增 `internal/server` 产品逻辑。** 2026-09-08/09 的布丁逻辑不是永久归属：应按 [P0-01A](01A-既有产品逻辑迁出internal-server.md) 从 `internal/server` 逐步迁至 `internal/productruntime`（产品 HTTP/运行时装配）及其下游专属包；迁移不得借机重构上游 server。只有自动化测试证明无法在 desktop 装配或产品运行时中实现时，才允许新增一处与布丁业务无关的窄扩展端口。每个例外都必须是单独 PR、带 `OCTO-FORK` 标记和回归测试。

## 开工前清单

1. 仓库负责人按既定流程先将上游合入 `main`，再将已批准的 `main` 合入 `buding`；P0 分支只从该 `buding` 开出。上游地址、remote、fetch 和 merge-base 由维护者负责，当前客户端开发不处理也不以其作为阻塞项。
2. 在本文件登记既有 P4/P6/P8/P11 下游改动，以及 P0 允许触碰的文件、原因、负责人和退出条件；未登记即默认禁止改 `internal/server`。
3. 冻结产品包命名：P0 不创建 `internal/backend` 或 `internal/productapp`。新建 `internal/productruntime`、`internal/productprofile`、`internal/productclient`、`internal/productclient/gateway`、`internal/productpolicy`、`internal/credentialstore`；各自职责见[P0-01](01-运行时Profile与窄端口抽象.md)。
4. 按[全局 Profile 配置](../../../运行时Profile配置.md)定义可信来源：只存在 `production` 和 `developer` 两种编译期 Profile，且由嵌入二进制的只读 JSON 与构建标签选择；环境变量、`config.yml`、本地数据和 WebView 参数均不得切换。`developer` 仅表示开发能力集，不对应用户可写的“环境”。
5. 在 合同测试实现 下先完成 production profile 的黑盒测试清单，不能以“接到 sandbox 后再补”替代。后续每个 P0 PR 都必须引用该清单。

## Production 启动面审计

现有代码存在面向开发/上游的启动与配置入口；它们不是要删除的功能，但不得成为正式包的旁路。P0-00 必须为下表每项确定 production 的拒绝、忽略或受控启用语义，并以自动化测试固定。

| 当前入口 | 现状证据 | production 要求 |
| --- | --- | --- |
| `OCTO_DESKTOP_DEV_URL` | 桌面启动可将 WebView 指向任意本地开发 URL | 正式签名包忽略该变量；开发构建才允许，且不得把 window token 暴露给非受信 URL。 |
| `OCTO_PROVIDER`、`*_MODEL`、供应商环境密钥 | `server` 先解析环境变量和 `config.yml` 的 provider/model | production 模型回合只接受已验证 catalog 的 `modelId` 和 gateway resolver；这些变量不能覆盖。 |
| `config.yml` endpoint / API key / headers | 上游配置 API 可新增供应商 endpoint | 保留读取、保存和测试能力；standard production UI 是否显示入口按 P0-00A/P0-04 控制，P0-05 后正式会话不以它作为模型来源。 |
| `ensureLocalEndpoint()` | **已于 2026-09-11 删除**（连同 `local_endpoint_test.go` 与 4 个 `buding-*` 假模型 endpoint 定义） | 假模型不再进入产品：正式模型列表来自中台签名目录并本地缓存（见 P0-04）。删除后 `server.New` 不再在启动期写用户 `config.yml`。`internal/provider/local` 保留为 developer/test 专用通道，需在测试配置里**显式**添加 endpoint 才能使用，不再自动种入。 |
| `OCTO_DATA_ROOT` | 数据根允许环境变量覆盖 | 正式桌面包固定到程序同级 `data/`；测试、CLI、developer 才可显式覆盖。 |
| `config.yml` 的 `workspace_dir` | `server.New` 当前用它覆盖默认会话工作区 | 保留既有优先级：显式 `workspace_dir` 继续生效；未设置时才解析为 `data/workspace`。用户在单次会话中明确选定主机目录后，后续访问仍按 PEP 单独授权。 |
| profile 选择来源 | `internal/productprofile/profiles/{production,developer}.json` 已嵌入二进制，由构建标签选择 | 任何环境变量、本地配置、缓存、命令行或 WebView 输入都不能切换 Profile。 |
| IM channel、MCP、任务、工具 | 桌面当前 `Tools: true`，且未显式 `NoChannel: true` | 保持既有启动与实现；首发菜单可隐藏，后续由能力矩阵处理可见性/资格，PEP 处理实际执行授权。 |

### `startup` 字段的现状（P0-00 尚未完成）

`internal/productprofile/profiles/{production,developer}.json` 的 `startup`（channels/tools/mcp/backgroundTasks）目前**全部为 `true`**，且 `Profile.Validate()` 不约束这组字段——它还没有接入实际启动面。这与当前 [P0-00A](00A-生产入口可见性与功能保留.md)“既有功能不因 Profile 关闭”和[开发顺序与协作计划](P0-开发顺序与协作计划.md) §5.2“IM channel、后台任务和工具的既有功能不因本任务禁用”是**一致**的，因此**不得**把 `startup=false` 当作首发收紧手段。待 P0-04 能力矩阵落地时再决定这组字段是否要区分“自动启动”与“功能保留”两种语义；一旦决定，必须同时更新两份 JSON、`Validate()`、启动装配、`profile_test.go` 与黑盒测试，并回写本文件。P0-00 验收只需确认这组字段当前不会被误读为“已接入启动面”。

## server 的最小挂点预算

P0 允许新增产品包与接口，但禁止把上游 server 的路由、会话或 agent 编排搬进新包。`internal/server` 默认零改；下表是仅在无外部替代方案且单独批准后可用的例外，不是预授权改动清单：

| 位置 | 允许的最小改动 | 禁止事项 |
| --- | --- | --- |
| `internal/server/` | 默认不改。仅当 desktop 与 `productruntime` 都无法接入时，允许一个通用、无产品业务语义的扩展端口 | `product_*.go`、登录/积分/词库/本地模型、产品 HTTP、HTTP 重试、签名、账本规则或公开函数签名 |
| `internal/productruntime/` | 承接 2026-09-08/09 的产品 HTTP、登录/状态投影、聊天前置策略和产品装配；只通过已批准的窄端口与 server 协作 | 复制 server 的通用路由、会话、WS 或 agent 生命周期 |
| `cmd/octo-desktop/` | 加载 `productprofile`，构造产品运行时并设置启动面 | 把中台业务实现塞进 `main.go` |

每次触碰上游文件：新增逻辑放独立新文件或末尾下游区块；使用 `OCTO-FORK` 标记；每个改动保留原有测试并新增 profile 回归。无法满足预算时，先回写本文件与架构评审，不在实现中临时扩大范围。

### 已有产品代码的迁移清单

以下是 2026-09-08/09 已落在 `internal/server`、且不应继续留在该包的代码。它们不是本次只改文档就可以删除的死代码；迁移必须按 [P0-01A](01A-既有产品逻辑迁出internal-server.md) 定义中性端口并保持既有行为测试。

| 现有位置 | 归属与迁移目标 | 约束 |
| --- | --- | --- |
| `product_handlers.go`、`product_login.go`、`product_nickname.go`、`product_prefs.go`、`product_sensitive.go`、`sensitive_dict_handlers.go` 及对应测试 | 登录、账户、偏好、词库等产品 HTTP → `internal/productruntime` | 路由挂载若无外部替代，才申请一个通用注册端口；不得把产品路由继续加到 server。 |
| `chatmode_handlers.go`、`privacy.go` 及对应测试 | 产品模式、PII/发送副本策略 → `productruntime` 与 `productpolicy` | 不复制 server 的 WS、会话或 agent 生命周期。 |
| `server.go` 中 product state/gate/login code、`apiProduct` 路由改写、敏感词/PII sender 包装 | 产品状态、产品策略与模型装配 → `productruntime`；`apiProduct` → 折叠为顶层产品门中间件并还原上游路由表（见 P0-01A §2） | 保留 server 的通用 sender/session 基础设施。`ensureLocalEndpoint()` 已删除，不再是暂留项。 |
| `handlers.go`、`ws_handlers.go`、`tasks_handlers.go` 中积分、产品门、隐私等调用点 | 依赖 P0-01 定义的通用回合前置端口后迁移 | 在端口获批前禁止分散地修改上游 handler。 |

## 验收与退出条件

- `buding` 已接收仓库负责人批准的 `main`；当前客户端不维护上游 remote、地址或 fetch 流程。
- 开发 URL、环境 provider/model、数据根覆盖与运行时 Profile 切换在 production 黑盒测试中被拒绝；`config.yml`、MCP、channel、工具与后台能力保留既有功能，另按 P0-00A 验证入口可见性与 PEP。P0-05 验证正式会话不会选择本地模型来源（`config.yml` 在 developer/test 仍可正常配置第三方模型）。
- `production`/`developer` 的嵌入式全局配置、构建标签和发布脚本已验收：**所有出包路径（`make desktop-portable` / `desktop-app` / `desktop-appimage`、`portable.yml`、`release.yml`、`desktop.yml`、`windows-installer-check.yml`）都必须带 `product_production`**，且由 `make release-profile-check`（CI `release-profile-guard`）固定——任一出包路径缺少该标签即 CI 失败。（`.goreleaser.yaml` 只构建 `cmd/octo` CLI，CLI 不携带产品 Profile，不在该 guard 范围内。）Profile 来源不可由运行时输入冒充。
- `internal/server` 的 P0 例外改动清单为空，除非有单独批准的通用扩展端口；没有“先重构再接服务”的隐藏任务。
- P0-01 的 `productclient` DTO、错误码和中台字段草案完成评审，才开始 P0-02 至 P0-10 的并行实现。
