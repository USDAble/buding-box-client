# P0-01：产品客户端契约与最小 Server 挂点

| 项 | 内容 |
| --- | --- |
| 主责建议 | 客户端核心组 |
| 优先级 | P0-00 完成后的首个功能 PR |
| 依赖 | [P0-00](00-Server最小改动与生产Profile前置.md)；不依赖中台真实服务 |
| 当前仓库范围 | 新建产品专属包、fake/contract、最小 server 注入设计；不迁移或重构上游运行时 |
| 需求基线 | [P0 正式上线需求基线](../P0-正式上线需求基线.md) R1、R4、R5、R6、R8、R12 |

## 目标

建立清晰的客户端边界，而不是建立一个名为“backend”的第二套后端，也不建立与 `internal/app` 重叠的应用层。

1. 新建 `internal/productclient`，按身份、控制面、只读用量三个窄客户端定义 DTO、typed error 和 fake。
2. 新建 `internal/productclient/gateway`，实现中台模型网关的 `agent.Sender` 适配器；不提供 `Consume` 或任意客户端扣费能力。
3. 新建 `internal/productpolicy`，验证签名策略快照并在本地 PEP 执行能力决策。
4. 定义 `internal/credentialstore` 的最小接口，为 P0-08 提供实现位置。
5. 在不移动 `internal/server` 代码的前提下，设计并测试一个 production sender resolver 挂点；真实接入时只在 P0-00 登记的少量下游位置调用。

## 合同骨架

```go
// internal/productclient: no server, Web, or agent-session dependency.
type AuthClient interface {
    SendCode(ctx context.Context, phone string) (Cooldown, error)
    Login(ctx context.Context, req LoginRequest) (LoginResult, error)
    Refresh(ctx context.Context, refreshToken string) (Session, error)
    Logout(ctx context.Context) error
}

type ControlPlaneClient interface {
    Bootstrap(ctx context.Context) (Bootstrap, error)
    Models(ctx context.Context, knownVersion string) (CatalogEnvelope, error)
    SensitiveDictionary(ctx context.Context, knownVersion string) (DictionaryEnvelope, error)
}

// Read only. Gateway owns debit/settlement; client never submits an amount.
type UsageClient interface {
    Usage(ctx context.Context) (Usage, error)
    RequestStatus(ctx context.Context, clientRequestID string) (RequestStatus, error)
}
```

- `remote` 实现只存在于 `internal/productclient`；请求/响应以[中台交付包](../产品客户端与中台对接/中台交付包.md)的 OpenAPI 为准。
- `fake` 是测试 fixture，不是正式 local 服务或本地业务权威。既有 demo 登录/本地 provider 继续留在已有产品代码和 developer/demo profile，不迁入新包。
- `gateway` 接受 credential provider、受信 catalog/policy snapshot 和 `clientRequestId`；它只返回模型流与终态 usage，不计算价格、不写产品余额。
- `productpolicy` 只产生本地决定和审计字段；工具执行仍复用现有 permission/agent 机制。

## Server 挂点与合并约束

P0-01 先以 fake 验证 extension contract，不改动所有 handler。实际接入时：

- `server.Config` 仅追加可选的 production sender resolver；零值保持上游现有行为。
- resolver 被选择时，production session 忽略 `config.yml` endpoint、`ModelConfig` 和 provider/model 环境变量，拒绝 catalog 外 model。
- 登录、bootstrap、词库的既有 `product_*.go` handler 只将请求映射到 `productclient`；每处改动带 `OCTO-FORK` 标记。
- 不修改上游公开函数签名，不移动 route 注册，不把 HTTP/SSE/retry 逻辑塞进 server。

## B0/B1 拆分与验收

| PR | 内容 | 验收 |
| --- | --- | --- |
| B0 | `productclient` DTO/错误、fake、gateway sender contract、policy fixture 与 resolver interface；无真实 HTTP | 新包依赖方向正确；无 `Consume`；fake 覆盖成功/401/过期/取消/余额不足；既有 server 行为不变 |
| B1 | production profile 和 resolver 最小接入；优先只改 desktop，确有必要才触碰 P0-00 已批准的 server 挂点 | 开发 URL、环境 provider/model、local endpoint、config endpoint 都不能让 production session 绕过 gateway；profile 不能由环境变量、配置、缓存或 WebView 输入切换；developer/demo 回归仍通过 |

固定积分、假 token 和本地账号状态不是 B0/B1 的“兼容目标”。它们分别按 P0-02、P0-05、P0-08 的正式链路替换或删除；P0-01 不为将被删除的 mock 增加抽象。
