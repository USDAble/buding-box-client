# P0-00：Fork 基线与 Production Profile 前置

| 项 | 内容 |
| --- | --- |
| 主责建议 | 客户端架构/发布负责人；需要仓库管理员配合 |
| 优先级 | **P0 开发前置阻塞项**；在任一真实中台接入 PR 前完成 |
| 当前仓库工作 | 建立上游基线、最小 server 挂点设计、production 启动面与绕过审计；不开发中台服务 |
| 依据 | [P0 正式上线需求基线](../P0-正式上线需求基线.md) R1、R4、R5、R6、R8、R12；[上游合并策略](../../../上游合并策略.md) |

## 为什么必须先做

`internal/server/server.go` 于 2026-06-01 已在上游存在，桌面启动代码也来自上游。当前仓库尚未配置 `upstream` remote，而产品代码已经在 `internal/server` 的登录、积分、词库和模型路径上叠加改动。若此时再把 `server` 重构成新的应用层，后续每次上游合并都会同时处理产品迁移和上游功能变化，风险不可接受。

P0 采用以下约束：**不搬迁、不重命名、不重构 `internal/server`。** 新逻辑新增于产品专属包；只有无法从外部实现的 production 安全门允许在既有 server 产品挂点做小改动，且必须是单独 PR、带 `OCTO-FORK` 标记和回归测试。

## 开工前清单

1. 由仓库管理员确认真实上游地址后添加只读 `upstream` remote，`fetch` 后在[上游合并策略](../../../上游合并策略.md)记录 merge-base、落后提交数和本轮是否先合并。
2. 按既定流程将上游先合到 `main`，再合入 `buding`；P0 分支只能从该最新 `buding` 切出。没有可核验上游基线，不开始 P0 代码开发。
3. 输出下游改动清单：`git diff <merge-base> -- internal/server cmd/octo-desktop web`，并用 `OCTO-FORK` 标记核对每个上游文件改动。为 P0 预先登记允许触碰的 server 文件、原因、负责人和退出条件。
4. 冻结产品包命名：P0 不创建 `internal/backend` 或 `internal/productapp`。新建 `internal/productclient`、`internal/productclient/gateway`、`internal/productpolicy`、`internal/credentialstore`；各自职责见[P0-01](01-运行时Profile与窄端口抽象.md)。
5. 在 fake 下先完成 production profile 的黑盒测试清单，不能以“接到 sandbox 后再补”替代。后续每个 P0 PR 都必须引用该清单。

## Production 启动面审计

现有代码存在面向开发/上游的启动与配置入口；它们不是要删除的功能，但不得成为正式包的旁路。P0-00 必须为下表每项确定 production 的拒绝、忽略或受控启用语义，并以自动化测试固定。

| 当前入口 | 现状证据 | production 要求 |
| --- | --- | --- |
| `OCTO_DESKTOP_DEV_URL` | 桌面启动可将 WebView 指向任意本地开发 URL | 正式签名包忽略该变量；开发构建才允许，且不得把 window token 暴露给非受信 URL。 |
| `OCTO_PROVIDER`、`*_MODEL`、供应商环境密钥 | `server` 先解析环境变量和 `config.yml` 的 provider/model | production 模型回合只接受已验证 catalog 的 `modelId` 和 gateway resolver；这些变量不能覆盖。 |
| `config.yml` endpoint / API key / headers | 上游配置 API 可新增供应商 endpoint | 功能代码保留给 developer；production session 忽略其作为模型来源，且 UI/路由策略不提供可用的正式旁路。 |
| `ensureLocalEndpoint()` | `server.New` 当前无条件种入 local demo endpoint | production profile 不种入、不选择、不回退；demo/developer 保留。 |
| `OCTO_DATA_ROOT` | 数据根允许环境变量覆盖 | 正式桌面包固定到程序同级 `data/`；测试、CLI、developer 才可显式覆盖。 |
| IM channel、MCP、任务、工具 | 桌面当前 `Tools: true`，且未显式 `NoChannel: true` | 标准包使用显式启动面 allowlist；默认不自动启动未获策略许可的 channel/后台能力。隐藏功能保留，后续按能力矩阵与 PEP 受控启用。 |

## server 的最小挂点预算

P0 允许新增文件与接口，但禁止把上游 server 的路由、会话或 agent 编排搬进新包。预计仅有以下类型的改动，实际 PR 必须逐项说明：

| 位置 | 允许的最小改动 | 禁止事项 |
| --- | --- | --- |
| `internal/server/server.go` | 在下游独立区块增加 profile/sender resolver 注入点；production 不种入 local endpoint | 改既有公开函数签名、移动 route 注册、重排初始化流程 |
| `internal/server/product_*.go` | 已存在的布丁登录/账户/词库 handler 调用 `productclient`，做请求/响应映射 | 将通用上游 handler 迁出、在 handler 内实现 HTTP 重试/签名/账本规则 |
| `internal/server/handlers.go`、`ws_handlers.go` | production sender 选择、删除固定积分 mock 的必要小改动 | 重写通用聊天/WS 生命周期或改变其他 surface 行为 |
| `cmd/octo-desktop/` | 构造 profile、credential store、product client，并显式设置 desktop 启动面 | 把中台业务实现塞进 `main.go` |

每次触碰上游文件：新增逻辑放独立新文件或末尾下游区块；使用 `OCTO-FORK` 标记；每个改动保留原有测试并新增 profile 回归。无法满足预算时，先回写本文件与架构评审，不在实现中临时扩大范围。

## 验收与退出条件

- `upstream` remote、merge-base、落后数量和合并决定均已记录；`main`/`buding` 基线符合合并策略。
- 所有 production 旁路在 fake 黑盒测试中被拒绝：开发 URL、环境 provider/model、local endpoint、`config.yml` endpoint、数据根覆盖和未授权启动面。
- `internal/server` 的 P0 改动清单已批准；没有“先重构再接服务”的隐藏任务。
- P0-01 的 `productclient` DTO、错误码和中台字段草案完成评审，才开始 P0-02 至 P0-10 的并行实现。
