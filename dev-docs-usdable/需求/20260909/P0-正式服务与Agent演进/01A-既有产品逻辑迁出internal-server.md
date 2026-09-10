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

| 当前位置 | 当前职责 | 处理 | 目标归属与验收 |
| --- | --- | --- | --- |
| `product_handlers.go`、`product_nickname.go`、`product_prefs.go` | 产品状态、退出、语言、昵称、偏好 HTTP | **迁出** | `productruntime/httpapi`；产品路由测试随实现移动，状态读写及语言/昵称校验结果不变。 |
| `product_login.go` | 本地验证码、激活、账号状态 | **迁出后替换** | 先迁入 `productruntime/httpapi` 的兼容适配层；P0-02 改为 `productclient.AuthClient`，P0-08 接管凭证保存。不得把固定验证码、激活码、进程内会话或本地 token 作为新接口契约。 |
| `product_sensitive.go`、`sensitive_dict_handlers.go` | 输入检查、词库编辑、输出过滤所需状态 | **迁出** | HTTP 与词库编排进入 `productruntime`；词库文件和 `internal/sensitive` 算法保持复用。P0-06 决定正式词库/隐私合同，不能在 server 内扩展产品规则。 |
| `chatmode_handlers.go`、`privacy.go` | 会话模式、默认模式、发送副本的隐私上下文 | **分阶段迁出** | P0-04 的目录/策略替换旧模式选择，P0-05 通过中性 turn hook 或 sender 装配接入。会话装载、绑定锁和 WS 生命周期留在 server。 |
| `handlers.go`、`ws_handlers.go`、`tasks_handlers.go` 的 `consumeCredit()` 与产品门/隐私调用点 | 本地积分、产品门、发送前产品策略 | **删除或改接中性端口** | 本地积分在 P0-05 删除；产品门和发送策略改由 productruntime 提供的通用请求/turn 适配执行，不在通用 handler 内写产品分支。 |
| `server.go` 的产品字段、产品状态初始化、产品 sender 包装、产品路由注册 | 产品状态被耦合进 server 构造和路由 | **迁出** | desktop 构造 `productruntime`；server 只接收中性扩展接口。`server.New` 的配置读取、通用 sender/session 初始化及 generic route 注册保留。 |
| `server.go` 的 `apiProduct()` 与全部 157 处 `s.apiProduct(` 调用点 | P3 为产品门把**整张上游路由表逐行改写**：上游 `main` 里 `apiProduct` 出现 **0** 次，`main` 的 147 处 `s.api(` 被替换 | **还原 + 折叠为顶层中间件（本任务最大的一笔回收）** | 见下文「apiProduct 折叠」一节。通过条件：`registerRoutes()` 与上游逐行一致，`internal/server` 不再存在 `apiProduct` 符号，而产品门语义（默认拦截、显式豁免）不变。`server-diff-guard` 以棘轮断言（基线 161，目标 0）。 |
| `ensureLocalEndpoint()` 与 `local_endpoint_test.go` | 为既有本地模型配置种入 4 个 `buding-*` 假模型 endpoint | **已删除**（2026-09-11） | 假模型不再是产品能力：正式模型列表来自中台签名目录并本地缓存（见 P0-04 模型目录）。删除后 `server.New` 不再写用户 `config.yml`，也消除了「seed 被 access-key 保存覆盖」那类读改写竞态。 |

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
2. **豁免集**（与现状等价，实现前须逐条核对）：`/api/health`、`/api/version`、MCP OAuth callback、静态文件、`/api/product/state`、`/api/product/locale`、`/api/product/send-code`、`/api/product/login`、协议/legal 路由。
3. **包裹层级**：中间件只能包在 `requireAuth` **那一层之内**，不能包整个 mux —— 否则会拦到心跳、静态文件、OAuth callback 等未登记路由。
4. **豁免判断用路由 pattern，不用路径字符串**：避免路径前缀匹配把 `/api/product/state/../chat` 之类误判为豁免。
5. **不保留 `apiProduct` 符号**：折叠后该名字应从 `internal/server` 完全消失（`server-diff-guard` 会断言，见 §7）。

**回归**：现有 `apiRoutes` 覆盖测试（断言每条路由都拒绝无 key 的非 loopback 请求）必须继续通过，并**新增**一条断言：未登录时被保护的样本路由返回产品门拒绝、豁免集路由不被拦。

## 3. 唯一允许的通用协作端口

产品 HTTP 和产品门目前直接依附于 `Server` 的私有 mux、`apiProduct`、会话绑定和 sender。为了迁出而复制这些通用机制同样不可接受。P0-01A 只允许在证明 desktop 不能完成装配后增加一个中性端口；它必须使用独立的中性包（建议 `internal/runtimeport`），不能把 `product`、账号、积分、词库、模型供应商等名词写进 `internal/server` API。

最小端口按以下顺序引入，未被迁移步骤需要时不创建：

1. **认证后路由注册器**：server 提供已完成机器访问校验、统一 no-store 的 `RouteRegistrar`；productruntime 注册自己的 HTTP handler。产品门作为 productruntime 的 handler/middleware，而非 `server.apiProduct`。
2. **请求过滤器 / 产品门中间件**：`apiProduct` 折叠**必须**用这个端口。形态是一个中性 `func(http.Handler) http.Handler`，由 productruntime 实现（其 allowlist、登录判定和错误体都由 productruntime 持有），在 `internal/server` 内以顶层中间件方式接入。server 不保存产品状态，也不逐路由改写路由表。这是 P0-01A 唯一必须引入的端口 —— 不做它就得保留 157 处 `apiProduct` 改写。
3. **turn 上下文钩子或 sender 装配点**：仅当 P0-05 迁出隐私/发送前策略时使用。输入只能是 `context`、`agent.Session`、通用 sender/请求元数据；不得暴露 server 私有锁、mux 或产品状态。

`internal/productruntime` 可依赖上述中性 `runtimeport` 接口，但不得依赖 `internal/server` 的实现或私有类型；`internal/server` 也不得导入 productruntime。由 `cmd/octo-desktop` 负责把两者装配起来。这是为了移除已有耦合的最小例外，不是为产品业务扩大 server API。

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

1. `git diff` 与静态搜索证明产品字段、`product_*.go` handler、产品 gate、词库 HTTP、积分扣减和产品 sender 包装不再遗留在 `internal/server`；`apiProduct` 出现次数为 0（由 `server-diff-guard` 断言）。
2. server 的通用 HTTP/WS/session/MCP/channel/tool/background 回归通过；`server.New` 仍读取 `config.yml`，且**启动期不再写用户 `config.yml`**。
3. productruntime 的 API、状态、门控、词库、隐私和 sender 装配测试通过；P0-05 后再增加 production gateway 唯一出口断言。
4. production/developer 的入口可见性、功能保留和权限判定符合 P0-00A/P0-04；任何隐藏入口都不能充当授权或删除功能的证据。
