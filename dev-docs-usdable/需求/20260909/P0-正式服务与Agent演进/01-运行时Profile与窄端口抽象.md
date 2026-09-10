# P0-01：产品客户端契约与最小 Server 挂点

| 项 | 内容 |
| --- | --- |
| 主责建议 | 客户端核心组 |
| 优先级 | P0-00/00A 完成后的首个功能 PR |
| 依赖 | [P0-00](00-Server最小改动与生产Profile前置.md)、[P0-00A](00A-生产入口可见性与功能保留.md)；不依赖中台真实服务 |
| 当前仓库范围 | 新建产品专属包、合同测试实现/contract、产品运行时装配；保留上游通用运行时，既有产品逻辑按 [P0-01A](01A-既有产品逻辑迁出internal-server.md) 迁出 |
| 需求基线 | [P0 正式上线需求基线](../P0-正式上线需求基线.md) R1、R3、R4、R5、R6、R7、R8、R11、R12 |

## 目标

建立清晰的客户端边界，而不是建立一个名为“backend”的第二套后端，也不建立与 `internal/app` 重叠的应用层。

1. 新建 `internal/productclient`，按身份、控制面、只读用量三个窄客户端定义 DTO、typed error 和 合同测试实现。
2. **不新建第二套 LLM 客户端**：中台网关的模型流复用 `internal/app` 的既有 provider 栈。`internal/productclient/gateway` 只新增中台特有的**终态查询/取消**与 `clientRequestId` 观察；SSE 解析、tool call、usage 聚合、重试、限流继续由 `internal/provider/{openai,anthropic}` 与 `internal/app` 承担。不提供 `Consume` 或任意客户端扣费能力。
3. 新建 `internal/productpolicy`，验证签名策略快照并在本地 PEP 执行能力决策。
4. 定义 `internal/credentialstore` 的最小接口，为 P0-08 提供实现位置。
5. 新建 `internal/productruntime`，承接既有布丁 HTTP/聊天策略的迁移和 production 装配；它优先由 desktop 构造，不把产品逻辑放回 `internal/server`。
6. **消费**（不新建）`internal/productprofile`：该包已随 P0-00 落地，按全局嵌入式配置固定 `production` 或 `developer`，不接受运行时切换；P0-01 只读取它，不重复定义 profile 语义与构建标签。

## 合同骨架

**本节是唯一的合同来源。** 其他文档（含[产品客户端与中台对接/开发计划](../产品客户端与中台对接/开发计划.md)）只引用本节，不得另行声明方法签名，否则 `02/03/04/06` 的 owner 会照着两份不同的合同写代码。

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
    LegalDocuments(ctx context.Context, knownVersion string) (LegalEnvelope, error)
    AcceptLegal(ctx context.Context, req AcceptLegalRequest) (LegalReceipt, error)
}

// Read only. Gateway owns debit/settlement; client never submits an amount.
type UsageClient interface {
    Usage(ctx context.Context) (Usage, error)
    RequestStatus(ctx context.Context, clientRequestID string) (RequestStatus, error)
    Ledger(ctx context.Context, query LedgerQuery) (LedgerPage, error)
}

// internal/productclient/gateway: 中台特有的终态控制面。
// 注意：模型流本身不在这里 —— 它复用 internal/app 的既有 provider 栈
// （见下方「gateway 的复用形态」）。本接口只覆盖中台独有、上游没有的两件事。
type GatewayControlClient interface {
    Cancel(ctx context.Context, clientRequestID string) error
    RequestStatus(ctx context.Context, clientRequestID string) (RequestStatus, error)
}
```

### gateway 的复用形态（不要把已有能力再写一遍）

**上游已经具备的能力，P0-03 一律不重写。** 中台网关是 OpenAI 兼容的 `chat/completions`，而本仓库已有：

| 已存在 | 位置 | 覆盖了 P0-03 的哪部分 |
| --- | --- | --- |
| 任意 `BaseURL` + 任意请求头 | `config.Endpoint`、`app.ProviderCustom` | 指向中台网关、注入产品 token/`clientRequestId` 头 |
| SSE 流式聚合 | `internal/provider/openai`（含 `[DONE]` 缺失容忍） | `meta`/`delta`/`tool_call`/`usage` 的解析 |
| tool call 分片拼接 | `internal/provider/openai`（按 `index` 拼接 JSON 片段） | `tool_call` 事件 |
| usage 归一（含 cache 语义差异） | `internal/provider/openai`（`apiUsage.nonCachedInput`） | `usage` 事件、`settled` 判定 |
| 限流 | `config.Endpoint.RPM` + `buildClient` limiter | 客户端侧限流 |
| 取消 | `context` 取消 → `ChatStream` 关闭 | `Cancel` 的流侧 |

因此正确形态是**装配 + 装饰**，不是新建 client：

```text
1. productruntime 解析凭证与策略快照
2. 构造 app.SenderOptions{
       Provider: app.ProviderCustom,      // 既有自定义 endpoint 通道
       Protocol: "openai",                // Custom 必须显式指定 wire 协议
       BaseURL:  <中台网关地址（来自受信配置，非 config.yml>）,
       APIKey:   <产品 access token>,
       Headers:  {"X-Client-Request-Id": id, ...},
       RPM:      <目录/策略给定的限流>,
   }
3. s, err := app.NewSender(opts)          // ← 复用；app 是构造 provider client 的唯一位置
4. sender = gateway.NewObserver(s)        // ← 唯一新增：终态/usage/错误码 → 会话事件
5. 终态控制（Cancel / RequestStatus）走 GatewayControlClient（中台独有）
```

> 字段名以上游 `app.SenderOptions` 为准（`Provider` / `Protocol` / `APIKey` / `BaseURL` / `Headers` / `RPM` / `MaxConcurrency`）。`Protocol` 对 `custom` 供应商是必填 —— 中台网关用 `"openai"`。

**判据（§3.5）**：任何在 `internal/productclient/gateway` 里出现的 SSE 解析、JSON 分片拼接、token 计数或 HTTP 重试代码，都必须先回答"为什么 `internal/provider/openai` 不能做这件事"。答不出来就是重复实现，必须删掉改走 `app.NewSender`。

- `remote` 实现只存在于 `internal/productclient`；请求/响应以[中台交付包](../产品客户端与中台对接/中台交付包.md)的 OpenAPI 为准。
- 方法覆盖与需求基线的对应关系：`AuthClient`→R3/R8；`ControlPlaneClient`（含 `LegalDocuments`/`AcceptLegal`）→R4/R11；`UsageClient`（含只读 `Ledger`）→R5/R10；`GatewayControlClient`→R5（模型流部分复用既有 `agent.Sender`，见「gateway 的复用形态」）。新增方法前先更新本节与需求基线。
- `合同测试实现` 是测试 contract sample，不是正式 local 服务或本地业务权威。既有 developer 登录/本地 provider 继续留在 developer 的本地功能集合中，不迁入新包。
- `gateway` 接受 credential provider、受信 catalog/policy snapshot 和 `clientRequestId`；它把既有 `agent.Sender` 的输出转成会话事件与终态 usage，不计算价格、不写产品余额、不解析 SSE。
- `productpolicy` 只产生本地决定和审计字段；工具执行仍复用现有 permission/agent 机制。

## 运行时装配与 Server 约束

P0-01 先以 合同测试实现 验证 contract，不改动所有 handler。实际接入时：

- `productprofile` 在读取数据根、`serve.env` 和构造 server 前确定不可运行时切换的 Profile；它不删除 `config.yml`、MCP、channel、工具或后台能力。P0-05 负责使正式会话不以本地模型来源绕过网关。
- `productruntime` 只调用 `productclient`/`gateway`/`productpolicy`/`credentialstore` 的窄接口，并持有产品 HTTP 映射和聊天前置策略；HTTP/SSE/retry 不进入 server。
- 既有 `product_*.go`、积分旧本地实现、敏感词/隐私聊天钩子和 `apiProduct` 路由改写按 [P0-01A](01A-既有产品逻辑迁出internal-server.md) 分阶段处理。迁移期如确实需要 server 协作，只能使用 P0-00 单独批准的中性扩展端口，不能追加 product-specific `server.Config` 字段。`ensureLocalEndpoint()` 与假模型种入已删除，不再属于迁移项。
- 不修改上游公开函数签名，不移动通用 route 注册，不重写聊天/WS 生命周期。

## B0/B1 拆分与验收

| PR | 内容 | 验收 |
| --- | --- | --- |
| B0 | `productruntime` contract（消费 P0-00 已落地的 `productprofile`）、`productclient` DTO/错误、合同测试实现、gateway sender contract、policy contract sample；无真实 HTTP | 新包依赖方向正确；无 `Consume`；合同测试实现 覆盖成功/401/过期/取消/余额不足；本节合同与 `开发计划` §3 零分歧；既有 server 行为不变 |
| B1 | production profile 和 productruntime 最小装配；优先只改 desktop，确有必要才申请 P0-00 的通用 server 端口 | 开发 URL、环境 provider/model、`config.yml` endpoint/本地 provider 都不能让 production session 绕过 gateway；profile 不能由环境变量、配置、缓存或 WebView 输入切换；developer 回归仍通过（developer/test 保留 `config.yml` 配第三方 endpoint/URL/key 的能力） |

固定积分、旧本地 token 和本地账号状态不是 B0/B1 的“兼容目标”。它们分别按 P0-02、P0-05、P0-08 的正式链路替换或删除；P0-01 不为将被删除的 旧本地实现 增加抽象。
