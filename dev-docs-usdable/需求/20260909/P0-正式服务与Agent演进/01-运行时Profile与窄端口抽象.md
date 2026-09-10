# P0-01：客户端 `internal/backend` 产品服务抽象层（最高前置）

| 项 | 内容 |
| --- | --- |
| 主责建议 | 当前仓库的客户端核心组 |
| 优先级 | **所有 P0 的第一优先级；端口、DTO、错误语义和 local 迁移须先合入** |
| 可并行 | 字段草案冻结后，中台可在其仓库实现接口；本仓库 P0-02/03/04/06/07 可使用 fake 并行 |
| 依赖 | 无 |
| 风险等级 | 高：触及 `server.New`、产品状态、配置、sender 装配与现有 demo 行为 |

> **需求约束**：本任务的产品边界以 [P0 正式上线需求基线](../P0-正式上线需求基线.md) R1-R10 为准，合并次序以 [P0 开发顺序与协作计划](P0-开发顺序与协作计划.md) 为准。`internal/backend` 和 `internal/productapp` 均为本任务新建的客户端包；中台服务不在当前仓库实现。

## 目标

先建立**客户端内部**的 `internal/backend` 产品服务抽象层：将散落在 `internal/server` 的本地登录、产品状态、模型目录和 mock 业务行为收口为稳定端口、DTO 与错误语义，并提供 local/remote adapter 的注入边界。同时新增很薄的 `internal/productapp`，承接登录、bootstrap、发起回合和本地数据处理等产品用例，避免 `internal/server` 在接中台后继续堆积业务编排。它们是后续远程 adapter、任务、技能、连接器演进的接缝，但不实现中台服务。

`demo` / `developer` / `production` profile 是这层抽象合入后的装配和权限约束；本任务会定义其接入点与测试口径，但不得让 profile 工作阻塞 B0 的端口收口。

## 设计与代码边界

1. 在 `internal/backend` 定义 `AuthPort`、`ControlPlanePort`、`UsageReader`、产品 DTO 和 typed error；`ModelGatewaySender` 留在 provider/agent 适配层，不能伪装成“扣积分服务”。
2. 以 `backend/local` 原样承接现有 mock 和产品状态；不改变登录、手机号、昵称、会话和 demo 的既有 machine code 语义。
3. `internal/productapp` 只实现 P0 的 `LoginUseCase`、`BootstrapUseCase`、`SendTurnUseCase`、`DeviceDataUseCase`；其输入/输出为产品 DTO，不依赖 HTTP、WebSocket 或 Svelte。
4. `internal/server` 只做 transport 与 composition root：在一个装配点选择 local 或后续 remote **客户端适配器**；handler 负责解码/映射并调用 product use case，不直接调用 HTTP、`productstate` 或 `config/endpoints` 决定正式模型。
5. 定义 remote adapter 的构造边界、fake 和 fixture；它只调用中台接口，当前任务不创建任何中台 HTTP handler、数据库或计费服务。
6. 在上述边界稳定后，新建 `internal/productprofile`：profile 由构建元数据和签名发布配置确定；生产包不接受普通环境变量切到 `developer`，且 `ensureLocalEndpoint()`、固定验证码和 endpoint CRUD 不能成为 production 的默认或失败回退。

## 实施步骤

1. **B0（首个 PR）**：在 ADR 中冻结端口、DTO、machine code、product use case 输入/输出、local/remote 依赖方向和“不实现中台服务”的边界；这一版字段清单可直接发送中台评审。
2. **B1（紧接 B0）**：完成 `backend/local` 的只读产品投影迁移；原有登录、会话等非计费行为测试不得改变断言或语义。固定积分扣减 mock 不迁入，按 P0-05 删除。
3. **B2（可独立 PR）**：新增最小 `internal/productapp`，将 `internal/server` 改为 transport/composition root，注入 local/fake 实现；禁止 handler 临时直连远端。
4. **B3（随后接入）**：定义 `remote` 客户端 adapter 的构造占位和 fake，供 P0-02/P0-03 注入；中台接口未就绪前不阻塞客户端单测。
5. **B4（装配约束）**：冻结 profile 行为表、构建注入字段、拒绝路径和数据目录迁移；每个 profile 的 sender、端点路由、菜单能力和失败行为可验证。

## 接口产出

```go
type AuthPort interface { Login(...); Refresh(...); Logout(...) }
type ControlPlanePort interface { Bootstrap(...); Models(...); SensitiveDictionary(...) }
type UsageReader interface { Usage(...); RequestStatus(...) }
type CapabilityPolicy interface { Evaluate(session, capability, input) Decision }
```

端口 DTO 由客户端先按产品需求冻结，再与中台 OpenAPI 做映射；不能让外部 OpenAPI 类型直接渗入 UI、server handler 或 agent。端口只暴露产品需要的字段，不泄漏供应商协议。

## 验收与合并

- B0/B1 先行合入：现有客户端的非计费行为测试在 local 实现下语义不变，handler 不再直接依赖散落的产品 mock。
- production profile 中手工写 `data/config.yml` 的外部 endpoint 后，新会话仍无法选作正式模型。
- demo/developer 的本地模型和 endpoint 管理不回归。
- `server.New` 的唯一装配点可在测试中注入 fake local/remote ports。
- 拆为 B0 端口/DTO、B1 local 等价迁移、B2 组合装配、B3 fake/remote adapter 边界、B4 profile guard；每份可独立回滚。

## 不做

不在此任务实现中台 HTTP 服务、模型网关、token 账本、数据库迁移或任务引擎；P0-02/03/05 只负责当前仓库内对应的 remote adapter、sender 和集成调用链。
