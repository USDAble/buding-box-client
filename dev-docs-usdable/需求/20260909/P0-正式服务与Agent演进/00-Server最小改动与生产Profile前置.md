# P0-00：Server 最小改动与 Production Profile 前置

| 项 | 内容 |
| --- | --- |
| 主责建议 | 客户端架构/发布负责人；需要仓库管理员配合 |
| 优先级 | **P0 开发前置阻塞项**；在任一真实中台接入 PR 前完成 |
| 当前仓库工作 | 确认 `buding` 已接收仓库负责人批准的 `main`、冻结最小 server 挂点、审计 production 启动面与绕过；不开发中台服务，也不处理上游 remote |
| 依据 | [P0 正式上线需求基线](../P0-正式上线需求基线.md) R1、R4、R5、R6、R8、R12 |

## 为什么必须先做

`internal/server/server.go` 于 2026-06-01 已在上游存在，桌面启动代码也来自上游。布丁的既有下游改动是在 2026-09-08 至 09 引入的：P4 登录/激活（`7b8c8aae`）、P6 积分占位（`640ad9d8`）、P8 敏感词接入（`e4d0c3c9`）和 P11 本地假模型端点（`aa7b596f`）。这是一份需要控制的历史清单，**不是继续在 `internal/server` 堆产品逻辑的理由**。

P0 的默认约束是：**不触碰 `internal/server`。** 既有下游文件保留原样，新逻辑优先新增于产品专属包。只有自动化测试证明无法在 desktop 装配或新包中实现的 production 安全门，才允许逐项批准一处最小 server 改动；禁止搬迁、重命名、重构或顺手整理 `internal/server`。每个例外都必须是单独 PR、带 `OCTO-FORK` 标记和回归测试。

## 开工前清单

1. 仓库负责人按既定流程先将上游合入 `main`，再将已批准的 `main` 合入 `buding`；P0 分支只从该 `buding` 开出。上游地址、remote、fetch 和 merge-base 由维护者负责，当前客户端开发不处理也不以其作为阻塞项。
2. 在本文件登记既有 P4/P6/P8/P11 下游改动，以及 P0 允许触碰的文件、原因、负责人和退出条件；未登记即默认禁止改 `internal/server`。
3. 冻结产品包命名：P0 不创建 `internal/backend` 或 `internal/productapp`。新建 `internal/productclient`、`internal/productclient/gateway`、`internal/productpolicy`、`internal/credentialstore`；各自职责见[P0-01](01-运行时Profile与窄端口抽象.md)。
4. 定义 production Profile 的可信来源：只能由签名发布包内的不可变构建标记或签名清单决定；环境变量、`config.yml`、本地数据和 WebView 参数均不得将标准包切换为 developer/demo。
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
| profile 选择来源 | 当前尚无 production/developer 可信切换机制 | 标准包的 profile 仅信任嵌入式构建标记或签名清单；任何环境变量、本地配置、缓存、命令行或 WebView 输入都不能提升到 developer/demo。 |
| IM channel、MCP、任务、工具 | 桌面当前 `Tools: true`，且未显式 `NoChannel: true` | 标准包使用显式启动面 allowlist；默认不自动启动未获策略许可的 channel/后台能力。隐藏功能保留，后续按能力矩阵与 PEP 受控启用。 |

## server 的最小挂点预算

P0 允许新增产品包与接口，但禁止把上游 server 的路由、会话或 agent 编排搬进新包。`internal/server` 默认零改；下表是仅在无外部替代方案且单独批准后可用的例外，不是预授权改动清单：

| 位置 | 允许的最小改动 | 禁止事项 |
| --- | --- | --- |
| `internal/server/server.go` | 仅当 desktop 层无法注入时，在下游独立区块增加一个可选 profile/sender resolver；production 不种入 local endpoint | 改既有公开函数签名、移动 route 注册、重排初始化流程 |
| `internal/server/product_*.go` | 仅修改既有下游产品 handler 的请求/响应映射 | 将通用上游 handler 迁出、在 handler 内实现 HTTP 重试/签名/账本规则 |
| `internal/server/handlers.go`、`ws_handlers.go` | 仅删除 P6 固定积分 mock，或经批准接入 production sender | 重写通用聊天/WS 生命周期或改变其他 surface 行为 |
| `cmd/octo-desktop/` | 优先在此构造可信 profile、credential store、product client 和启动面 | 把中台业务实现塞进 `main.go` |

每次触碰上游文件：新增逻辑放独立新文件或末尾下游区块；使用 `OCTO-FORK` 标记；每个改动保留原有测试并新增 profile 回归。无法满足预算时，先回写本文件与架构评审，不在实现中临时扩大范围。

## 验收与退出条件

- `buding` 已接收仓库负责人批准的 `main`；当前客户端不维护上游 remote、地址或 fetch 流程。
- 所有 production 旁路在 fake 黑盒测试中被拒绝：开发 URL、环境 provider/model、local endpoint、`config.yml` endpoint、数据根覆盖和未授权启动面。
- production Profile 的来源不可由运行时输入伪造；`internal/server` 的 P0 例外改动清单已批准，且没有“先重构再接服务”的隐藏任务。
- P0-01 的 `productclient` DTO、错误码和中台字段草案完成评审，才开始 P0-02 至 P0-10 的并行实现。
