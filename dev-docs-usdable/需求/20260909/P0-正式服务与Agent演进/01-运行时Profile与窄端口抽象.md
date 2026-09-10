# P0-01：运行时 Profile 与窄端口抽象

> **关于本文的名字（D4 决策，2026-09-11）**：本文在 2026-09-09 评审时的 H1 是「产品客户端契约与最小 Server 挂点」，但**文件名**一直是 `01-运行时Profile与窄端口抽象.md`，`README.md` 里又用「`productclient` / gateway / policy / credential store 合同」描述它 —— 三种叫法指的是同一份文档，引用时无法判断是不是同一处。
>
> **canonical 名 = 文件名去掉扩展名：`01-运行时Profile与窄端口抽象`，简称「P0-01」。** 其他叫法一律不再使用。H1 已改为与文件名一致。之所以**改标题而不改文件名**：全文有 12 处链接指向该路径（`grep -rn "01-运行时Profile与窄端口抽象.md"`），改名要同步 12 处且每处都可能漏；而文档标题只是人读的入口，与链接稳定性无关。内容范围以「合同骨架」一节为准 —— 标题里的「窄端口」只是本文的一个子话题，不是全文范围。

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

// 可选的设备会话面（中台交付包 §4.2.4）。与 AuthClient 分开：只需要登录的调用方
// 不必依赖会话吊销，两者也可以由不同 PR 拥有。
type SessionLister interface {
    Sessions(ctx context.Context) ([]DeviceSession, error)
    RevokeSession(ctx context.Context, sessionID string) error
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
| `internal/productpolicy/` | `Verifier`（**端口声明**，见下）、`Policy`（PEP）、`Signed`、`CompareVersions`、时钟窗口与 fail-closed 判定 |
| `internal/credentialstore/` | `Store` 最小接口：`Load`（无会话返回 `ErrNotFound`）/ `Save`（替换）/ `Clear`（幂等）；只持久化 refresh token。已随 B0 落地，**方法名以代码为准**（P0-08 实现平台后端） |
| `internal/productruntime/` | 装配根：`Deps` 全必填、`Preflight`、`Evaluate`、`Observe`、**构造 `productpolicy.Verifier` 的 adapter** |

**信封验签的归属（消歧，避免 01 / 04 / 协作计划各写一版）**：验签的**唯一实现必须服务两个信封** —— 策略/目录信封（P0-04）与词库信封（P0-06）。但 `productpolicy` 已经 import `productclient`（`Signed` 里有 `productclient.Catalog`），所以"把实现放进 `productclient` 并返回 `Signed`"会构成**import 环**。落地形态固定为：

```text
productprofile.TrustedKeyIDs ──┐
                               ▼
  internal/productclient  通用信封验签（ed25519 + JCS 归一 + `signature` 剥离 + keyId 查表）  ← owner 02，只此一份
                               │  verified payload + keyID
                               ▼
  internal/productruntime  adapter：构造 productpolicy.Signed            ← 装配根
                               ▼
  internal/productpolicy  Verifier 端口（Policy/Evaluate 消费；audience / 窗口 / 版本单调在这里）  ← owner 04，不含 crypto
```

即 **crypto 的实现 owner 是 02**（与协作计划 §3「M2 信封的唯一 owner」一致），`04`/`06` 都不得再写一套；密钥由 `productruntime` 从 `productprofile` 注入 `productclient`（`productclient` 是叶子包，`deps_test.go` 不允许它 import `productprofile`）。

两条**可执行**的约束（不是注释里的一句话）：

- `internal/productclient/code_test.go` 的 `TestEveryCodeIsRegisteredInTheDoc` 断言每个 `Code` 常量都出现在[中台交付包](../产品客户端与中台对接/中台交付包.md) §3.2 中 —— 登记表漂移会让测试失败，而不是等到前端漏映射一个错误码。
- `internal/productruntime/deps_test.go` 断言依赖方向：**产品包**（`productPackages` 列表，含 `productruntime` 自身、`productclient` 及其子包）不得 import `internal/server` / `internal/app` / `internal/provider`（`bannedImports`）。net/http 一侧由 `TestOnlyTheTransportLayerSpeaksHTTP` 断言，它**只拦 `internal/productclient/gateway`**：该包出现 `net/http` 或 `net/http/*` 即失败（与 `scripts/reuse-guard.mjs` 同一意图，但更早）。**不要把它读成"只有 `productclient` 可以 import `net/http`"** —— 没有测试做这个全局断言，`productpolicy` / `productruntime` 当前不受 net/http 约束；该缺口已登记在[问题盘点](../问题盘点.md)，不假装被覆盖。该测试**不带构建标签**，因此 `product_production` 下同样执行。

`mock` 包（`internal/productclient/mock`、`.../gateway/mock`）带 `//go:build !product_production`：一个能返回"登录成功、余额充足"的 mock 若可被链接进发行二进制，就等于"平台说可以"这件事能被一个构建标签伪造 —— 与已删除的假模型同一类风险，故发行构建里不存在该包（`go list -tags product_production ./internal/productclient/...` 只剩 `productclient` 和 `gateway`）。

**B1 已定案（2026-09-11）：`clientRequestId` 通过"每次网关调用构造一个 sender"送达 wire，不用装饰器。** 起因是粒度不匹配：`app.SenderOptions.Headers` 是**构造期**（每个 sender 一份），而 `clientRequestId` 是**每次调用**一份。备选方案是在 sender 之上做 decorator，被否掉的理由是 `agent` 的能力不是一条链而是**五处独立类型断言**（`agent.go:770/848/948/1617` 的 `StreamingSender`/`ToolSender`/`ToolStreamingSender`/`NoReasoningSender`/`LowEffortSender`），手工装饰器要逐层追这套会**持续增长**的接口集合，漏掉一个不会编译失败、不会报错，只会静默丢掉流式、工具或标题生成；而且 `NoReasoning()`/`LowEffort()` 返回的是 `agent.Sender`，装饰器还得再包一层才不把身份丢掉。

因此 B1 的形态是：每次调用用该次的 `clientRequestId` 构造 sender（`Headers` 随调用固定），调用结束即弃。代价是连接复用按调用粒度重建 —— 相比控制面，模型流本身是长连接，这个代价可接受。`internal/productclient/gateway/capabilities.go` 的 `CapabilitiesOf`/`PreservesCapabilities` 仍然保留：它是这个决定的安全网（一旦有人日后引入装饰器，`gateway_test.go` 里的契约测试会立刻报出能力丢失）。

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

因此正确形态是**装配 + 装饰**，不是新建 client。

**装配点是宿主 `cmd/octo-desktop`**（它实现 `runtimeport.SenderFactory`，见 [01A §3.1](01A-既有产品逻辑迁出internal-server.md)）。`productruntime` 只**持有**依赖、做观察与判定，**不构造 provider client** —— `internal/productruntime/deps_test.go` 禁止任何产品包（含 `productruntime` 自身与 `productclient/gateway`）import `internal/app`，所以"由 productruntime 调 `app.NewSender`"写在文档里也实现不出来。真实调用链：

```text
productSenderFactory(p) → productSender.SenderForTurn(ctx, req)
  │
  1. 向 productruntime 取这次调用的受信参数：网关地址（profile.GatewayHost，
     不是 config.yml）、产品 access token、目录/策略给定的限流，以及**这次调用**
     的 clientRequestId（每次网关调用一个，见本节末 B1 定案）
  2. 构造 app.SenderOptions{ Provider: app.ProviderCustom, Protocol: "openai", ... }
  3. base, err := app.NewSender(opts)   // ← 复用；app 是构造 provider client 的唯一位置
  4. sender, err := rt.Observe(base, meta, sink)
     // ← 唯一新增：终态/usage/错误码 → 会话事件。签名在
     //    internal/productruntime/runtime.go：
     //    Observe(agent.Sender, gateway.CallMeta, gateway.TerminalSink) (agent.Sender, error)
  5. 终态控制（Cancel / RequestStatus）走 productruntime → `gateway.ControlClient`（中台独有）
```

> 字段名以上游 `app.SenderOptions` 为准（`Provider` / `Protocol` / `APIKey` / `BaseURL` / `Headers` / `RPM` / `MaxConcurrency`）。`Protocol` 对 `custom` 供应商是必填 —— 中台网关用 `"openai"`。

> **没有 `gateway.NewObserver` 这个函数，也不要有。** 冻结合同是一个接口一次 `Wrap`：`gateway.Observer.Wrap(base, meta, sink) agent.Sender`（`internal/productclient/gateway/gateway.go`），由 `productruntime.Observe` 调用，实现经 `productruntime.Deps.Observer` 注入。本文与 `03`/`05` 早先的伪代码写作 `gateway.NewObserver(s)` 且漏了 `meta`/`sink` 两个必需参数，照着写编译不过 —— 这是"文档说 A、代码是 B"，以代码为准。
>
> **`meta` 与 `sink` 由谁提供**：`meta`（`gateway.CallMeta`）是这次网关调用的身份（`clientRequestId` 等），由装配点在构造这次调用时就地产生；`sink`（`gateway.TerminalSink`）**由宿主实现** —— `cmd/octo-desktop` 把 `OnMeta`/`OnTerminal` 转成会话事件推给 UI。sink 只报事实、不做决定：用户可见的"已结算/已冲正/待对账"呈现与余额刷新发生在这个边界之上。哪个调用点传哪个 sink、绑到哪条会话是 P0-05 的接线，P0-03 只负责 contract 与观察实现。

**判据（§3.5）**：任何在 `internal/productclient/gateway` 里出现的 SSE 解析、JSON 分片拼接、token 计数或 HTTP 重试代码，都必须先回答"为什么 `internal/provider/openai` 不能做这件事"。答不出来就是重复实现，必须删掉：`gateway` 不允许出现 `net/http`（`deps_test.go` 与 `scripts/reuse-guard.mjs` 双重拦截），它只做装配与观察，SSE 由装配点复用的 provider 栈承担。

> **这条判据的实际覆盖范围**：`reuse-guard` 的 `REUSE_SCOPES` 目前**只有 `internal/productclient/gateway` 一条**，而真正做装配的 `cmd/octo-desktop` 是独立 `go.mod` 的嵌套模块 —— 根模块的 `go build ./...`、`go test ./...` 与 `deps_test.go` 都到不了它。即：**"复用 `internal/app` 而非自写 HTTP/SSE"这个决策的落点，是全仓唯一没有守卫覆盖的目录。** 待补，见[问题盘点](../问题盘点.md)。

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

**`trustedKeyIDs` 的注入 owner 与时间点（2026-09-11 定）**：B0/B1 期间 `production.json` 里它是 `{}`，于是 `release-config-guard` 每次出包都会报「trustedKeyIDs is empty — no signed policy or catalog can be verified」。**这个警告是预期的，不是待修的缺陷**，但它必须有人接手，否则会在几个月后变成永久噪音以致被无视 —— 那这个 guard 就死了。因此把它写死在这里：

| 项 | 值 |
| --- | --- |
| 注入内容 | 中台**签名服务**的 ed25519 公钥（keyId → base64 32 字节公钥），**至少一把、建议两把**（轮换期需要新旧并存，只放一把则换钥当日所有老客户端同时失效） |
| owner | **中台/服务端**（签名服务与公钥的持有方）—— 客户端只是消费方，不由客户端研发决定公钥内容 |
| 时间点 | **P0-04 目录/策略验签接通之前**（`productpolicy.Verifier` 的第一次真实实现）。在此之前 `{}` 是自洽的：没有真公钥就没有真目录，`Preflight` 一律 fail-closed |
| 交付形式 | 更新 `internal/productprofile/profiles/production.json` 的 `trustedKeyIDs` + 一次 `make release-config-check` 转绿 —— **是发布动作（改数据），不是代码改动** |
| 未注入时的行为 | 出包**允许**（advisory 只告警不阻断，理由见 `运行时Profile配置.md`）；但该包**不能验签任何策略**，因而拿不到任何模型目录 —— 它是内部测试包，不是可发布包 |
| 如何防止被遗忘 | `问题盘点.md` §0.1 已把「发布面无人认领的项」单列，本项是其中第 6 项；**P0-04 开工清单的第一条就是检查它是否已注入** —— 而不是等出包时被 guard 提醒（那时离发布已经很近，且 guard 只告警不阻断，很容易被放过） |

**fail-closed 分两种状态，别混为一谈。** `Validate()` 只校验"非空 + https + 绝对 URL"，所以 `https://api.invalid/v1` 这种占位**能通过校验、进程也能启动** —— 把它读成"占位会拒绝启动"会让实现者以为不用处理首屏：

| 出厂状态 | `Validate()` | 进程 | 首屏 |
| --- | --- | --- | --- |
| `apiHost` **为空** | 失败 | **拒绝启动** | 不适用（进程未起） |
| `apiHost` = `.invalid` 占位（当前 `production.json` 的形态） | 通过 | **可以启动** | 立即呈现"未配置/不可用"：`ControlPlaneConfigured() == false`，`Preflight` 一律 fail-closed。不是等第一个回合失败 |
| 真实主机 + 受信公钥 | 通过 | 启动 | 正常 |

占位能启动是**故意**的：一个开不了机的桌面客户端连登录页都显示不出来，用户只看到进程闪退，没有任何可采取的动作。所以 fail-closed 落在 `ControlPlaneConfigured()`（每个回合都拒绝）而不是 `Validate()`（拒绝进程）。**发布门由 `release-config-guard` 承担**：它报 `apiHost`/`gatewayHost` 仍是占位、或 `trustedKeyIDs` 为空 —— 即"占位包是内部测试包，不是可发布包"（该 guard 是 advisory，理由见 `运行时Profile配置.md`）。任何情况下都**不得**回退到默认值、`localhost` 或 `config.yml`。

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

**装配关系（避免 P0-03 / P0-05 各写一套）**：构造 provider client 的唯一位置是 `internal/app`，而**调用它的唯一位置是宿主 `cmd/octo-desktop`**（见 §gateway 的复用形态）。`productruntime` **不得** import `internal/app`（`deps_test.go` 的 `bannedImports` 会针对它变红），它只提供这次网关调用所需的受信参数，以及 `Observe` 这个装饰入口。


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

### B1.0 绕过面清单（2026-09-11 侦察，**动手前必须逐行定案**）

B1 的验收句是「这些输入都不能让 production session 绕过 gateway」。但把这句话变成代码之前，得先知道**到底有哪几个入口**——否则 B1 会变成「发现一个堵一个」的一串散改，而每一笔都可能落在上游文件上。以下是在当前 `buding` 上实际能读到 provider/model 的入口（`文件:行号` 均为实测）：

| # | 入口 | 位置 | 在 production 下必须的结果 | 建议的封堵方式 |
| --- | --- | --- | --- | --- |
| 1 | `OCTO_PROVIDER` | `internal/server/server.go:1649`（`firstNonEmpty(flagProvider, os.Getenv("OCTO_PROVIDER"), entry.Provider, "anthropic")`） | 被忽略 | **上游文件。** 走端口 4（宿主 sender 工厂），不是在 handler 里写 `if production`。注意：只把 `Config.Provider` 设为非空**并没有封堵** —— provider 名一旦非空，凭据就改由 `resolveAPIKey` 从厂商环境变量或 `data/config.yml` 取，只是换了入口 |
| 2 | `OCTO_PROVIDER`（CLI 路径） | `cmd/octo/config.go:47` | 只影响 CLI，不影响便携包 | **已核实：出 B1 范围。** `package-portable.mjs` 断言顶层**有且仅有一个** exe 且必须是品牌 exe（`unexpected top-level exe` / `missing … at package root` 两处硬失败），`bin/` 下只捆绑 `uv.exe` —— 便携包不含 `octo` 可执行文件，所以这个入口发不出去，不要为它改代码 |
| 3 | `config.yml` 的 endpoint 条目 | `internal/server/server.go:1649` 的 `entry.Provider`；`config/endpoints` 路由 | endpoint 不参与 provider/model 选择 | 同 #1（同一处代码）；且标准产品 UI 不提供添加入口（D6/C7 已定） |
| 4 | 本地 provider（`internal/provider/local`） | 已用 `product_production` 编译排除（2026-09-11） | 不可达 | **已完成**，由 `package-portable.mjs` 的 `checkProductionBinary()` 反向断言 |
| 5 | **会话绑定的 `ModelConfig`** | `internal/server/server.go:1768` `senderForSession`：读 `sess.ModelConfig` → `config.LoadCached()` → `cachedSenderForEntry`，**构建失败还静默回落到默认 sender** | 被忽略；且**不得回落** | **端口 4 存在的真正理由。** 输入是已落盘的会话 JSON + `data/config.yml`，都不经过 `server.Config`，所以 desktop 侧关不掉（除非去改写上游的会话结构）。申请见 [01A §3.1](01A-既有产品逻辑迁出internal-server.md) |
| 6 | `OCTO_DESKTOP_DEV_URL` | `cmd/octo-desktop/main.go:90`（仅 `AllowDevWebview` 为真时） | 被忽略 | 已由 profile 门控（`main.go:89`）；B1 只需补一条 production 反向测试 |
| 7 | `OCTO_DATA_ROOT` | `internal/datapath` | **保持可用**（测试/只读安装需要），但它只改数据根、不改 profile | 不封堵；但要用测试断言它**不能**切换 profile、也不能让 production 读到 developer profile |
| 8 | `data/product-state.json` / 缓存 | 见 P0-08 | 不能被当作已登录，不能携带 provider 配置 | 归 P0-08；B1 只断言「profile 不从缓存读」 |
| 9 | WebView 输入 | 见 P0-02 | 不能改 profile / 不能配 endpoint | 归 P0-02；B1 只断言 profile 只来自编译期嵌入 |
| **10** | **视觉描述器**（`vision_helper`） | `internal/server/server.go:1454`（`buildAgent`，WS/REST/scheduled）与 `:2724`（`buildChannelAgent`，IM）：`a.SetImageDescriber(app.NewVisionDescriber(a, cfg))`，而 `internal/app/vision.go:71` 用 `entry.Provider`/`entry.BaseURL` 直接 `NewSender` | 不构造；描述器为 `nil`（图片不作描述，无外发） | **本次审计新增。** 不经 `senderForEntry`，是**独立的一处**。读 config.yml 的 `vision_helper`，把**图片内容**发给该 endpoint —— 与 `/ai/chat/completions` 之外的第二个内容出口 |
| **11** | **lite sender**（压缩与标题生成） | `internal/server/server.go:1455`（`buildAgent`）与 `:2725`（`buildChannelAgent`）→ `liteSenderFromConfig`（`:1917`）→ `cachedSenderForEntry` | 不构造；`a.LiteSender` 为 `nil`，压缩回落会话自身的 sender（生产下即工厂 sender） | **本次审计新增。** `internal/agent/compaction.go:552` 真发模型请求，把**会话原文**发给该 endpoint。读 config.yml 的 `lite`/`lite_model` |
| **12** | **channel 模型绑定** | `internal/server/server.go:2712`（`buildChannelAgent` 的 `Resolve(profile.Model)`）、`applyChannelModel`（`:2887`，每回合在 `:3720` 调用）：经 `channelModelOps`（`:2831`）→ `cachedSenderForEntry`（`:2869`/`:2899`） | 不构造；回落服务器默认 sender（生产下即工厂 sender） | **本次审计新增，且出厂即启用**（`profiles/production.json` 为 `"channels": true`）。IM 的 `/model` 或 web 模型选择器写入的绑定可在每回合覆盖工厂。其注释自述"解析失败就**回落默认**，和 `senderForSession` 给 REST 的降级一样"——正是端口 4 要消灭的那件事 |

**结论（2026-09-11 修订）**：这张清单原本只覆盖「**决定 sender 的输入**」，因此漏掉了**第二类**——「**另外几处自己造 provider client 的地方**」：#10/#11/#12。两类都要堵，但它们不是一回事，且第二类的收口方式要选（见下节 B1.0a）：

- **第一类（#1–#9）**：决定"默认/会话 sender 是谁"。**9 个入口、只有 3 处代码**（#1/#3 同一处、#5 一处），且**只有 #5 是 desktop 侧真的关不掉的** —— 这就是 [01A §3.1](01A-既有产品逻辑迁出internal-server.md) 那份端口申请的依据。已由端口 4 收口。
- **第二类（#10–#12）**：根本不问 sender 是谁，各自从 `data/config.yml` 造一个 client。端口 4 管不到它们，因为它们不经过 `resolveProviderAndModel`，也不经过 `senderForSession` 的工厂分支。

其余两条不变：② 除端口 4 之外，B1 **只改 desktop**，不要再顺手动 `internal/server`；③ #2、#8、#9 不要顺手做 —— #2 已核实不发出去，#8/#9 属 P0-08/P0-02，在 B1 里做会越界（§3.4 的「别的方案也会这么干」问一次：#2 的答案就是"不发这个二进制，所以不改"）。

### B1.0a 第二类绕过的收口方案（2026-09-11 新增，**待决策**）

**为什么第二类不能靠"出厂不配那些键"解决。** 表面上，只要 `config.yml` 里没有 `vision_helper`/`lite`，三条路径就都不生效。但 `开发规范` §3.10 已经把口径定死：**"the production **session** must not use `config.yml` as its model source"** —— 约束的是**生产会话**，不是"我们没发这个键"。而 portable 产品的 `data/config.yml` 是**用户可写**的（这正是端口 4 存在的理由：输入不经过 `server.Config`），所以"没发这个键"不是边界，只是默许。三条路径都会把**内容**（图片、会话原文、整轮对话）发给一个未受信的 endpoint，这是与 `03` 同级的出口问题，不能靠默认值挡。

**好消息：收口点只有两个，不是三处。** 实测调用链——#11 与 #12 **共用**一个咽喉，`cachedSenderForEntry`（`server.go:1865`）是它们唯一的低层构造入口；#10 是独立的一处：

| 要堵的 | 位置 | 覆盖 |
| --- | --- | --- |
| ① `cachedSenderForEntry` | `server.go:1865` | #11 lite（`:1922`）、#12 channel（`:2869`/`:2899`），以及 #5 会话绑定（`:1847`，端口 4 已覆盖，此处是冗余保险） |
| ② 视觉描述器构造 | `server.go:1454` / `:2724` | #10 |

**把①堵掉之后，现有回落语义恰好变成正确行为**，这是这套方案最值得记的一点：三条路径在构造失败时的既有回落**都指向"默认 sender"**，而生产下默认 sender **就是工厂 sender**（`senderForSession` 的工厂分支）。也就是说它们从"静默降级到未授权来源"变成"降级到唯一授权来源" —— `Bounded degradation`（§3.9）要求的"回落目标要可命名"，在这里的答案就是"网关"。

**唯一残留**：`a.LiteModel` 这个**模型名**仍可能来自 config（`buildAgent:1461` 用 `EntryByModel(sess.ModelConfig)` 的 provider/baseURL 问厂商注册表），于是会拿一个网关不认识的模型名去请求 → 回合失败（不是外发，是坏请求）。生产下 lite 模型应当取自目录，属 P0-05。

**三个选项**：

| 选项 | 做法 | 代价 | 判断 |
| --- | --- | --- | --- |
| **A 扩宽端口 4**（让工厂也决定这三处） | 端口加一个"按 ref 取 sender"的方法，把 ①② 两处改为先问宿主 | 更中性的语义更宽；`internal/server` 改动最多 | 不推荐：语义上"lite 模型""视觉助手"在生产下**没有网关对应物**，扩宽端口会把一个产品决策伪装成接口问题 |
| **B 出厂不配 + 不暴露入口** | profile/打包层保证不含这三类键；生产 UI 隐藏 endpoint 管理 | 几乎零代码 | **单独不成立**：用户手改 `config.yml` 即可绕过（§3.10 的措辞正是为了避免这种"听起来更严"的假边界）。可作为 A/C 之外的**附加**措施 |
| **C 结构性收口（推荐）** | 工厂置位时，①② 两处**拒绝**构造 config sender；失败即走既有回落（生产下落到工厂 sender） | `internal/server` 上加两处判断；**server.go 当前 543/543，零余量** | **推荐**。它把"3 处散堵"变成"2 个点"，且复用既有回落语义，不需要新增降级路径 |

**推荐的落地方式（两条都要）**：

1. **现在就登记，不现在改代码。** `internal/server/server.go` 是 `543/543`、零余量；刚用三条理由申请过 484→543（[01A §3.1](01A-既有产品逻辑迁出internal-server.md)），紧接着再抬会削弱这个棘轮。收口动作**并入 01A 阶段 C**（`apiProduct` 折叠会砍掉数百行，届时这两处判断不占新额度），与 §3.1 已登记的 `Resolve` 的 ctx 债同一个窗口。
2. **在收口完成前，它必须是发布门而非常识。** 在 01A 阶段 C 完成前，任何声称"production 只走网关"的结论都**不成立**；P0-05 开工清单要把 #10/#11/#12 列为前置检查，`package-portable.mjs` 的产物自检增加一条反向断言（生产包内出现 `vision_helper`/`lite` 配置即告警）——注意这条只是**附加**措施，不能替代 C。

**需要产品拍板的一个问题（§3.7）**：收口后 `vision_helper` 与 lite 在 production 下**就没有对应能力了**（描述器 `nil`、压缩回落主 sender）。是要"生产下关闭视觉描述"（安全、少功能），还是要"用目录里 `capabilities.vision` 的模型通过网关做视觉"（多一个目录字段的消费方，属 P0-05）？两条路的实现量与验收都不同，必须先答。无论选哪条，**都不得**保留 `config.yml` 作为来源。

**每一条堵完都要有一条"反向测试"**：developer 下该入口**必须仍然有效**（否则就是拿开发能力换安全，`开发规范` §3.9 的降级路径要有去处）。只写"production 下被忽略"的测试是不够的——那样把 developer 功能一起关掉也能通过。
