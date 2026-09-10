# P0-01A：既有布丁逻辑迁出 `internal/server`

| 项 | 内容 |
| --- | --- |
| 主责建议 | 客户端核心负责人；与 P0-01 同一所有者 |
| 优先级 | P0；P0-01 的结构工作完成后开始，P0-05 集成前完成与其相关的迁移/删除 |
| 依赖 | [P0-00](00-Server最小改动与生产Profile前置.md)、[P0-00A](00A-生产入口可见性与功能保留.md)、[P0-01](01-运行时Profile与窄端口抽象.md) |
| 目标 | 把 2026-09-08/09 新增的产品业务从上游通用运行时迁出；保留通用 server 行为和已存在的配置、MCP、channel、工具、后台能力 |
| 不做 | 不重写 HTTP/WS/agent/session 生命周期；不借机删除 `config.yml`、`server.New` 的配置读取、MCP、channel、工具或后台能力；不把本地临时账号/积分当作正式产品能力延长 |

## 1. 边界与完成定义

`internal/server` 应只保留上游通用 HTTP/WS、认证包装、会话/agent 生命周期、工具调度和 sender 基础设施。2026-09-08/09 引入的账号、产品状态、积分、词库、模式和隐私策略属于布丁产品层，不能因为当前恰好能访问 `Server` 私有字段而永久留在这里。

本任务的完成不是“把文件改名”。完成时必须同时满足：

1. 产品 HTTP、产品状态、登录/退出、产品门、词库、产品偏好和发送前策略的实现与测试位于 `internal/productruntime` 或其产品专属下游包。
2. `internal/server` 不再持有 `productState`、`productGate`、登录码会话、敏感词引擎、积分扣减或布丁专属路由；通用 server 行为保持原有回归。
3. `server.New` 继续加载 `config.yml`；`workspace_dir` 的显式配置继续生效；MCP、channel、工具和后台能力不因本次迁移被关闭或删除。`ensureLocalEndpoint()` 已删除（假模型不再种入 `config.yml`），不再作为暂留项。
4. P0-05 删除本地积分路径，不将其移动到新的产品包；P0-02 的真实认证接入完成前，遗留本地登录仅作为兼容路径，不能被称为正式认证或作为产品授权边界。
5. 每次迁移后都有原行为回归、server 通用回归和 production/developer 边界回归；不得以页面隐藏替代运行时断言。
6. **`apiProduct` 折叠必须完成**：`internal/server` 中不再存在该符号，`registerRoutes()` 与上游 `main` 逐行一致，由 `server-diff-guard` 持续断言（§7）。这是本任务中收益最大、也最容易被跳过的一项 —— 不做它，`01A` 就只完成了一半。

## 2. 精确盘点与去向

> **这张表也是「上游合并影响」的登记表。** 任何**新增**工作包只要触碰 `internal/server` 或其他上游文件，就必须在下表增加一行（文件 → 改动性质 → 是否回收、归哪个阶段），否则视同缺 `技术方案/` 的「上游合并影响」一节 —— 见 [README §5.1](README.md#51-本批次为什么不按-技术方案-七节结构e7-决策2026-09-11)。`server-diff-guard` 断言的新产品文件登记工作包，指的就是这张表。

| 当前位置 | 当前职责 | 处理 | 目标归属与验收 |
| --- | --- | --- | --- |
| `product_handlers.go`、`product_nickname.go`、`product_prefs.go` | 产品状态、退出、语言、昵称、偏好 HTTP | **迁出** | `productruntime/httpapi`；产品路由测试随实现移动，状态读写及语言/昵称校验结果不变。 |
| `product_login.go` | 本地验证码、激活、账号状态 | **迁出后替换** | 先迁入 `productruntime/httpapi` 的兼容适配层；P0-02 改为 `productclient.AuthClient`，P0-08 接管凭证保存。不得把固定验证码、激活码、进程内会话或本地 token 作为新接口契约。 |
| `product_sensitive.go`、`sensitive_dict_handlers.go` | 输入检查、词库编辑、输出过滤所需状态 | **迁出** | HTTP 与词库编排进入 `productruntime`；词库文件和 `internal/sensitive` 算法保持复用。P0-06 决定正式词库/隐私合同，不能在 server 内扩展产品规则。 |
| `chatmode_handlers.go`、`privacy.go` | 会话模式、默认模式、发送副本的隐私上下文 | **分阶段迁出** | **`chatmode_handlers.go`（含 `handleGetChatModes` 的模式/模型来源）的唯一 owner 是本任务阶段 D**：P0-04 只交付 `chatmode.Project` 纯函数，P0-05 只做 session/UI 接线，**两者都不改此文件**（早先 `05` 实施步骤 3 曾把它写成自己的步骤，已删）。投影接入必须与迁出**同 PR**，禁止"先在 server 里加、以后再搬"的两处并存中间态（§6）；`compositeIDForModel` 不动。会话装载、绑定锁和 WS 生命周期留在 server。 |
| `handlers.go`、`ws_handlers.go`、`tasks_handlers.go` 的 `consumeCredit()` 与产品门/隐私调用点 | 本地积分、产品门、发送前产品策略 | **删除或改接中性端口** | 本地积分在 P0-05 删除；产品门和发送策略改由 productruntime 提供的通用请求/turn 适配执行，不在通用 handler 内写产品分支。 |
| `server.go` 的产品字段、产品状态初始化、产品 sender 包装、产品路由注册 | 产品状态被耦合进 server 构造和路由 | **迁出** | desktop 构造 `productruntime`；server 只接收中性扩展接口。`server.New` 的配置读取、通用 sender/session 初始化及 generic route 注册保留。 |
| `server.go` 的 `apiProduct()` 与全部 157 处 `s.apiProduct(` 调用点 | P3 为产品门把**整张上游路由表逐行改写**：上游 `main` 里 `apiProduct` 出现 **0** 次，`main` 的 147 处 `s.api(` 被替换 | **还原 + 折叠为顶层中间件（本任务最大的一笔回收）** | 见下文「apiProduct 折叠」一节。通过条件：`registerRoutes()` 与上游逐行一致，`internal/server` 不再存在 `apiProduct` 符号，而产品门语义（默认拦截、显式豁免）不变。`server-diff-guard` 以棘轮断言（基线 161，目标 0）。 |
| `internal/productgate`（**既有包**，`gate.go` / `gate_test.go`） | 产品门的**当前**实现：请求期中间件 `Gate.Middleware(next http.Handler)`；`server.go` 直接 import 并持有 `productGate *productgate.Gate`（`server.go:43` / `423` / `620` / `919`） | **折叠后替换**（阶段 C，与 `apiProduct` 折叠同一个 PR） | 替换为注册期端口 `runtimeport.ProductGate`（§3 端口 2） + `productruntime` 的实现，**不新写一套**。注意现状的 `Middleware(next)` 拿不到 route pattern，正是 §3 端口 2 明确否决的形态 —— 所以这是**待替换的现状**，不是"已经做好了"；拒绝响应与 `WriteDenied` 的语义原样保留。 |
| `ensureLocalEndpoint()` 与 `local_endpoint_test.go` | 为既有本地模型配置种入 4 个 `buding-*` 假模型 endpoint | **已删除**（2026-09-11） | 假模型不再是产品能力：正式模型列表来自中台签名目录并本地缓存（见 P0-04 模型目录）。删除后 `server.New` 不再写用户 `config.yml`，也消除了「seed 被 access-key 保存覆盖」那类读改写竞态。 |
| `server.go` 的 3 个 `resolveProviderAndModel` 调用点 + 新增 `internal/runtimeport/senderfactory.go` | 四类输入（`New`/`ensureSender`/`reloadDefaultSender` 的 `OCTO_PROVIDER`·`entry.Provider`，以及 `senderForSession` 的会话绑定 `ModelConfig`）能让 session 用上非中台 sender | **不回收（端口化）**，见 §3.1 | P0-01 B1 申请的端口 4：`Config` 加 1 个 `SenderFactory` 字段，3 个调用点统一走 `chooseDefaultSender`，`senderForSession` 加 1 个分支；`nil` 时逐行等于今天的路径；置位时失败即失败、不回落。`server-diff-guard` 上限 484 → 543（§3.1 有让步理由）。除端口 4 之外 B1 只改 desktop。 |

### `apiProduct` 折叠（本任务最大的一笔回收）

**事实**（`git` 实测，2026-09-11）：

```text
上游 main 中 "apiProduct" 出现次数：0        ← 纯 fork 符号
main    里 s.api(      调用点：147
当前分支 里 s.api(      调用点：4
当前分支 里 s.apiProduct( 调用点：157
当前分支 里 "apiProduct" 总出现次数：161（157 调用点 + 1 函数定义 + 3 注释）
server.go diff：317 added / 167 removed = 484 行
  ├─ 新增行中含 "apiProduct(" 的：158 行（≈50%）
  └─ 删除行中原为 "s.api("    的：147 行
引入提交：c3bd45fe(2026-09-08, P3)，单次转换 150 处
```

`server-diff-guard` 以这组数字为棘轮基线（`internal/server` 总 diff 与逐文件上限见 §7.1）。

两个 wrapper 除产品门那 6 行外**完全相同**。结果是：上游每新增一条路由，冲突都落在这 147 行的逐行改写区域 —— 这是全仓最大、且**可以完全消除**的合并冲突面。

**目标形态**：产品门从"逐路由改写"变为"一层顶层中间件 + 显式豁免集"。

```text
现状（默认拦截靠改写路由表）：
  s.api(health)          ← 隐式豁免：谁用 api 谁豁免
  s.apiProduct(chat)     ← 逐条改写，157 处

目标（默认拦截靠中间件）：
  registerRoutes()  ← 与上游逐行一致，全部 s.api(...)
  requireAuth( productGateMiddleware( mux ) )
        ↑ 产品门在位；豁免集显式枚举
```

**实现约束（必须照做，否则会引入静默安全洞）**：

1. **语义会反转**。现状是"谁用 `api()` 谁豁免"（**隐式**）；改成中间件后是"默认全拦、显式豁免"（**显式**）。因此豁免集必须**显式枚举**，不得依赖"没用 `apiProduct` 就自然豁免"。
2. **包裹层级**：产品门只能包在 `requireAuth` **那一层之内**，不能包整个 mux —— 否则会拦到心跳、静态文件、OAuth callback 等未登记路由。
3. **豁免判断用路由 pattern，不用路径字符串**：避免路径前缀匹配把 `/api/product/state/../chat` 之类误判为豁免。
4. **不保留 `apiProduct` 符号**：折叠后该名字应从 `internal/server` 完全消失（`server-diff-guard` 会断言，见 §7）。
5. **豁免集与现状等价，且是可穷举的**（2026-09-11 实测；`rg -c 's\.api\(' internal/server/server.go` 恰好为 4，加上 157 处 `apiProduct` 即棘轮基线 161）：

| 路由 / 位置 | 当前注册方式 | 折叠后 | 说明 |
| --- | --- | --- | --- |
| `GET /api/health` | 直接 `mux.HandleFunc`（`server.go:982`） | 不变 | 不在 `requireAuth` 内，产品门不参与 |
| `GET /api/version` | 直接 `mux.HandleFunc`（`server.go:983`） | 不变 | 同上 |
| `GET /api/mcp/servers/{name}/oauth/callback` | 直接 `mux.HandleFunc`（`server.go:1110`） | 不变 | OAuth 回调**有意**绕过鉴权，不得挪进产品门 |
| `/` 静态文件 | `mux.Handle("/", …)`（`server.go:1129`） | 不变 | 不在 `requireAuth` 内 |
| `GET /api/product/state` | `s.api(`（`server.go:989`） | **豁免**（`Wrap` 原样放行） | 登录页/拦截页要读状态 |
| `PUT /api/product/locale` | `s.api(`（`server.go:990`） | **豁免** | 拦截页语言切换 |
| `POST /api/product/send-code` | `s.api(`（`server.go:991`） | **豁免** | 登录取码 |
| `POST /api/product/login` | `s.api(`（`server.go:992`） | **豁免** | 登录提交 |
| 协议 / legal 路由（**当前尚不存在**） | — | 新增时必须登记为豁免 | 未创建前不得预留裸路径 |

**豁免集的唯一来源**：折叠后豁免集由 `productruntime` 以**导出常量**持有（`internal/server` 只在 `api()` 里把 pattern 交给 `productGate.Wrap`，不自己维护豁免名单）；测试直接引用该常量、不复制字符串 —— 否则「测试里的豁免集」会成为第二份真相（§3.8），而两份里总会先改一份。

**回归**：现有 `apiRoutes` 覆盖测试（断言每条路由都拒绝无 key 的非 loopback 请求）必须继续通过，并**新增**一条断言：未登录时被保护的样本路由返回产品门拒绝、上表 4 条豁免路由不被拦。

## 3. 唯一允许的通用协作端口

产品 HTTP 和产品门目前直接依附于 `Server` 的私有 mux、`apiProduct`、会话绑定和 sender。为了迁出而复制这些通用机制同样不可接受。P0-01A 只允许在证明 desktop 不能完成装配后增加一个中性端口；它必须使用独立的中性包（建议 `internal/runtimeport`），不能把 `product`、账号、积分、词库、模型供应商等名词写进 `internal/server` API。

最小端口按以下顺序引入，未被迁移步骤需要时不创建：

1. **认证后路由注册器**：server 提供已完成机器访问校验、统一 no-store 的注册入口（`internal/runtimeport` 的 `RouteRegistrar`）；productruntime 用它注册自己的 HTTP handler。产品门作为 productruntime 的 handler/middleware，而非 `server.apiProduct`。**仅阶段 B 迁出产品 `s.api` 路由时才创建**。
2. **产品门端口（`ProductGate`）**：`apiProduct` 折叠**必须**用这个端口，也是本任务唯一必须新增的端口 —— 不做它就得保留 157 处 `apiProduct` 改写。**此前写的「中性 `func(http.Handler) http.Handler`」与同节要求的「豁免按路由 pattern 判断」不能同时成立**（拿不到 pattern 的请求期中间件做不到后者），形态以这里为准：

```go
// internal/runtimeport —— 中性包，无 product / 账号 / 积分 / 词典 / 供应商名词。
type ProductGate interface {
	// Wrap 在路由**注册期**调用（不是请求期），因此能拿到 Go 1.22 的 pattern
	// 并按 pattern 判断豁免。实现由 productruntime 提供：豁免集、登录判定、
	// 拒绝响应体都由它持有；server 不保存产品状态、不逐路由改写路由表。
	//
	//   - 豁免 pattern：原样返回 next；
	//   - 其它 pattern：检查桌面窗口登录态，放行或自行写出拒绝响应。
	Wrap(pattern string, next http.HandlerFunc) http.HandlerFunc
}
```

`internal/server` 的接入：`api()` 在 `requireAuth` **之内**调用 `productGate.Wrap(pattern, h)`；`productGate == nil`（`octo serve` 与现有测试）时**零变化**。豁免集见 §2.1 的精确表，其唯一来源是 productruntime 的导出常量。这是 P0-01A 唯一必须引入的端口。
3. **turn 上下文钩子或 sender 装配点**：仅当 P0-05 迁出隐私/发送前策略时使用。输入只能是 `context`、`agent.Session`、通用 sender/请求元数据；不得暴露 server 私有锁、mux 或产品状态。
4. **宿主 sender 工厂（`SenderFactory`）**：见 §3.1 —— 由 P0-01 B1 申请。它和端口 3 都要一个"sender 装配点"，但回答的是不同问题：端口 3 是"每一轮在既有 sender 上做什么"，端口 4 是"这一轮的 sender 由谁决定"。**不要合并成一个端口** —— 合并后 `internal/server` 就会知道"产品会换 sender"这件事，那正是 B1 要消除的知识。

`internal/productruntime` 可依赖上述中性 `runtimeport` 接口，但不得依赖 `internal/server` 的实现或私有类型；`internal/server` 也不得导入 productruntime。由 `cmd/octo-desktop` 负责把两者装配起来。这是为了移除已有耦合的最小例外，不是为产品业务扩大 server API。

### 3.1 端口 4 申请：宿主 sender 工厂（P0-01 B1，2026-09-11）

**要解决的问题**：B1 的验收句是「`OCTO_PROVIDER` / `config.yml` endpoint / 本地 provider 都不能让 production session 绕过 gateway」。经侦察，当前 `buding` 上决定 sender 的输入有**三处**，且**只有第三处无法从 desktop 侧关闭**：

| 输入 | 位置 | desktop 能否单独关闭 |
| --- | --- | --- |
| `OCTO_PROVIDER` / `entry.Provider` | `internal/server/server.go:1649`（`resolveProviderAndModel`） | 勉强能（`Config.Provider` 是 `firstNonEmpty` 的第一项），但 provider 名一旦非空，凭据就转由 `resolveAPIKey` 从**厂商环境变量或 `data/config.yml`** 取 —— 等于换了个入口，没有真正封堵 |
| 同一函数被懒重试再次调用 | `internal/server/server.go:2193`（`ensureSender`） | 同上 |
| **会话绑定的 `ModelConfig`** | `internal/server/server.go:1768`（`senderForSession`）：读 `sess.ModelConfig` → `config.LoadCached()` → `cachedSenderForEntry` 建 sender，**且构建失败时静默回落到默认 sender** | **不能。** 输入是**已落盘的会话 JSON** 加上 `data/config.yml`，都不经过 `server.Config`。desktop 唯一的替代是改会话状态 —— 让客户端去改写上游的会话结构，比加一个端口糟得多 |

**结论**：第三处构成 01A §3 要求的"证明 desktop 不能完成装配"，端口 4 因此成立；而且它同时让前两处不必各自打补丁。

**端口形态**（中性包 `internal/runtimeport`，无 `product`/账号/积分/供应商名词）：

```go
// SenderFactory supplies the agent.Sender one turn runs on. A host that sets it
// has taken responsibility for vendor, endpoint and credentials.
type SenderFactory interface {
	// SenderForTurn returns the sender and the model id for one turn. A
	// non-nil error fails the turn: the server does not fall back to its own
	// resolution, because that fallback is the bypass this port exists to
	// close.
	SenderForTurn(ctx context.Context, req SenderRequest) (agent.Sender, string, error)
}

type SenderRequest struct {
	Session *agent.Session // read-only; the host may read Model / ModelConfig
	Model   string         // the model the session asked for, if any
}
```

**三条语义必须写死在端口里**，否则它会退化成第四个绕过入口：

1. **置位即独占**：`SenderFactory != nil` 时，`resolveProviderAndModel` 不再读 `OCTO_PROVIDER`、厂商 API key 环境变量、`data/config.yml` 默认条目；`senderForSession` 不再读 `sess.ModelConfig`。
2. **失败即失败，不回落**：工厂返回 error 时该轮直接失败。这与上游"构建失败则静默回落到默认 sender"的既有降级相反 —— 那个降级在 production 下就是绕过 gateway。**回落语义必须在端口上写死，不能留给调用方选择**，否则迟早有人为了"更健壮"把回落加回来。
3. **`nil` 时零变化**：`octo serve` 与现有所有构造点（含测试）都不设置它，走今天完全相同的代码路径。这条是"不扩大上游行为"的保证，也是端口能通过评审的前提。

**改动清单**（每处都要 `// OCTO-FORK:` 标记，并按 §2 登记）：

| 文件 | 改动 | 量 |
| --- | --- | --- |
| `internal/runtimeport/senderfactory.go` | 新增：`SenderFactory`、`SenderRequest`、`Resolve`（优先级与"不回落"策略住在这里）、`FailingSender`、`ErrNoSender` | 新文件 |
| `internal/server/server.go` | `Config` 加 1 个 `SenderFactory` 字段；**3 个 `resolveProviderAndModel` 调用点**（`New`、`ensureSender`、`reloadDefaultSender`）统一改走新的 `chooseDefaultSender`；`senderForSession` 加 1 个分支接管会话绑定 `ModelConfig`；`registerRoutes()` 不动 | +56 / −3 行 |

**已经落地的三个实现决定**（都是实现时才发现、值得记下来的）：

1. **策略不住在 server 里。** 第一版把"工厂置位即独占、出错不回落"的判断写在 `server.go` 的 `chooseDefaultSender` 里，diff 是 +82 行。`server-diff-guard` 立刻拒了。改成：判断与降级策略放进 `runtimeport.Resolve`，`server.go` 只留"问宿主；宿主没意见就照旧"—— diff 降到 +56/−3。这不是为了讨好行数守卫：**优先级规则是 fork 知识，不是上游知识**，`internal/server` 只该知道"宿主决定这件事"，不该知道为什么。
2. **`reloadDefaultSender` 是第 4 个入口。** 它同样调 `resolveProviderAndModel`，在"全局设置或 model-config 条目变化"时重建默认 sender。只堵 `New`+`ensureSender` 会漏掉它。正因为有 3 个调用点，才必须**统一走一个 `chooseDefaultSender`** —— 逐个打补丁的话，将来第五个调用点没人会记得加判断。
3. **工厂提供的 sender 仍要过 `wrapProductSender`。** 否则 P8（敏感词回显过滤）与 P10（隐私模式副本打码）会在这一条路径上静默失效 —— 而那正是"能复用不复用"要避免的降级。用 `FailingSender` 表达失败时也要包，保持一致。

**未接通项：`Resolve` 的 `ctx` 目前是死参数（2026-09-11 登记，本阶段不修）。**

端口形态上 `SenderForTurn(ctx, …)` 带 ctx，但 `runtimeport.Resolve` 的实现把它替换成了 `context.Background()`，因为 **server 侧四个调用点手上根本没有 ctx 可传**：`server.go:479`（`New`）、`1429`（`buildAgent(sess)` → `senderForSession`）、`2246`（`ensureSender()`）、`2305`（`reloadDefaultSender()`）都不是带 ctx 的函数。要让 turn 的取消/超时真的到达工厂，必须把 ctx 穿过 `buildAgent`/`senderForSession` 这一串 server 内部函数 —— 那是**改上游代码**，而 `internal/server/server.go` 现在是 `543/543`、**零余量**，任何改动都会立刻触发 `server-diff-guard`。

| 项 | 值 |
| --- | --- |
| 现状 | `Resolve` 传 `context.Background()`；接口上的 ctx 不生效 |
| **临时约束（必须遵守）** | **工厂实现不得依赖 ctx 的取消/超时语义**，也不得在其中做需要被取消的 I/O。P0-05 若要在工厂里取/刷新产品 token，必须先回到本节把 ctx 接通，不能假设它已经能取消 |
| owner | 01A **阶段 D**（sender/state 搬迁）+ 阶段 C（`apiProduct` 折叠让 `server.go` 降回 484 以下，腾出额度） |
| 恢复动作 | 给 `runtimeport.Resolve` 与四个调用点加 ctx（二选一：加参数，或从接口删掉 ctx 并写明"工厂不得做可取消 I/O"）。**保留一个恒被丢弃的参数是最坏选项** —— 它让实现者以为拿到了取消语义 |
| 验收 | 取消一个进行中的 turn，断言工厂收到的 ctx 已 done |

**做过的守卫让步（§3.7 人工确认项）**：`server-diff-guard` 的 `internal/server/server.go` 上限由 **484 提到 543**（+59）。理由三条，缺一条就该改成折叠而不是提上限：① 端口是已批准的架构变更；② 没有更小的形态（`Config` 是唯一构造通道；守卫建议的"包级 setter + server 读取"是隐藏全局状态，是真实退化而非更小的 diff）；③ 提之前**先**把策略行搬进了 fork 包（−23 行）。C 阶段折叠 `apiProduct` 时这个数必须落到 484 **以下**，而不只是 543 以下。

**不做的事**：不改 `resolveProviderAndModel` 的内部逻辑（`nil` 路径必须逐行等于今天）、不动 `registerRoutes()`、不动 `apiProduct`（那是 C 阶段）、不把 `runtimeport` 变成通用插件系统。

**端口 4 的作用域窄于绕过面（2026-09-11 审计新增，必读）**：端口 4 收口的是「**决定默认/会话 sender 是谁**」这一类。还有**第二类**绕过 —— 视觉描述器、lite sender（压缩/标题）、channel 模型绑定 —— 它们**不问 sender 是谁，各自从 `data/config.yml` 造一个 client**，因此端口 4 管不到它们，其中 channel 那条**出厂即启用**（`production.json` 的 `"channels": true`）。收口点实测只有两个（`cachedSenderForEntry` 覆盖 lite+channel 两个，视觉描述器独立一处），方案、代价与否决理由见 [P0-01 §B1.0a](01-运行时Profile与窄端口抽象.md#b10a-第二类绕过的收口方案2026-09-11-新增待决策)。**在该收口完成前，任何"production 只走网关"的结论都不成立。**

**验收（正向 + 反向都要）**：

- 正向：注入一个工厂后，同时设置 `OCTO_PROVIDER=anthropic`、`OCTO_ANTHROPIC_API_KEY=…`、`data/config.yml` 里一个指向第三方的默认条目、以及会话里 `ModelConfig` 指向同一第三方 —— 断言三处**全部无效**，实际使用的是工厂给的 sender。
- 失败路径：工厂返回 error → 该轮失败，**且断言没有回落到 env/config 的 sender**（这条是端口 2 号语义唯一的守卫）。
- 反向：工厂为 `nil` 时，现有 provider/model 解析测试**一字不改**地通过（它们已存在，正是 01A §4 A 阶段要的特征测试）。
- production：`cmd/octo-desktop` 在 `product_production` 下恒设置它；developer 下不设置，保留 `config.yml` 配第三方 endpoint/URL/key 的能力。

## 4. 执行顺序

| 阶段 | 变更 | 不变量 | 通过条件 |
| --- | --- | --- | --- |
| A：冻结基线 | 为上表每个现有路由、产品门结果、状态迁移、词库写入、聊天/隐私行为补齐特征测试；记录 route、状态文件和错误码 | 不改变 UI 可见性，不改变 `config.yml`/MCP/channel/tool/background 行为 | 迁移前后同一组测试可运行；没有依赖未记录的 server 私有字段。 |
| B：迁出独立产品 HTTP | 建立 productruntime 的状态、HTTP handler 和测试；需要时仅增加“认证后路由注册器” | server 仍负责机器访问校验、通用 no-store、HTTP 服务启动 | `product_handlers`、昵称、偏好、词库 handler 不再是 `Server` 方法；API 行为回归通过。 |
| C：迁出账号与产品门 + 折叠 `apiProduct` | 把本地兼容登录/状态门移到 productruntime；以中性请求过滤器（顶层中间件）覆盖需要保护的路由；**同时折叠 `apiProduct`**：`registerRoutes()` 全部还原为 `s.api(`，删除该函数及 157 处调用，产品门改由中间件承担 | 未完成 P0-02 前不把本地 token 当正式凭证；loopback/机器访问既有边界不扩大 | `Server` 不再保存登录码、产品状态或产品门；`make server-diff-check` 的 `apiProduct` 计数降到 0（棘轮，只允许降）；`registerRoutes()` 与上游 `main` 逐行一致；登录、退出、未登录阻断和豁免路由回归通过；新增「被保护路由被拦 / 豁免路由不被拦」断言 |
| D：迁出发送策略和模式 | P0-04/P0-05 分别接目录/PEP、gateway sender；用中性 hook 迁出隐私和默认模式 | session 绑定、WS、agent 生命周期不移动；隐藏入口不改变 MCP/工具能力 | server 不再含产品 sender wrapper、chat-mode/PII 产品判断；生产会话只经 gateway。 |
| E：删除旧积分与收尾 | 按 P0-05 删除 `consumeCredit`、`ConsumeCredit`、`initialCredits`、`Credits` 及响应/测试；清除空字段和旧路由 | usage 只读投影来自 gateway/控制面，失败/取消可追溯 | 静态搜索无旧积分路径；请求终态、余额显示和撤销测试通过。 |

## 5. 允许暂留在 `internal/server` 的代码

以下内容有明确的上游通用归属，不随产品逻辑迁出：

- HTTP server、mux、机器访问校验、统一响应缓存头和 generic route 注册；
- WebSocket、会话加载/保存、会话绑定锁、agent 生命周期、通用 `agent.Sender` 插槽；
- `config.yml` 读取/保存、`workspace_dir` 解析；
- MCP、channel、工具、后台任务及其启动和关闭流程；
- 仅为上述迁移实际需要而新增的中性 `runtimeport` 适配（认证后路由注册器、产品门中间件）。每一处必须有 `OCTO-FORK` 注释、关联本文件的阶段和 server 回归测试。

**不再计入暂留项**：`ensureLocalEndpoint()` 与 4 个 `buding-*` 假模型 endpoint 已删除（2026-09-11，见 §2）。`internal/server` 不再在启动期写入用户 `config.yml`。

### 5.1 恢复上限（B 类：永久不可恢复）

本任务常被理解成"迁完产品业务，`internal/server` 就回到上游原样"。**这个理解是错的**，需要在开工前写清楚，否则 §7.2 的验收会被误设成"`git diff` 对上游为空"，而那是一个永远达不成、且达成即违规的目标。

恢复目标分两类：

| 类别 | 内容 | 是否恢复 | 原因 |
| --- | --- | --- | --- |
| **A 类：产品业务** | 产品 HTTP handler、产品门、登录态、词库、隐私与发送策略、积分 mock、`apiProduct` 折叠 | **恢复**（§4 阶段 B–E 的目标） | 它们本就属于 productruntime，不属于上游 server |
| **B 类：datapath 强制的承载** | 见下方清单 | **永久保留，不恢复** | 恢复它们 = 让 `datapath-guard` 变红 = 违反[开发规范](../../../开发规范.md) §3.1 硬规则 |

**B 类清单**（`buding @ ea37bc53` 实测）：

- **测试侧 58 个文件**：`internal/server/*_test.go` 中 `t.Setenv("OCTO_DATA_ROOT", …)` 的调用。上游测试直接读写 `~/.octo`，本 fork 必须把测试数据根指到临时目录 —— 没有这一行，测试会污染真实数据根。这是 fork 硬规则的直接后果，逐行改写无法避免。
- **非测试侧 11 个文件、18 处 `datapath.` 调用**：`server.go`(3)、`sensitive_dict_handlers.go`(3)、`profile_handler.go`(2)、`onboard_config_handlers.go`(2)、`attachments.go`(2)、`uploads_housekeeping.go`(1)、`tasks_handlers.go`(1)、`session_groups.go`(1)、`lightapps_handlers.go`(1)、`handlers.go`(1)、`chatmode_handlers.go`(1)。这些是**上游就存在**的路径解析（附件、上传清理、会话分组、任务、profile 等），本 fork 只把 `os.UserHomeDir()` + `".octo"` 换成了 `datapath.*`，逻辑与上游一致。

**与 §6「不保留 `apiProduct`」的区别**：`apiProduct` 是**纯 fork 改写**（收益为零、成本为每次合并），所以目标是 0。B 类是**硬规则强制的承载**（不写就违反 CI），所以目标是"行数冻结、只降不升"。两者的验收口径不同，不要混用。

**与 `server-diff-guard` 的关系**：B 类的 diff 行数正是 `DEBT_CEILINGS`（§7.1 R2）里 6 个登记文件的构成部分。因此"恢复上限"不是一句免责声明 —— 它有 CI 断言：B 类 diff **只允许降不允许升**，上调 ceilings 属于[开发规范](../../../开发规范.md) §3.7 的人工确认事项。

## 6. 禁止的捷径

- 不因首发 UI 不显示某个入口而关闭其 route、MCP、channel、工具或后台执行能力。
- 不把产品状态、登录、积分、词库、隐私、模型目录或中台 HTTP 放入新的 `server.Config` 字段。
- 不复制 `server` 的 mux、认证、WS、会话锁或 agent 编排到 productruntime。
- 不用本地配置、固定积分、临时 token 或手工 API 绕过 P0-02 至 P0-05 的正式链路。
- 不在迁移过程中顺带改变 `config.yml` 或 `workspace_dir` 的兼容语义。
- **不保留 `apiProduct` 作为“以后再说”的遗留**：它是 157 处纯 fork 改写，收益为零、成本为每次上游合并；折叠它属于本任务范围，不属于“可选优化”。`server-diff-guard` 会持续断言这一点。

## 7. PR 与验收证据

每个 PR 必须注明本文件的阶段、迁出的源文件、目标包、是否新增中性端口，以及影响的 API/状态/配置回归。若改动 `internal/server`，还必须列出该端口为何无法由 desktop/productruntime 替代、`OCTO-FORK` 位置和删除/收敛条件。

### 7.1 `server-diff-guard`（**已落地** 2026-09-11）

`apiProduct` 这类“逐行改写上游”的改动，靠人工评审看不住 —— 它每次都很小、每次都很合理，累计起来才是问题。因此做成**棘轮（ratchet）**：把今天的债务登记为上限，只允许降。

```text
scripts/server-diff-guard.mjs        断言 vs 上游 main：
  R1  apiProduct 出现次数 ≤ 161（实测：上游 0，157 个调用点 + 1 定义 + 3 注释）；
      目标 0，只允许降不允许升
  R2  登记文件（server.go 484、handlers.go 83、ws_handlers.go 36、
      native_handlers.go 31、attachments.go 27、lightapps_handlers.go 14）
      的 fork diff 行数 ≤ 上限，只允许降
  R3  internal/server 下匹配 product_*/privacy/chatmode_handlers/
      sensitive_dict_handlers 的文件必须在 PRODUCT_FILES 登记收敛工作包
```

**为什么是棘轮而不是“必须为 0”**：`01A` 是分阶段做的（B/C/D/E），一上来把目标设成 0 会让 guard 永远红着，从而被忽略。棘轮让每次折叠都立刻被 CI 确认，同时任何新增都当场失败。

**上限表改高 = 触发人工确认**：`DEBT_CEILINGS` / `PRODUCT_FILES` 是唯一可调的地方，改它属于 [开发规范](../../../开发规范.md) §3.7 的人工确认事项。这样“又顺手多改了几行”会在 CI 里显式暴露，而不是在半年后的合并里。

**执行**：`make server-diff-check`；CI 的 `server-diff-guard` job。需要 `fetch-depth: 0` 并有 `main` ref（guard 拿它当上游基线）。

**首次运行即抓到三件事**（说明它确实在做事，而不是摆设）：`apiProduct` 的真实计数是 161 而非我先前手数的 157（漏算了函数定义与 3 处注释）；产品测试文件也属于上游树里的产品债；以及 6 个上游 handler 的具体债务行数。

### 7.2 最终验收

最终验收至少包括：

1. `git diff` 与静态搜索证明**产品业务**（产品字段、`product_*.go` handler、产品 gate、词库 HTTP、积分扣减、产品 sender 包装）不再遗留在 `internal/server`；`apiProduct` 出现次数为 0（由 `server-diff-guard` 断言）。**验收对象是 A 类，不是"对上游 diff 为空"** —— B 类（§5.1）永久保留，其 diff 只允许降不允许升。
2. server 的通用 HTTP/WS/session/MCP/channel/tool/background 回归通过；`server.New` 仍读取 `config.yml`，且**启动期不再写用户 `config.yml`**。
3. productruntime 的 API、状态、门控、词库、隐私和 sender 装配测试通过；P0-05 后再增加 production gateway 唯一出口断言。
4. production/developer 的入口可见性、功能保留和权限判定符合 P0-00A/P0-04；任何隐藏入口都不能充当授权或删除功能的证据。

## 8. 修订记录

| 日期 | 说明 |
| --- | --- |
| 2026-09-11 | 初版：边界、精确盘点、`apiProduct` 折叠、阶段 B–E、`server-diff-guard`。 |
| 2026-09-11 | 补 §5.1「恢复上限」：区分 A 类（产品业务，恢复）与 B 类（datapath 强制承载，永久保留），给出 B 类实测清单（58 个测试文件 + 11 个非测试文件 18 处），并修正 §7.2 的验收口径，避免被误设成"对上游 diff 为空"。 |
| 2026-09-11 | 产品门端口从「中性 `func(http.Handler) http.Handler`」改写为具体 `runtimeport.ProductGate`（注册期拿到 route pattern）：原形态与「豁免按 pattern 判断」互相矛盾，实现者只能自行发明一套。§2.1 豁免集由散文清单改为精确到 `文件:行号` 的表（4 条 `s.api(` + 4 条非 `requireAuth` 路由），并声明豁免集唯一来源是 productruntime 的导出常量。 |
| 2026-09-11 | 外部审计落地：① §2 补登**既有包** `internal/productgate` —— 此前只登记了「要新增的端口 `runtimeport.ProductGate`」，没登记「已有实现是谁」，做端口 2 的人极易新写一套（§3.5/§3.8 都禁止）；并写明现状 `Gate.Middleware(next)` 拿不到 route pattern，正是 §3 端口 2 否决的形态；② §2 的 `chatmode_handlers.go` 行明确**唯一 owner 是阶段 D**，投影接入必须与迁出同 PR（此前 `05` 实施步骤 3 也自认领了它）；③ 新增 §3.1「未接通项：`Resolve` 的 ctx 是死参数」—— 记录现状、临时约束（工厂实现不得依赖取消语义）、owner（阶段 C 腾额度 + 阶段 D 接通）与验收。 |
| 2026-09-11 | **代码审计（P0-02 A/B 完成后）**：§3.1 增补「端口 4 的作用域窄于绕过面」—— 审计发现**第二类**绕过（视觉描述器 #10、lite sender #11、channel 模型绑定 #12），它们不问 sender 是谁、各自从 `data/config.yml` 造 client，端口 4 管不到，其中 channel 那条出厂即启用（`production.json` 的 `"channels": true`）。收口点实测只有两个（`cachedSenderForEntry` 覆盖 lite+channel，视觉描述器独立一处），方案与选项见 [P0-01 §B1.0a](01-运行时Profile与窄端口抽象.md)。**收口完成前"production 只走网关"不成立**；收口动作并入阶段 C（`server.go` 543/543 零余量）。 |

