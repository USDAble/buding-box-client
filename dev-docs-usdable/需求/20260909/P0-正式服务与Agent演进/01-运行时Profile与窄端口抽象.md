# P0-01：产品客户端契约与最小 Server 挂点

| 项 | 内容 |
| --- | --- |
| 主责建议 | 客户端核心组 |
| 优先级 | P0-00/00A 完成后的首个功能 PR |
| 依赖 | [P0-00](00-Server最小改动与生产Profile前置.md)、[P0-00A](00A-生产入口可见性与功能保留.md)；不依赖中台真实服务 |
| 当前仓库范围 | 新建产品专属包、mock/contract、产品运行时装配；保留上游通用运行时，既有产品逻辑按 [P0-01A](01A-既有产品逻辑迁出internal-server.md) 迁出 |
| 需求基线 | [P0 正式上线需求基线](../P0-正式上线需求基线.md) R1、R3、R4、R5、R6、R7、R8、R11、R12 |

## 目标

建立清晰的客户端边界，而不是建立一个名为“backend”的第二套后端，也不建立与 `internal/app` 重叠的应用层。

1. 新建 `internal/productclient`，按身份、控制面、只读用量三个窄客户端定义 DTO、typed error 和 mock。
2. **不新建第二套 LLM 客户端**：中台网关的模型流复用 `internal/app` 的既有 provider 栈。`internal/productclient/gateway` 只新增中台特有的**终态查询/取消**与 `clientRequestId` 观察；SSE 解析、tool call、usage 聚合、重试、限流继续由 `internal/provider/{openai,anthropic}` 与 `internal/app` 承担。不提供 `Consume` 或任意客户端扣费能力。
3. 新建 `internal/productpolicy`，验证签名策略快照并在本地 PEP 执行能力决策。
4. 定义 `internal/credentialstore` 的最小接口，为 P0-08 提供实现位置。
5. 新建 `internal/productruntime`，承接既有布丁 HTTP/聊天策略的迁移和 production 装配；它优先由 desktop 构造，不把产品逻辑放回 `internal/server`。
6. **消费**（不新建）`internal/productprofile`：该包已随 P0-00 落地，按全局嵌入式配置固定 `production` 或 `developer`，不接受运行时切换；P0-01 只读取它，不重复定义 profile 语义与构建标签。

## 合同骨架

**本节是唯一的合同来源。** 其他文档（含[产品客户端与中台对接/开发计划](../产品客户端与中台对接/开发计划.md)）只引用本节，不得另行声明方法签名，否则 `02/03/04/06` 的 owner 会照着两份不同的合同写代码。**分工**：方法签名以本节为准；DTO 字段名、错误信封与 `code` 取值以[中台交付包 §3.2](../产品客户端与中台对接/中台交付包.md) 为唯一登记表（含客户端本地 fail-closed 码），本节不复述码表。

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
//
// 包内命名去掉 *Gateway* 前缀：`gateway.GatewayControlClient` 会重复，落点见
// internal/productclient/gateway/gateway.go。
type ControlClient interface {
    Cancel(ctx context.Context, clientRequestID string) error
    RequestStatus(ctx context.Context, clientRequestID string) (RequestStatus, error)
}
```

### 合同在代码中的落点（B0 已冻结，2026-09-11）

上面这段 Go 不再是"文档里的签名"，已落地为可编译、可测试的包；**方法签名以这些文件为准，本文不复述**：

| 位置 | 内容 |
| --- | --- |
| `internal/productclient/code.go` | `Code` 常量表（中台码 + 客户端本地 fail-closed 码）与唯一的 `*Error` 类型；`Retryable()` / `RetryAfter()` |
| `internal/productclient/envelope.go` | 成功/失败信封的唯一解码入口 `DecodeEnvelope`，错误映射集中在这一处 |
| `internal/productclient/dto.go` | 全部 wire DTO（字段名逐字引用[中台交付包](../产品客户端与中台对接/中台交付包.md) §4） |
| `internal/productclient/contract.go` | `AuthClient` / `ControlPlaneClient` / `UsageClient` / `SessionLister` |
| `internal/productclient/gateway/` | `ControlClient`（取消/状态）、`Observer`、`CallMeta` |
| `internal/productpolicy/` | `Verifier`、`Policy`（PEP）、`Signed`、版本单调与时钟窗口 |
| `internal/credentialstore/` | `Store` 最小接口（P0-08 实现） |
| `internal/productruntime/` | 装配根：`Deps` 全必填、`Preflight`、`Evaluate`、`Observe` |

两条**可执行**的约束（不是注释里的一句话）：

- `internal/productclient/code_test.go` 的 `TestEveryCodeIsRegisteredInTheDoc` 断言每个 `Code` 常量都出现在[中台交付包](../产品客户端与中台对接/中台交付包.md) §3.2 中 —— 登记表漂移会让测试失败，而不是等到前端漏映射一个错误码。
- `internal/productruntime/deps_test.go` 断言依赖方向：产品包不得 import `internal/server` / `internal/app` / `internal/provider`，且只有 `productclient` 可以 import `net/http`；`gateway` 包出现 `net/http` 即失败（与 `scripts/reuse-guard.mjs` 同一意图，但更早）。该测试**不带构建标签**，因此 `product_production` 下同样执行。

`mock` 包（`internal/productclient/mock`、`.../gateway/mock`）带 `//go:build !product_production`：一个能返回"登录成功、余额充足"的 mock 若可被链接进发行二进制，就等于"平台说可以"这件事能被一个构建标签伪造 —— 与已删除的假模型同一类风险，故发行构建里不存在该包（`go list -tags product_production ./internal/productclient/...` 只剩 `productclient` 和 `gateway`）。

**B0 未决、必须在 B1 之前定案的一项**（本文档先记录，不预先拍板）：`clientRequestId` **如何到达 wire**。`app.SenderOptions.Headers` 是构造期（每个 sender 一份），而 `clientRequestId` 是**每次调用**一份，两者粒度不同。候选形态有二：① 每次调用构造一个 sender（header 随调用固定，代价是连接复用按调用粒度重建）；② 在 sender 之上做一个 decorator。若选 ②，必须遵守 `gateway.Observer` 的能力保持契约 —— `internal/productclient/gateway/capabilities.go` 的 `CapabilitiesOf`/`PreservesCapabilities` 就是为此提供的探针，因为 agent loop 是用**五处独立类型断言**（`agent.go:770/848/948/1617`）探测能力的，一个只返回 `agent.Sender` 的 decorator 不会编译失败、不会报错，只会静默地丢掉流式、工具或标题生成。

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
5. 终态控制（Cancel / RequestStatus）走 `gateway.ControlClient`（中台独有）
```

> 字段名以上游 `app.SenderOptions` 为准（`Provider` / `Protocol` / `APIKey` / `BaseURL` / `Headers` / `RPM` / `MaxConcurrency`）。`Protocol` 对 `custom` 供应商是必填 —— 中台网关用 `"openai"`。

**判据（§3.5）**：任何在 `internal/productclient/gateway` 里出现的 SSE 解析、JSON 分片拼接、token 计数或 HTTP 重试代码，都必须先回答"为什么 `internal/provider/openai` 不能做这件事"。答不出来就是重复实现，必须删掉改走 `app.NewSender`。

- `remote` 实现只存在于 `internal/productclient`；请求/响应以[中台交付包](../产品客户端与中台对接/中台交付包.md)的 OpenAPI 为准。
- 方法覆盖与需求基线的对应关系：`AuthClient`→R3/R8；`ControlPlaneClient`（含 `LegalDocuments`/`AcceptLegal`）→R4/R11；`UsageClient`（含只读 `Ledger`）→R5/R10；`gateway.ControlClient`→R5（模型流部分复用既有 `agent.Sender`，见「gateway 的复用形态」）。新增方法前先更新本节与需求基线。
- `mock` 是测试 contract sample，不是正式 local 服务或本地业务权威。既有 developer 登录/本地 provider 继续留在 developer 的本地功能集合中，不迁入新包。
- `gateway` 接受 credential provider、受信 catalog/policy snapshot 和 `clientRequestId`；它把既有 `agent.Sender` 的输出转成会话事件与终态 usage，不计算价格、不写产品余额、不解析 SSE。
- `productpolicy` 只产生本地决定和审计字段；工具执行仍复用现有 permission/agent 机制。

### 中台请求客户端的实现形态（`internal/productclient` 的 "how"）

前面两节规定了**合同**（方法、DTO、错误码）和**中台侧义务**。本节补上此前缺失的一环：**客户端自己怎么发这个请求**。之所以必须写下来，是因为 §合同骨架只回答了"调用什么"，而实现者会各自发明一套 —— 于是同一个 base URL 来源、同一个 401 刷新、同一个验签会出现三份实现（§3.5/§3.8）。

#### 1. 中台地址从哪里来（**生产必须是编译期常量，不是配置**）

这是本节最要紧的一条。`AuthClient`/`ControlPlaneClient` 的 base URL **在 production 不得来自** `config.yml`、环境变量、`product-state.json`、缓存或 WebView 输入 —— 与 §3.10 的 production 拒绝矩阵一致。

推导：bootstrap 自身需要地址才能发起，所以地址**不可能**由远端下发（先有鸡还是先有蛋）。因此：

```text
production 的 api-host  =  嵌入式 profile 的一部分（与 product_production 标签一同编译进二进制）
developer/test         =  可覆盖（本地联调、sandbox、mock），沿用既有开发通道
```

**需要落地的改动**：`internal/productprofile` 的 `Profile` 目前只有 `allowDevWebview` / `allowEnvironmentModelSource` / `allowDataRootOverride` / `startup` 四个字段，**没有任何 api-host 字段**。需要增加（P0-00 的 profile schema）：

| 字段 | 含义 |
|---|---|
| `apiHost` | 正式 API 主机（`https://<host>/v1`） |
| `gatewayHost` | 模型网关主机（可与 `apiHost` 同源） |
| `trustedKeyIDs` | 受信签名公钥的 keyId → 公钥（或指向编译期嵌入的公钥表） |

**fail-closed**：production 构建若 `apiHost` 为空 → **拒绝启动并发起任何中台请求**，而不是回退到任何默认值、`localhost` 或 `config.yml`。

#### 2. 一个共享 HTTP transport，不是每个 client 一套

`AuthClient`、`ControlPlaneClient`、`UsageClient`、`gateway.ControlClient` 的**控制面调用**共用一个 `internal/productclient` 内部的 HTTP 底座，集中实现：

| 关注点 | 规定 |
|---|---|
| 传输 | 单一 `http.Client`（连接复用），`https` only；不得明文 HTTP（对齐[中台交付包](../产品客户端与中台对接/中台交付包.md) §3.1） |
| 超时 | 见下表；不得依赖默认零值（零 = 永不超时） |
| 公共头 | `Authorization: Bearer`、`X-Client-Version`、`X-Client-Platform`、`X-Client-Arch`、`X-Install-Id`、`Accept-Language` 集中注入，不在各方法里手写 |
| 错误映射 | HTTP 状态 + 机器码 → typed error，集中一处；`message` 永不作为文案（只按 `code` 映射） |
| 401 刷新单飞 | **在底座里拦截**：并发 401 只触发一次 `Refresh`，其余请求等待同一结果；刷新失败统一清凭证并回登录页。不能在每个 client 里各写一份 |
| 幂等 | 所有创建副作用的请求自动带 `Idempotency-Key` = `clientRequestId`（对齐中台交付包 §3.3） |

**这一条与 `reuse-guard` 的关系（避免误读）**：`reuse-guard` 禁止 `net/http` 的是 `internal/productclient/gateway` —— 因为**模型流**必须复用 `internal/app` 的 provider 栈。`internal/productclient`（认证/控制面/用量）**本来就该自己发 HTTP**：那些是普通 JSON 接口，上游没有对应能力可复用。两者不矛盾，不要因为看到守卫就以为整个 `productclient` 不能出现 `net/http`。

#### 3. 超时与重试（有界，按接口语义定）

| 调用 | 超时 | 重试 |
|---|---|---|
| `SendCode` / `Login` | 10s | **不自动重试**（中台交付包 §3.3） |
| `Refresh` | 10s | 单飞内一次；失败即清凭证 |
| `Bootstrap` / `Models` / `SensitiveDictionary` / `LegalDocuments` | 15s | 有界退避（≤2 次），带 `If-None-Match` |
| `Usage` / `RequestStatus` / `Ledger` | 10s | 只读，可退避重试 |
| `Cancel` | 10s | 幂等，可重试 |
| 模型流 | 由 `internal/provider` 与 `context` 决定 | **不在此处实现** |

统一原则：退避上限、总时长上限都必须有明确数值并写进实现（§3.9 有界降级）；`429` 一律按 `retryAfterSec` 冷却，`5xx` 先查 `clientRequestId` 状态再决定是否重发。

#### 4. 签名验签只实现一次

`catalog` / `capabilities` / `policy` / 词库四个信封**共用一套**验签 + 缓存 + 刷新（owner 见 [P0-开发顺序与协作计划](P0-开发顺序与协作计划.md) §3 的 M2 消歧）：

- 受信公钥来自**嵌入式 profile**（§1 的 `trustedKeyIDs`），不从网络获取 —— 否则验签失去意义。
- 规范化（JCS）与 `keyId` 选择集中实现；未知 `keyId` = fail-closed，不是"忽略验签继续"。
- 四个信封的差异只在 **DTO 与合并规则**，不在验签。
- **不引入代码生成依赖**：本项目禁止无理由新增三方依赖（`.octorules`）。中台交付的 OpenAPI 用于**产出 contract sample 与测试断言**，客户端实现**手写** DTO —— 见下方 §合并方式。

#### 5. 与 `productruntime` 的边界

`productruntime` 只**调用**上面这些窄接口并做产品映射；它不持有 HTTP 细节、不自己拼 URL、不自己加头。`internal/server` 完全不参与（`01A` 负责把既有产品 HTTP 迁出）。


P0-01 先以 mock 验证 contract，不改动所有 handler。实际接入时：

- `productprofile` 在读取数据根、`serve.env` 和构造 server 前确定不可运行时切换的 Profile；它不删除 `config.yml`、MCP、channel、工具或后台能力。P0-05 负责使正式会话不以本地模型来源绕过网关。
- `productruntime` 只调用 `productclient`/`gateway`/`productpolicy`/`credentialstore` 的窄接口，并持有产品 HTTP 映射和聊天前置策略；HTTP/SSE/retry 不进入 server。
- 既有 `product_*.go`、积分 mock、敏感词/隐私聊天钩子和 `apiProduct` 路由改写按 [P0-01A](01A-既有产品逻辑迁出internal-server.md) 分阶段处理。迁移期如确实需要 server 协作，只能使用 P0-00 单独批准的中性扩展端口，不能追加 product-specific `server.Config` 字段。`ensureLocalEndpoint()` 与假模型种入已删除，不再属于迁移项。
- 不修改上游公开函数签名，不移动通用 route 注册，不重写聊天/WS 生命周期。

## B0/B1 拆分与验收

| PR | 内容 | 验收 |
| --- | --- | --- |
| B0 | `productruntime` contract（消费 P0-00 已落地的 `productprofile`）、`productclient` DTO/错误、mock、gateway sender contract、policy contract sample；无真实 HTTP。**profile 需含 `apiHost`/`gatewayHost`/`trustedKeyIDs`（P0-01 §1）**，且 production 缺失时 fail-closed | 新包依赖方向正确；无 `Consume`；mock 覆盖成功/401/过期/取消/余额不足；本节合同与 `开发计划` §3 零分歧；既有 server 行为不变 |
| B1 | production profile 和 productruntime 最小装配；优先只改 desktop，确有必要才申请 P0-00 的通用 server 端口 | 开发 URL、环境 provider/model、`config.yml` endpoint/本地 provider 都不能让 production session 绕过 gateway；profile 不能由环境变量、配置、缓存或 WebView 输入切换；developer 回归仍通过（developer/test 保留 `config.yml` 配第三方 endpoint/URL/key 的能力） |

固定积分、旧本地 token 和本地账号状态不是 B0/B1 的“兼容目标”。它们分别按 P0-02、P0-05、P0-08 的正式链路替换或删除；P0-01 不为将被删除的 mock 增加抽象。
