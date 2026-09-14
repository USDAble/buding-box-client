# 本地 API 契约（`/api/product/*`）

> **状态**：`v0.16` —— **版本号与变更内容都看 [`§6 修订`](#6-修订) 的顶行**，本节不复述（本行此前停在第 `v0.10` 而修订表已到 `v0.12`：同一个版本写两处，第二处必然落后 —— `V-56` 同型，2026-09-14 收敛）。
> **本批次第二份契约**，解决 [`需求基线.md`](需求基线.md) §5.1 `S-1` 与 §5.2 `PQ24`。
> **权威实现落点**：`internal/productruntime`（[`需求基线.md`](需求基线.md) `G1`）。**不继续加进 `internal/server`**——`internal/server/product_*.go` 是 20260909 线上的旧落点，只作参考。
> **与《中台交付包》的分工**：本文件管「Web UI ↔ 本地 Go 服务」；[`中台交付包.md`](中台交付包.md) 管「本地 Go 服务 ↔ 中台」。两份契约**不互相复制字段定义**，只引用结论。

---

## 0. 怎么读

- 本文件是本地接口的**唯一登记表**（`开发规范` §3.8）。前端 TS 类型与 Go handler **都引用它**，不得反向定义。
- **状态列**：
  - `✅ 现状` —— 与本分支当前代码一致，可直接照此实现；
  - `🚧 修订` —— 本契约新定，代码尚未实现（**先改代码，不是改文档**）；
  - `⏳ 待补` —— 尚未定，登记在同一行的「未定」栏。
- **变更流程（三处同改，缺一不可）**：① 本文件 → ② Go handler + 测试 → ③ 前端类型 + 测试。契约里登记的端点与错误码若未被实现，CI 应当失败（见 §5 `S-1` 的契约测试）。

### 0.1 本文件的依据与可信度（重要）

`v1` 上**后端的落地是部分的**（2026-09-12 更正，原文说"还没有真后端"已过期；**2026-09-15 再更正：§1.4 的 15 条端点现在全部已注册**）：[`internal/productruntime`](../../../internal/productruntime) 已随 `PR-2b1` / `PR-2b2a` 落地并接进服务，**§1.4 的 15 条端点**（`state` / `send-code` / `login` / `logout` / `locale` / `nickname` / `prefs` / `chat-modes` / `sensitive/dict` 的 `GET`＋`PUT` / `sensitive/dict/import` / `control-plane` / `catalog` / `credits` / `sensitive/check`）**都在 `Mount` 表里有对应一行、可反向核对**；最后一条 `POST /api/product/sensitive/check` 由 `PR-6b3`（2026-09-15）挂上，`V-24` 随之清零（第 3 行 `login` 的 `🚧` 说的是**形状仍在改**，不是未注册）。所以本文件的可信度是**混合**的：

| 依据 | 位置 | 可信度 |
| --- | --- | --- |
| 已落地的 Go handler（**最高**） | `internal/productruntime/`（上列 14 条 ＝ 该包 `Mount` 表里除一条之外的全部 `api(...)` 行）+ `internal/productclient/dto.go` | **高** —— 已实现且经测试，与它不一致的是本文件而不是代码 |
| 前端调用与解析逻辑 | `web/src/lib/product.ts`、`api.ts`、`sensitive.ts`、`sensitiveDict.ts` | **高** —— 前端必须这样解析才能工作，字段名与信封形状是硬事实 |
| ~~临时假后端~~ | **已拆除（`PR-3`，2026-09-13）** —— 原 `web/src/dev/devBackend.ts`。它曾是一处"中等"可信度来源（替身形状），也因此让 `V-24`/`V-46` 两条"路由根本没注册"的缺口在界面上不可见 | **无** —— `rg devBackend web/src` 已无命中；凡此前只标"取自假后端"的行，形状以 `internal/productruntime` 的实现为准（见各行的 2026-09-13 更正） |
| 本文档的设计决定 | 本文件新定 | **待实现** —— 一律标 🚧 |

因此每行的状态要这样读：`✅ 现状` = **前端已依赖此形状**（改它要动前端，不是纯后端改动）；`🚧 修订` = 本契约新定，代码尚未实现。

**标 `✅` 不等于「后端已验证」**（2026-09-12 收紧）。上一句只保证"前端依赖它"。要算**后端已验证**，还须该行落在上表第一行的已实现端点里。**2026-09-14 更正（`V-24` 的相邻面）**：此前这条口径与 §1.4 的 ✅ 列合起来仍会误导 —— 四条词库/检测路由在 §1.4 标着 `✅`、在这里又被列进"其实没有后端"，读者分不出哪一列在说形状、哪一列在说存在（**2026-09-14 晚：其中三条已由 `PR-6b2` 挂上；2026-09-15：最后一条 `sensitive/check` 由 `PR-6b3` 挂上，四条全部落地**）。**现在 §1.4 的状态列直接标存在性**（⬜ ＝ 该路由今天没有注册），两处不再互相打架。

---

## 1. 通用约定

### 1.1 传输与鉴权

- 请求与应答体一律 `application/json; charset=utf-8`。
- 桌面外壳启动时生成**内存态 window token**，注入 webview URL；前端把它落到 `sessionStorage` 后，每个请求带 `X-Octo-Window-Token` 头（`web/src/lib/product.ts` 的 `WINDOW_TOKEN_HEADER`，`api.ts` 的 `withWindowToken`）。
- **产品门只对外壳窗口生效**。普通浏览器（`octo serve` / 开发期）没有 token，产品门形同不存在，`phase` 直接 `ready`。因此本组端点在浏览器里可能返回 404 或空态——**这不是错误**。
- 不使用 Cookie，**前端不发送 `Authorization`**；中台的 bearer token **绝不经过前端**（作用域：前端 ↔ 本地服务。本地服务 ↔ 中台 用 `Authorization: Bearer`，那是 [`中台交付包.md`](中台交付包.md) 的事）。

### 1.2 错误信封（重要，易漏）

本组端点**不用 HTTP 状态码承载业务语义**。只有下面几种 JSON 信封，**必须能区分**（「限流」是业务级的一个特化形状，共用 `code` 家族）：

| 类型 | 形状 | 触发 | 前端行为 |
| --- | --- | --- | --- |
| **产品门** | `403 {"error": "product_gate"}` | 任何受门保护的路由在未登录时 | `api.ts` 捕获后把 `productPhase` 置 `blocked`，回到登录门（**不静默**） |
| **字段级** | `400 {"fieldErrors": {"<field>": "<code>"}}` | 能定位到某个输入框 | code 映射到该输入框下方文案（`product.err_*`） |
| **业务级** | `400/403/409 {"code": "<code>", "phoneMasked"?: "138****1234"}` | 无法归到单个输入框 | 顶部横幅/对话框；`phoneMasked` 用于「此目录已绑定 138\*\*\*\*1234」 |
| **限流** | `429 {"retryAfterSec": <int>}` | 仅 `send-code` | 倒计时结束后允许重发 |

- HTTP 状态约定：`400` 字段/业务错误、`403` 产品门或账号受限、`409` 状态冲突、`429` 限流、`5xx` 本地或上游故障。
- **用户可见文案不写在这里**。文案的唯一来源是 `web/src/lib/i18n.ts`（`product.err_*` ——— 本文件只登记 machine code，中文/英文取值见该文件）。
- `api.ts` 另外容忍 `{"error": "..."}` 与 `{"message": "..."}` 两种泛用形状（旧端点沿用）。

### 1.3 状态对象 `ProductStateDTO`

所有「返回状态」的端点共用同一形状（Go 侧 `productstate.PublicState`）。**脱敏是硬约束**。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `schemaVersion` | number | 状态文件结构版本，**只增不减**（`S-5`，见 [`需求基线.md`](需求基线.md) §5.1） |
| `loggedIn` | boolean | 决定前端 `phase` |
| `activated` | boolean | 是否已激活；`loggedIn===true` 时恒为 `true` |
| `activation` | object \| null | `{activatedAt, expiresAt, boxCode?}`。`boxCode` 是**明文盒子编号，不脱敏**（`E1` 规则 7）；老数据缺字段时为 `null`，前端显示「—」 |
| `account` | object \| null | `{phoneMasked, nickname, lastLoginAt}` |
| `credits` | object | `{balance: <int64>}` —— **只有余额一个字段**（2026-09-14 收敛）。原 `monthUsed` / `monthKey` **已删除**：中台账本只给 `balanceMicroCredits`，而 `E9` 规则 5 明令客户端不做跨月加总 ⇒ 两个字段**既无来源、又不许本地算**，留着只会永远显示 0（凭空造出来的事实）。判定与理由见 [`需求基线.md`](需求基线.md) `E9` 规则 7；**本文件是该对象的字段表 owner**（§3.8） |
| `plan` | object | `{name}` |
| `prefs` | object | `{locale, inputSensitiveCheck, defaultChatMode}` |
| `suppressOnboarding` | boolean | 桌面构建跳过首启向导（P9） |

**永不出现在这里**：任何 token、明文手机号、`U盘激活码`（一次性凭证，只在激活当次使用，不留存）。完整字段表见 [`需求基线.md`](需求基线.md) `E6.1`。

### 1.4 端点总表

| # | 方法 | 路径 | 需登录 | 应答含 `state` | 状态 |
| --- | --- | --- | --- | --- | --- |
| 1 | GET | `/api/product/state` | 否 | 直接返回状态 | ✅ |
| 2 | POST | `/api/product/send-code` | 否 | 否 | ✅ |
| 3 | POST | `/api/product/login` | 否 | `{state}` | 🚧 加 `boxCode` |
| 4 | POST | `/api/product/logout` | 是 | 否 | ✅ |
| 5 | PUT | `/api/product/locale` | 否 | 否 | ✅ |
| 6 | PUT | `/api/product/nickname` | 是 | `{state}` | ✅ 已注册（`PR-6b1`，2026-09-14） |
| 7 | PUT | `/api/product/prefs` | 是 | `{state}` | ✅ 已注册（`PR-6b1`，2026-09-14） |
| 8 | GET | `/api/product/chat-modes` | 否 | 否 | ✅ 新形状已实现（`PR-4d`，2026-09-12） |
| 9 | GET | `/api/product/sensitive/dict` | 否 | 否 | ✅ 已注册（`PR-6b2`，2026-09-14；§2.9） |
| 10 | PUT | `/api/product/sensitive/dict` | 是 | 否 | ✅ 已注册（`PR-6b2`，2026-09-14；§2.10） |
| 11 | POST | `/api/product/sensitive/dict/import` | 是 | 否 | ✅ 已注册（`PR-6b2`，2026-09-14；§2.11） |
| 12 | POST | `/api/product/sensitive/check` | 否 | 否 | ✅ 已注册（`PR-6b3`，2026-09-15；§2.12）—— **`V-24` 的最后一条欠账清零** |
| 13 | GET | `/api/product/control-plane` | 否 | 否 | ✅ 新增（`PR-2c`，见 §2.13） |
| 14 | GET | `/api/product/catalog` | 否 | 否 | ✅ 新增（`PR-4c`，见 §2.14） |
| 15 | GET | `/api/product/credits` | 是 | 否 | ✅ 新增（`PR-5d1`，见 §2.15） |

> **状态列说的是「这条路由今天在不在」**（2026-09-14 明确，`V-24`）。判据是可核对的一件事：它在 `internal/productruntime` 的 `Mount` 表里有没有一行 `api(...)`。**在此之前这一列只表达了"形状定了没有"**，于是四条词库/检测路由挂着 `✅` 而代码里一条都没有（2026-09-14 晚 `PR-6b2` 挂上三条，2026-09-15 `PR-6b3` 挂上最后一条 `sensitive/check`，**17 条全绿**） —— 这正是 `V-24`（前端 `sensitive.ts` / `sensitiveDict.ts` 调得很顺，真机一律 404）能在文档里长期不可见的原因：**"形状已定"和"路由已挂"被同一个记号表达了**。`🚧` 仍表示"形状本身还在改"（只有第 3 行）。

> **「需登录」列是设计约定，不是观测事实。** 前端对 `/api/product/*` 一律带 window token（`api.ts` 的 `withWindowToken`），**产品门是否拦某条路由由服务端决定**，前端不区分。本列表达的是**应有的门策略**：凡读写账号数据或改词库的都要登录；`state` / `locale` / `send-code` / `login` 必须在未登录时可达，否则登录页根本渲染不出来。
>
> **但这层「需登录」判据本轮尚未实现**（2026-09-12 更正，原文给了一条已被推翻的实现指引）：已落地的 `internal/server` 产品门是**注册器的属性**，对经 `MountAPI` 缝注册的**全部**产品路由**一视同仁** —— 它校验的是窗口身份（`X-Octo-Window-Token`），**不是登录态**，因此没有、也不应有"按前缀白名单放行四条"这回事（那条路线曾定为"宽门"，落地前被实测推翻，理由见 [`开发计划.md`](开发计划.md) §4.1 第 12 行与 [`待解决问题.md`](待解决问题.md) `D-006`）。**按激活态区分可达性需要服务端读 `internal/productstate`**，那是 `D-006` 记的未做项，不是门的事。

---

### 1.5 会话级路由（**不在 `/api/product/*` 之内**）

> **为什么单开一节、而不是并进 §1.4 / §2：** 那两处的编号是"产品端点"的编号（现有 13 条，`开发计划` §1.1 与 `E2E闭环清单` 的判据都在引用它们），把一条会话路由塞进去会连带改动那些编号。**本节的路由由 `internal/server` 直接注册，不经 `MountAPI` 缝** —— 因此它**不过产品门（窗口令牌）**，只走 `requireAuth`，与同族的 `PATCH /api/sessions/{id}/permission_mode` 同信任级（这是 §1.1 那条"门是注册器的属性"的直接推论）。

| 方法 | 路径 | 语义 | 状态 |
| --- | --- | --- | --- |
| PATCH | `/api/sessions/{id}/chat_mode` | 设置**本会话**的模式（`privacy` / `smart` / `default`） | ✅ 新增（`PR-4d1`，2026-09-13；修 `V-46`） |

- **请求**：`{"chat_mode": "<mode id>"}`。
- **应答 `200`**：`{"ok": true, "chat_mode": "<mode id>"}`。
- **错误**：`400`（mode id 不在 `internal/chatmode` 的产品模式集合内 —— **该集合是唯一 owner**，逐字校验，不猜前缀）；`404`（会话不存在，body 是 JSON `{error}`）；`409`（会话被另一入口占用，与 `permission_mode` 同规矩）。
- **副作用**：写进会话文件（`chat_mode` 字段，**随 `data/` 跨机器走** —— `需求基线:357`），并广播一条 `session_update`（载荷含 `chat_mode`；`session_update` 是**上游既有事件**，前端 `ChatView.svelte:1202` 的消费者早已存在）。
- **为什么不是 `chat-mode`（连字符）**：五个兄弟路由全是下划线（`permission_mode` / `reasoning_effort` / `show_reasoning` / `working_dir` / `agent_profile`）。前端原来那半是"连字符 + `PUT`"，而**服务端从来没有这条路由**（`V-46`）⇒ "保持前端现状"没有任何兼容收益，只剩一处不一致。
- **字段名逐字一致**：`chat_mode` 是**线上与落盘的同一个名字**（`Session` 结构体标签、`sessionItem` 标签、WS 载荷、前端 `Session` 类型 —— 四处必须一致，见 `开发计划` §4.2）。
- **它是 `B5` 规则 6 的那一半**：会话级**模型**早已持久化（`PATCH /api/sessions/{id}/model`，属上游既有路由，不登记在此），会话级**模式**此前从无落点（`V-46`）；两条合成闭环 `L-C8`。

---

## 2. 逐端点契约

### 2.1 `GET /api/product/state` ✅

读产品状态。**是前端启动的第一个调用**：`refreshProductState()` 用它决定 `phase`。

- **请求**：无体。
- **应答 `200`**：直接是 `ProductStateDTO`（**不包 `state`**）。
- **错误**：`403 product_gate`（仅当带了 token 但已失效——此时前端应回登录门）。
- **落点**：`internal/productruntime` 的 state reader。
- **备注**：读磁盘失败**不得**让外壳卡死。前端已有兜底：失败 → `phase = blocked`（让用户重试登录，而不是看转圈）。这属**有界降级**，目标明确、用户可见、可恢复（§3.9）。

### 2.2 `POST /api/product/send-code` ✅

请求短信验证码。

- **请求**：`{"phone": "<11 位>"}`
- **应答 `200`**：`{"cooldownSec": 60}`
- **错误**：
  - `400 {"code": "invalid_phone"}` —— ⚠️ **注意信封**：手机号错误走的是**业务级 `code`**，不是 `fieldErrors.phone`。前端在 `product.ts:177` 读到 `body.code` 之后**自己把它落到 `phone` 字段**上（`new ProductError(status, { phone: body.code })`）。
    这是 §3 那类「命名不要统一掉」的又一个实例：**信封类型由服务端决定，字段归属由前端决定**，两者解耦。实现时若"顺手"改成 `fieldErrors.phone`，前端会把 `invalid_phone` 读成 `undefined` 而回落成默认文案——**不报错，但文案错**。
  - `429 {"retryAfterSec": <int>}` —— 冷却期内重复请求
- **落点**：`internal/productruntime` → 中台 `POST /v1/auth/sms-code`（见《中台交付包》§3.2）。
- **备注**：`send-code` 的**响应体预留 `captchaToken` 位**（`PQ23`）：将来中台返回 `{"captchaRequired": true}` 时前端才渲染人机验证 UI；P0 不渲染、不校验。

### 2.3 `POST /api/product/login` 🚧（加 `boxCode`）

首次激活 + 登录，或二次登录。**首启的门面**。

- **请求**：
  ```json
  {
    "phone": "13800001234",
    "code": "123456",
    "nickname": "张三",
    "activationCode": "…",   // 仅首次激活
    "boxCode": "BOX-…"       // 仅首次激活；二次登录两者都省略
  }
  ```
  首启传 **五个字段**；二次登录只传 `phone` / `code` / `nickname`（`activationCode`、`boxCode` 省略）。
- **两个凭证是一对，可选**（2026-09-13 更正，`V-45` / `PQ28`）：**要么都给、要么都不给**，本地只查这个**形状**；"这次是不是首启"**不由客户端判定**（`开发规范` §3.8）—— 用本地 `activated` 推断首启，正是把丢 `data/` 的用户（激活码已一次性用掉）逼到"只能联系客服"的那条规则。半填仍然本地拒绝（`invalid_activation` / `invalid_box_code`），所以用户不必为了"少填一个"跑一趟中台。**无凭证的登录是否被接受，由中台答**：若该手机号已有可用授权，就签发令牌并回带激活记录（客户端据此回填 `boxCode`，见 `E1` 规则 2 / `PQ28`）；若没有，回 `activation_required`。
- **应答 `200`**：`{"state": ProductStateDTO}`
- **错误**：
  - 字段级 `400 {"fieldErrors": {...}}`：
    | code | 归属字段 | 含义 |
    | --- | --- | --- |
    | `invalid_phone` | `phone` | 手机号格式 |
    | `invalid_code` | `code` | 验证码格式（未发送/位数不对，**与业务级 `invalid_code` 不同**） |
    | `nickname_format` | `nickname` | 昵称格式 |
    | `nickname_sensitive` | `nickname` | 昵称命中敏感词 |
    | `invalid_activation` | `activationCode` | **空或格式**不合法（客户端也预校验） |
    | `invalid_box_code` | `boxCode` | **空或格式**不合法（客户端也预校验，`BlockedView.svelte`） |
  - 业务级（状态码取自 `中台交付包` §3.2，**不是一律 400**）：
    | code | 含义 | 用户要做的事 |
    | --- | --- | --- |
    | `code_not_sent` | 还没获取验证码 | 点「获取验证码」 |
    | `invalid_code` | 验证码错误或过期 | 重填 |
    | `activation_invalid` | 激活码校验失败（中台侧） | 核对激活码 |
    | `activation_code_used` | **该激活码已被使用过**（一码一用） | **联系客服**——这是唯一出路 |
    | `box_code_unknown` | 盒子编号不认识 | 核对编号 |
    | `box_code_mismatch` | 激活码与盒子编号不匹配 | **联系客服** |
    | `activation_required` | **该手机号没有可用授权**（`403`；`中台交付包` §4.2 的登录必备码） | **去激活** —— 客户端渲染「该手机号尚未注册和激活！」，并给出跳到激活表单的链接。**必带 `200`/`403` 之分**：它是业务级，不挂 `field`（问题在账号，不在某一个字段） |
    | `phone_mismatch` | 本目录已绑定另一手机号 | 随响应带 `phoneMasked` |
- **落点**：`internal/productruntime` → 中台 `POST /v1/auth/login`。
- **规则**（`E1`）：
  - `activationCode` 与 `boxCode` 是**两个独立凭证**：前者答「这份授权买过没」，后者答「属于哪台盒子」。服务端**分别校验**，因此错误码也必须分开——用户错一个字符要能看出错在哪一个。
  - **一个盒子编号可对应多个激活码**（一对多），所以**不存在** `box_code_already_bound`；实现时不要发明这个码。
  - **一个 `U盘激活码` 只能使用一次**。用掉即废，换机/重装一概不能复用，唯一补救是走客服。
  - **表单形态**：登录表单与激活表单**可以互跳**（两个跳转链接常驻，各在对面表单底部）。默认形状由 `/state` 的 `activated` 在**拦截页出现时取一次**决定，此后只由用户改变 —— **不在用户读消息时把手底下的表单换掉**（`V-44` / `V-45`，`P4-拦截页.md` §4.6）。链接**不按错误码出现**：按码显示就得在前端维护一份"哪些码露出按钮"的清单，即第二份激活家族分类（§3.8）。
  - **一次「没带激活凭证」的登录若被平台以激活类码拒绝，本地 `activated` 随之降为 `false`（`GET /api/product/state` 可见），应答码、信封与状态码不变（`V-44` / `PR-2d`）。** 判据两条**同时**成立才降：① 该次请求的 `activationCode` 与 `boxCode` 去空白后都为空；② 平台码属于 `activation_invalid` / `activation_code_used` / `box_code_unknown` / `box_code_mismatch` / **`activation_required`**（2026-09-13 补第五个：它比那四个更直接地说"这份授权不成立"，且正是"没带凭证"时会收到的那个码 —— `V-45` 之前替身错发 `activation_invalid`）。**理由**：`activated` 的 owner 是平台（`E1` 规则 6），本地那份只是它上次的答复。**反面同样成立**：传输失败、5xx、`invalid_code`、`code_not_sent`、`phone_mismatch` 一律**不降**（本地故障不是平台的答复）；`account` 与 `activation` 记录**保留**（`E7`）。判定在**服务端**（`internal/productruntime`）。
  - **2026-09-13 更正上面这条的用户可见半边（`V-45` / `PR-2e`）**：降 `activated` 是**数据**半边，那时前端还跟着重读、于是**自动**把表单切回激活表单 —— 现在不再自动切。表单形状在拦截页出现时从 `/state` 取一次初值，此后只由用户用两个跳转链接改变；用户看到的是**消息 + 「去激活」按钮**（详见 `P4-拦截页.md` §4.6）。所以："服务端纠正数据、前端不偷偷换表单" —— 前端仍会在业务级失败后重读状态（让 `productState` 与平台一致，授权页等消费者读它），但**重读不再决定形状**。
- **✅ 与本分支现状的关系**（2026-09-12 更正，原文写「后端尚无」且引用了分支 `feat/activation-box-code`，两者都已过期）：前端与**后端**都已有 `boxCode` 全链路 —— 后端见 `internal/productruntime` 的登录处理与 `TestFirstActivationReturnsWrappedStateWithServerBoxCode`（`boxCode` **取自服务端应答，不是表单值**）。原引用的分支 `feat/activation-box-code` **已不存在且无归档 tag**（内容已进 `v1`，见 `web/src/views/BlockedView.svelte` 的 `boxCode` 字段与 Go 侧同名 DTO），故不再作为落点引用。

### 2.4 `POST /api/product/logout` ✅

- **请求**：空体。
- **应答 `200`**：`{"ok": true, "revoked": true}` —— `revoked` 是 **2026-09-14（`PR-2f` / `V-54`）新增的字段**：`true` = 平台侧会话已撤销，`false` = **本机已登出但平台没撤销**（离线、超时、平台拒绝）。**老服务端不带这个字段时按 `false` 读**（前端 `body?.revoked === true`），因为"没说"与"没撤销"对用户是同一件事 —— 静默当作成功正是 `§3.9` 禁止的那种兜底。形状的依据是 Go handler 本身（`internal/productruntime/handleLogout`）。
- **副作用**：① **撤销平台会话**（`POST /v1/auth/logout`，`中台交付包` §4.1 #4）—— **这是 `PR-2f` 补上的半边**，此前只删本地文件、平台侧毫无效果（`V-54`）；② 清 `data/credential.json`；③ `productPhase → blocked`。**②③ 无论 ① 成功与否都执行**（`PQ29` 取法 ①，人工定）：离线登出是 U 盘产品的常态，把用户锁在登录态里等于让他去手删 `data/`。
- **备注**：**绑定手机号与昵称仍留在 `data/`**，供二次登录表单预填与比对（`E2`）。注销 ≠ 清数据。**`activated` 不降**（`E7`：登出不是取消激活）。

### 2.5 `PUT /api/product/locale` ✅

登录**之前**也要能保存界面语言。

- **请求**：`{"locale": "zh" | "en"}`
- **应答 `200`**：`{"ok": true}`（同上，2026-09-13 起依据为 Go 实现；`runtime.go:431`）
- **错误**：`400 {"field": "locale", "code": "invalid_value"}` —— **2026-09-14 统一（`PR-6b1` / `V-72`）**。§2.7 从一开始就是这个形状，而本节原先写的是 `{"fieldErrors": {"locale": "invalid_value"}}`，同一张 §3 码表于是描述了两种信封。统一到 `{"field": …, "code": …}` 的理由有三条：① 前端今天对这条路由**不解析应答体**（`setProductLocale` 只读 `res.ok`，`product.ts:494`），两条形状都没有消费者，改成哪个都不破坏界面；② §3 的 `fieldErrors` 是**转发平台**在登录路径上的字段级判断，而 `invalid_value` 是**本地自己**对「这个值不在可接受集合里」的判断（`envelope.go` 的 `fieldLevelCodes` 只有三条，`invalid_value` 本来就不在其中）；③ 同一个值在两条路由上被拒，用户该看到同一句话。实现上两条路由共用 `writeValueRefusal`。
- **备注**：默认语言**跟随系统语言**，非中文落 `en`（`PQ18`）。首启时由桌面壳把系统语言带进来，不靠前端猜。

### 2.6 `PUT /api/product/nickname` ✅

- **请求**：`{"nickname": "<新昵称>"}`
- **应答 `200`**：`{"state": ProductStateDTO}`
- **错误**：`400 {"code": "nickname_format" | "nickname_sensitive"}` —— **业务级**：应答体里**没有** `fieldErrors`。这两个码**同时**在 `fieldLevelCodes` 表里（那条路径服务登录表单的中台应答），所以这里最容易"顺手统一"成 `writeFieldErrors`；那样写**不报错**，只是前端 `body.code` 读到 `undefined` 并回落成「昵称格式不正确」——**命中敏感词会说成格式错误**。与 §2.2 的 `invalid_phone` 同型。
- **备注**：应答里的 `state` **就是持久化后的真相**（服务端回读自己的存储），前端直接覆盖共享 store，不再本地乐观更新。

### 2.7 `PUT /api/product/prefs` ✅

- **请求**：`{"locale"?: "zh"|"en", "defaultChatMode"?: "<mode id>", "inputSensitiveCheck"?: true|false}`
  未变的字段可省略，默认「保持不变」。
- **应答 `200`**：`{"state": ProductStateDTO}`
- **错误**：`400 {"field": "<字段名>", "code": "invalid_value"}` —— 信封由 `writeValueRefusal` 一处写出，`PUT /api/product/locale`（§2.5）同形（`V-72`）。
- **备注**：`defaultChatMode` 必须是 §2.8 目录里存在的 mode（`internal/chatmode` 是 mode 分组的唯一 owner，§3.8）。

### 2.8 `GET /api/product/chat-modes` ✅（新形状已实现，`PR-4d` 2026-09-12）

模式与模型选择器的数据源。

- **请求**：无体。
- **应答 `200`（新形状）**：
  ```json
  {
    "modes": [
      {
        "id": "privacy",
        "models": [{ "id": "…", "displayName": {"zh": "…", "en": "…"}, "compositeId": "endpoint::model" }],
        "defaultModel": "endpoint::model"
      }
    ],
    "catalogVersion": "…",
    "policyVersion": "…"
  }
  ```
- **✅ 与旧形状的差异（`S-4`）—— 六条已全部落地（`PR-4d`，2026-09-12）**：
  1. **删掉 `fallback: boolean`**。现在的类型注释写「`chat-modes.json` 不可读时用内置默认」——这正是 `B1` 已作废的行为。**顺着旧形状实现，会把「内置名单」带回来**。
  2. **模型的 `displayName`**（中英双语）来自中台签名目录。**前端不得保留 id→名称映射表**（`B6`，§3.8）。
  3. `id` 保持**英文 ASCII、不做本地化**（它是数据键，等同 `buding-*` 那类固定标识）。
  4. `catalogVersion` / `policyVersion` **两者都不是新拟字段，本行已按实际来源定稿**（原写"本契约新拟，若中台已有则以中台为准"）：`policyVersion` 取中台签名信封自己的 `policyVersion`，`catalogVersion` 取 `catalog.version`（`internal/productruntime/runtime.go` 的 DTO 逐字转发，`productclient.Policy` 里两个字段都在）。**前端只用于判断"目录是否换了一版"，不参与任何判定** —— 版本单调与降级判定都在 Go 侧（`§3.8` 一个 owner）。
  5. **`modes[]` 不带 `displayName`（2026-09-11 更正）。** 原第 5 条说它"来自模式分组"，方向错了：**模式的展示名是界面文案，不是数据**（`B5` 规则 1「展示名是界面文案、分组归属来自目录，两者不得互相推导」），而前端 i18n **已经拥有**这三个名字（`i18n.ts` 的 `mode.privacy` / `mode.smart` / `mode.default`，中英各一份）。让它同时出现在本应答里就是第二份真相（§3.8）—— 中台或后端改一处、前端显示另一处。**模式展示名一律由前端按 `mode.<id>` 渲染，本应答只给 `id`。**
  6. **`modes[].models` 是投影结果，其分组来源是模型自身的 `modeIds`**（见 [`中台交付包.md`](中台交付包.md) §4.3「`catalog.modes` 的形状」）—— 中台目录不在 `modes[]` 里再列一遍模型，本应答也不得据此再造一份分组事实。
- **降级（§3.9）**：目录不可用时的**目标行为**是**显示「目录暂不可用 + 重试」，不给内置名单**，也就是 fail-closed。不许回落本地 provider（`A1`/`B1`）。
  - **现状（`PR-4d` 之后、`PR-4c` 之前）**：没做的是**把四种原因分开说**。`PR-4d` 保证的是"不给内置名单"这一半 —— 无缓存 / 缓存不可读 / 投影为空一律返回 `200` + **空 `modes`**，响应里**没有** `fallback` 字段（正反两条断言在 `chatmodes_route_test.go`），且前端 `loadChatModes()` 出错时**保留上一份列表**而不是清空（清空会把"过期"文案误配到一次网络抖动上）。
  - **⚠️ 更正（2026-09-12 质检）**：本条原写"文案整体归 `PR-4c`"，容易被读成"今天界面上一片空白"。**不准确** —— 选择器对空组的既有文案 `mode.no_models`（「该模式下暂无可用模型」）照常渲染，`PR-4d` 并不屏蔽它。**准确的接缝**：今天「无缓存 / 过期 / 验签失败 / 分组空」**四种原因共用同一句**，用户无从判断该重试、该等、还是该去登录；把它们分开是 `PR-4c`（`L-C2`）的交付物。
- **落点**：`internal/productruntime` 投影中台目录 + `internal/chatmode` 做模式分组。

### 2.9 `GET /api/product/sensitive/dict` ✅（已注册，`PR-6b2` 2026-09-14）

- **请求**：无体。
- **应答 `200`**：`{"builtin": ["…"], "user": ["…"]}`
- **错误**：**无业务错误**。用户词文件缺失 ⇒ `200` 且 `user: []`（**不是错误、也不建文件** —— 缺文件等于「用户层为空」，内置词照旧生效）。
- **落点**：`internal/sensitive`（词库唯一 owner）。
- **备注**：`builtin` 是**随包内置词（`go:embed`）**，只读；`user` 是 `data/` 下的用户词文件。

### 2.10 `PUT /api/product/sensitive/dict` ✅（已注册，`PR-6b2` 2026-09-14）

整份覆盖用户词，且是「**改动即写盘**」。

- **请求**：`{"user": ["词1", "词2"]}`
- **应答 `200`**：`{"user": ["词1", "词2"]}` —— 回的是**去重 + 归一化之后真正写进文件的那份**，所以它可能与请求不同（内置词、重复词都会被去掉）。
- **错误**：`400 {"code": "invalid_word", "word": "<原词>"}` —— 该词**归一化后为空**（纯空白或纯符号），它会匹配任意文本，故整次写入被拒、**盘上逐字不变**。**业务级**，不进 `fieldLevelCodes`（见 §3）。
- **备注**：服务端**权威去重 + 空白/符号归一化**；前端的 `normalizeWord`（`sensitiveDict.ts`）只用于即时提示，不构成契约。两边的归一化规则必须保持同步（前端注释已注明这一约束）。**用户删不掉内置词**：内置词参与去重，写进来的同形词会被丢掉（`D4`）。
- **备注（文件形态）**：写盘**保留文件头部的注释与空行块**（用户自己写在那里的说明），**词之后的注释不保留** —— 这是刻意接受的取舍，不是缺陷。

### 2.11 `POST /api/product/sensitive/dict/import` ✅（已注册，`PR-6b2` 2026-09-14）

- **请求**：`{"words": ["…"], "dryRun": true|false}`
- **应答 `200`**：`{"added": <int>, "skipped": <int>}`
- **错误**：**无业务错误**。归一化后为空的词、重复词、与内置同形的词都**不是错误**，一律计入 `skipped`（导入是"尽力合并"，`PUT` 才是"整份替换"—— 那条对空词是拒绝，见 §2.10）。
- **备注**：`dryRun: true` 是**导入预览**（只统计不落盘）；`false` 才写盘。**合并非覆盖**：盘上已有的词与本次导入的词都在。

### 2.12 `POST /api/product/sensitive/check` ✅

输入框的即时检测（`P8`）。

- **请求**：`{"text": "<待检文本>"}`
- **应答 `200`**：`{"hit": true|false, "masked": "<命中词被替换为 * 的文本>"}` —— **没命中时 `masked` 就是原文**（字段始终在，前端不必判空）。
- **错误**：**无业务错误**（空文本 ⇒ `hit: false`）。
- **备注**：这是**给输入框做替换 + 提示**用的，不是权威。**服务端在聊天链路上必须再检一次**（前端可被绕过）——见 `P8-敏感词接入.md`。
- **落地（`PR-6b3`，2026-09-15）**：实现落在 `internal/productruntime`（§0.1 的落点纪律），**与回合门共用同一个 `sensitive.Engine` 与同一个用户开关**（`prefs.inputSensitiveCheck`，读在每次调用而非装配时 ⇒ 关掉后同一句话立刻放行）。这条路由只回答"这一句怎么打码"，**与"拒"无关**；拒绝发生在三个回合入口（见 §4 的 `input_sensitive`）。**装配期无引擎 ⇒ 不拒**（与 `Config.SensitiveEngine` 的回落同向，§3.9：回落目标是已验证的本地判据，不是另一个词源）。

### 2.13 `GET /api/product/control-plane` ✅ 新增（`PR-2c`，2026-09-12）

**这是第 13 条端点**，为 `L-B2` 的两页服务：让拦截页在**用户输入之前**就能分辨"这包不对"，而不是让用户先撞一次失败。

- **请求**：无体。
- **应答 `200`**：`{"configured": <bool>, "hasTrustedKeys": <bool>}`
- **判定位置**：**`internal/productprofile`** —— 两个判据的唯一 owner（`ControlPlaneConfigured()` / `HasTrustedKeys()`）。`internal/productruntime` **只转发**，不自己判断；`cmd/octo-desktop/product.go` 在装配期读一次 Profile 填入 `Deps`。
- **需登录：否。** 未登录时正是要显示它（本列的判据见 §1.4 的注：它表达应有策略；当前产品的门校验的是窗口身份，不是登录态，所以本端点与其余产品路由一样需要 window token）。
- **为什么不用 `GET /api/product/state`**（`P4-拦截页.md` §2.2）：`ProductStateDTO` 是**状态文件 `product-state.json` 的投影**（`E6.1`），而这两个值是**编译期 Profile 的事实**，与用户数据无关。塞进 state 会让"state = 磁盘状态"这条映射失效，也把两个 owner 混进一个 DTO。
- **前端消费规则（优先级从上到下，不可交换）**（`P4-拦截页.md` §2.2）：
  1. `configured == false` ⇒ 「未配置」页；
  2. `hasTrustedKeys == false` ⇒ 「无公钥」页；
  3. 两者都正常 ⇒ 登录表单；
  4. 本端点**读取失败** ⇒ 登录表单（保守：不因读不到而谎报"未配置"）。
  **顺序不可交换，且 `configured` 在前**：`configured == false` 时「无公钥」页的文案（"有地址但不信任密钥"）不成立 —— 这个包没有地址。「地址」是「公钥」的前置。落地时经测试纠正，初稿顺序写反，理由见 `P4-拦截页.md` §2.2。
- **文案约束（`A1` 规则 6）**：两页**都不出现 URL、不出现技术名词**。用户看不到 `apiHost`、`"production.json"`、`trustedKeyIDs` —— **这不是用户可以修的东西**，出现地址只会让人以为"是不是填错了"，而他们没有任何地方可填。
- **i18n 键**：`product.blocked.unconfigured_title` / `_body`、`product.blocked.no_keys_title` / `_body`（中英各一份，两页文案**必须能看出是两回事**）。

---

### 2.14 `GET /api/product/catalog` ✅ 新增（`PR-4c`，2026-09-13）

**这是第 14 条端点**，为 `L-C2` 服务：回答「目录能不能用、为什么不能」，让**发消息前**（`B4` 规则 1"不得开启新回合"）和**选择器为空时**（`B9` 四条文案）都有可上屏的判据。

- **请求**：无体。
- **应答 `200`**：
  ```json
  {"state": "ready|absent|stale|unverifiable", "retryable": <bool>, "catalogVersion": "<string>", "expiresAt": "<RFC3339 | \"\">"}
  ```
  **四个字段始终出现**（`absent` 时后两项为空串 / `false`）—— 前端靠**取值**分辨四态，靠"字段缺失"表达不了。

| `state` | 含义 | `retryable` | `B9` 文案键 |
| --- | --- | --- | --- |
| `ready` | 有验签通过且未过期的缓存 | `false` | （不出现；分组空才是文案） |
| `absent` | 从来没有缓存 | `true` | `catalog.absent` |
| `stale` | 有缓存但已过期，且刷新未成功 | `true` | `catalog.stale` |
| `unverifiable` | 收到过信封但**验签失败** | `false` | `catalog.unverifiable` |

- **判定的唯一 owner**：**`internal/productruntime`**（`assessCatalog`，三个输入：缓存有无 / 是否过期 / 最近一次刷新的结局）。前端**只做 i18n 取值**，不重新判断 —— 状态→文案的映射在 `web/src/lib/chatMode.ts` 的 `catalogNoticeKey()` 一处（`开发规范` §3.8）。
- **`retryable` 是服务端给的，不是前端推导的**：`unverifiable` 为 `false`，因为它的恢复途径是**平台换签名/密钥**，用户按键按不出来 —— 给一个按不动的重试按钮就是承诺做不到的事（§3.9）。
- **本端点是一次读，也是一次刷新**（`B3`"TTL 到期后下一次开选择器触发刷新"）：命中 `absent` / `stale` 时它**恰好发起一次**条件刷新（带 `knownVersion`），然后报告刷新后的状态。`unverifiable` **不刷新** —— `B4`"不自动重试同一份响应"。
- **需登录：否**（与其余产品路由同规矩，见 §1.4 的注：门的判据是窗口身份）。但**未登录时它大概率答 `absent`**，因为刷新需要会话。
- **为什么单开一条，而不是加进 `state` 或 `chat-modes`**：可用性是**运行时**事实（缓存 + 最近一次拉取），而 `ProductStateDTO` 是**磁盘文件 `product-state.json` 的投影**（`E6.1`）；只挂在 `chat-modes` 上则 composer 要为了"能不能发"先拉一次模型列表。三处都能各自造一份意见的地方，就要定一个 owner（§3.8）。形状照抄 §2.13（同样是"运行时装配事实 + 唯一 owner"）。
- **`expiresAt` 为空串而不是零值时间戳**：Go 的零值 `time.Time` 会序列化成公元 1 年，读起来像一个远比真实更早的过期时间。

---

### 2.15 `GET /api/product/credits` ✅ 新增（`PR-5d1`，2026-09-14）

**这是第 15 条端点**，为 `L-C4a` 服务：**余额的唯一写入路径**（`E9` 规则 2）。它做两件事 —— 拉一次中台账本，把结果落进本地投影，然后把落盘后的真相回给调用方。

- **请求**：无体。
- **应答 `200`**：`{"state": ProductStateDTO}` —— **与 §2.6 / §2.7 同形**（"写了一个字段之后回读自己"）。**不新增形状**：`credits` 对象已在 §1.3 登记，再开一个 `{balance}` 的扁平应答就是同一个事实的第二份定义（§3.8）。
- **错误**：
  - `401/403` 与其余需登录端点一致（按 §1.4 的注：当前产品的门校验的是**窗口身份**，不是登录态）；
  - 平台失败按**统一失败漏斗**（`internal/productruntime` 的 `failPlatform`）：`session_expired` ⇒ 清凭证 + 进拦截页（`L-A6`）；其余（`upstream_unavailable` / `invalid_signature` / …）⇒ 原样转发中台的码，**本地投影保持不变**。
- **为什么不是"顺带从别的应答里带出来"**（这是本条的设计要点，也是 `E9` 规则 2 的落地）：余额只能有一个来源、一个写入路径。若它同时从登录应答、终态帧、账本三处进来，就必然出现"两个数谁说了算"、"哪个更新"、"终端帧丢了怎么办"三类问题，以及**两版验收判据**（这正是 `V-11` 那条已被取代的条文留下的状态）。收敛之后：**任何"余额可能变了"的信号都只是"调用本端点"的触发器**。
- **登录路径**：登录成功后**也调用同一个函数**（`refreshCredits`），不复用登录已经打过的 `Bootstrap` —— 后者的应答里确实带着 `credits`，但让余额从登录应答里进来就是第二条写入路径。代价是登录多一次往返，**人工已明确接受**（2026-09-14：「客户端只需要老老实实从服务器定时刷新余额就行了」）。
- **`credits_update` WS 广播**：**本端点不发射它**（见 §4 的状态栏）。跨窗口一致性记在残余里、归属 `PR-8`。
- **需登录**：本表登记为「是」（它读写账号数据），实际门的判据同 §1.4 的注。

---

## 3. 错误码总表

**machine code 是契约；用户可见文案不在本文件**（唯一来源 `web/src/lib/i18n.ts`）。

| code | 层级 | 归属字段 / 场景 | i18n key |
| --- | --- | --- | --- |
| `product_gate` | 产品门 | 未登录访问受门保护路由 | （`api.ts` 内部处理，不渲染文案） |
| `invalid_phone` | 字段 / **业务** | `phone`；**`send-code` 走业务级 `code`**（见 §2.2） | `product.err_phone` |
| `invalid_code` | 字段 | `code`（格式） | `product.err_code` |
| `nickname_format` | 字段/业务 | `nickname` | `product.err_nickname` |
| `nickname_sensitive` | 字段/业务 | `nickname` | `product.err_nickname_sensitive` |
| `invalid_activation` | 字段 | `activationCode`（空/格式） | `product.err_activation` |
| `invalid_box_code` | 字段 | `boxCode`（空/格式） | `product.err_box_code` |
| `invalid_value` | **字段名 + 业务码** | 一个不在可接受集合里的值：`locale` / `prefs.*`。信封是 `{"field": …, "code": "invalid_value"}`，**不是** `fieldErrors`（§2.5 / §2.7） | — |
| `invalid_word` | **业务码 + 词** | 词库写入时**归一化后为空**的一个词（`PUT` / `import`）。信封是 `{"code": "invalid_word", "word": "<原词>"}`，**不是** `fieldErrors`（`V-75`，2026-09-14） | —（`PR-6c` 落文案；前端提交前已本地拦过同类词） |
| `input_sensitive` | 业务（**客户端本地码**） | 用户手敲的正文命中词表：`400 {"code": "input_sensitive", "text": "<已打码的正文>"}`（§2.12 是检测、本条是拒绝）；`/ws` 上同一判据走 `input_sensitive` 事件（§4）。码值登记在 `中台交付包` §3.2，与 `model_withdrawn` 同族 | `sensitive.hit_notice`（**已在** `i18n.ts`：中英各一份；前端用应答里的 `text` 回填输入框，不按 `code` 另取文案） |
| `code_not_sent` | 业务 | 未获取验证码 | `product.err_code_not_sent` |
| `invalid_code` | 业务 | 验证码错误/过期 | `product.err_invalid_code` |
| `activation_invalid` | 业务 | 中台拒激活码 | `product.err_activation_invalid` |
| `activation_code_used` | 业务 | 激活码已用（一码一用） | `product.err_activation_used` |
| `box_code_unknown` | 业务 | 盒子编号不认识 | `product.err_box_code_unknown` |
| `box_code_mismatch` | 业务 | 激活码与盒子编号不匹配 | `product.err_box_code_mismatch` |
| `phone_mismatch` | 业务 | 目录已绑其他手机号（带 `phoneMasked`） | `product.err_phone_mismatch` |
| `activation_required` | 业务 | **该手机号没有可用授权**（`403`；`中台交付包` §4.2 登录必备码） | `product.err_activation_required` |
| `control_plane_unconfigured` | 业务 | 本构建没有配置中台地址（`developer` 构建未填 Sandbox / 正式构建漏配） | —（S-6/S-7 未定，暂由前端兜底文案） |
| `unauthorized` | 业务 | **会话失效**：中台**明确拒绝**了 refresh token（`productclient.ErrSessionExpired`）。本地应答 **401** | —（**刻意不渲染文案**：前端 `noteSessionLost` 收到 401 即回拦截页，见 `P4-拦截页.md` §4） |

**三处刻意保留的命名/信封差异（不要"统一"掉）**：
1. 字段级 `invalid_activation`（空/格式）与业务级 `activation_invalid`（中台校验失败）**语义不同**，故拼写不同。同理 `invalid_box_code`（字段）与 `box_code_unknown` / `box_code_mismatch`（业务）。
2. `invalid_code` 同时出现在两个层级，靠**信封类型**区分（有 `fieldErrors` 就是字段级）。
3. `invalid_phone` 在 `login` 里是字段级、在 `send-code` 里是业务级（前端自己落回 `phone` 字段）。**同一个 code 名、两种信封**，实现者"顺手统一"会造成不报错但文案错的结果（§2.2）。

**前端本地兜底（不是服务端契约）**：`BlockedView` 的 `businessErrorKey()` 对未知 code 渲染 `product.submit_failed`（「登录失败，请重试」）并 `console.warn` 一次（每个 code 只报一次）。这是**收到未知 code 时的兜底文案**，不代表服务端可以返回未登记的 code——**未知 code 应当视为契约违约并记日志**，而不是静默落到通用文案。

> **2026-09-13 更正（`V-45`）：这段话此前描述的是一个不存在的兜底。** 实现里那个分支写的是 `default: return ''` —— 未知码渲染成**空串**，用户看到的是「没有错误」，比静默落到通用文案更糟。之所以长期没被察觉，是因为**唯一会走到那里的码（`activation_required`）没有任何测试**。现在两者都改了：兜底真的落到 `product.submit_failed`，且 `activation_required` 上了表（见上一行）。这条差异值得记住：**规范里写的行为不是行为，代码里的才是**。

**信封层级由本表决定，不由中台决定（2026-09-11，`PR-2b` 实施时明确）。** 中台会在它的错误体里带 `field`（例如它把 `activation_invalid` 标成 `activationCode` 的错），但**本地该走字段级还是业务级，是本契约的决定** —— 上表把 `activation_invalid` / `box_code_unknown` / `box_code_mismatch` / `phone_mismatch` 都定为**业务级**，所以实现必须**忽略中台那个 `field`**，把它们送到顶部横幅而不是输入框下面。理由：用户改不动这些值（`activation_code_used` 只能找客服），落在输入框下面会误导成"改一下就能过"。
- **实现落点：`internal/productruntime/envelope.go` 的 `fieldLevelCodes` 表**（只有 3 个 code 是字段级：`invalid_code` / `nickname_format` / `nickname_sensitive`）。**本表与那张表必须同步改** —— 这与 §2.10 前端 `normalizeWord` 的同步约束是同一类要求：规范在文档，执行在代码，两处一起动。
- **`invalid_value` 不在这 3 个里，且这是判据不是遗漏（`V-72`，2026-09-14 统一）**：`fieldLevelCodes` 的唯一调用方是 `writePlatformError`，只有**中台**的错误码能走到那里；而 `invalid_value` 是**本地**对「这个值不在可接受集合里」的判断，永远不会以中台错误的形式到达。所以它按 `{"field": …, "code": …}` 走，由 `writeValueRefusal` 一处写出 —— §2.5 与 §2.7 共用它。**下一次有人觉得"既然带字段名就该并进 `fieldLevelCodes`"时，代价是把同一个 code 变成两种形状**（这正是 `V-72` 被登记的原因：本表原先把 `invalid_value` 标成"字段"，而 §2.5 与 §2.7 给了两种信封）。
- **`invalid_word` 是同一个判据在词库面上的兄弟（`V-75`，2026-09-14）**：它**同样不进 `fieldLevelCodes`** —— 那张表映射的是「哪个输入框该变红」，而这个词是**列表里的一个值**（用户要改的是列表里的某一项，页面上没有对应输入框）。它与 `invalid_value` 的唯一区别是多带一个 `word` 而不是 `field`：**名字不同是因为被指的东西不同**（一个是输入框，一个是列表项），不是因为信封换了代。**登记它的过程本身就是这条缺口的意义**：四条词库/检测路由在契约里连「错误」行都没有，而归档实现里已经有一个码在跑 —— 与 `V-72` 同族、方向相反（`V-72` 是「一个码两种形状」，`V-75` 是「一个形状没有码」）。
- 其余 code 一律**业务级**（安全默认：宁可让用户在上方看到一条横幅，也不要让文案被静默丢进错误的输入框）。
- `invalid_phone` / `invalid_code` 在两层都出现，靠**上下文**区分，规则是：**格式问题在本地校验阶段就拦下**（`productruntime` 的 `validPhone`/`validCode`），所以**凡是从中台回来的同码，按业务级处理**。

**`phoneMasked` 的来源（2026-09-11 补）。** 它由**中台**在 `phone_mismatch` 的**错误体**里返回（`中台交付包.md` §3.2 已加该字段），不是本地状态里的号码。理由：触发这条的典型场景是「全新 `data/` + 已被绑定的激活码」，此时本地从没见过那个号码，只有中台知道它。

**`unauthorized` 为什么必须是独立一行（2026-09-12 补，`V-21`）。** 它是全表里唯一一个**形状不是中台错误信封**的 code：中台那次应答可能是 401，也可能是「某个已授权的调用」之后**刷新被拒**，客户端要到第二次往返才知道会话没了，所以 `productclient` 用**哨兵错误** `ErrSessionExpired` 表达它。这个形状差异曾造成一处真实缺陷：`writePlatformError` 先用 `errors.As` 取信封，取不到就归入「请求没到达中台」⇒ 会话失效被报成 `503 network_unavailable`，前端于是给了一个**永远不可能成功**的重试按钮。修法是在该函数**最前面**单独判 `errors.Is(err, productclient.ErrSessionExpired)`。**登记在表里就是为了让下一个"顺手统一信封"的人停一下**：这一行存在的理由是它的形状不同，不是漏改。**它的触发点在今天只有一处** —— `PR-4b` 的目录拉取（`client.Bootstrap` 是首个走 `doAuthorized` 的调用），在那之前这条路径在端到端上到不了。


---

## 4. 产品相关的 WebSocket 推送

同一份本地契约的另一半在 `/ws`（上游传输，本文件只登记产品新增的事件）。

| 事件 | 载荷 | 接收方 | 说明 |
| --- | --- | --- | --- |
| `credits_update` | `{ credits: {balance} }` | `App.svelte`（**接手半已在**） | ⬜ **无发射方（2026-09-14 核实）** —— `App.svelte:278` 的接收端在，而服务端**零命中**。它与 `PR-5d1` **不是一回事**：`E9` 规则 2 定下"余额只有拉取一个写入路径"之后，本事件的**触发时机**变成一个独立问题（推送 vs 拉取的关系），而已登记的 `N-5`（四个产品 WS 事件的触发时机与重放语义）**正是这个问题** ⇒ 归属 `PR-8`，本轮不实现、也不发明时机 |
| `input_sensitive` | `{ session_id, text }` | `ChatView.svelte` | 服务端在聊天链路拦下敏感输入，前端把文本还回输入框 + 提示。**✅ 已有发射方（`PR-6b3`，2026-09-15）**：`text` 是**已打码**的那份（不是用户敲的原文 —— 事件回到输入框，原文不该被还回去）；三个回合入口（`/ws` 的 `user_message`、`POST /api/chat`、`POST /api/chat/{id}/turn`）共用这一条判据，**广播发生在广播用户消息与落库之前**，所以被拦下的一句**不入库、不广播、不产生一次模型请求**（`需求 D1`）。REST 两个入口回 `400 {"code":"input_sensitive","text":…}`（§3），`/ws` 入口回本事件。**范围**：只覆盖用户手敲的正文 —— 重试、机器生成文本（任务续跑/工具输出）、IM 通道**不过门**（§3.10，理由见 `开发计划` §PR-6b3 第 4 步） |
| `datastore:lost` | 无 | `App.svelte` | 数据目录失联（U 盘拔出）→ 冻结遮罩，阻止所有输入 |
| `datastore:restored` | 无 | `App.svelte` | 同路径恢复 → 解冻 |
| `turn_error`（**上游事件，本 fork 加了一个字段**） | `{ session_id, error, code?, input_rolled_back? }` | `ChatView.svelte`、`mobile/chatWiring.ts` | **`code` 是 2026-09-14（`PR-5d3`）新增的字段**，取 `中台交付包` §3.2 登记的码值；**有码才带该字段**（空串与"没有码"必须分得开）。前端按码取 i18n 文案（`web/src/lib/turnError.ts` 是 `code → key` 的**唯一** owner），`error` 句退化为**未知码时的兜底**——`C8` 规则 1 禁止把中台的 `message` 当界面文案，而今天走到这里的 `402` 原文正是一串英文 JSON（`L-C4c`） |

> `⏳ 待补`：这四个事件的**触发时机、重放语义、`session_id` 缺失时的作用域规则**尚未定，登记为 `N-5`（见 §5）。

---

## 5. 未定项

**2026-09-12 更正：本表原先把 `S-1` / `S-3` / `S-5` / `S-6` / `S-7` / `N-2` 列为「未定」，那已经过期** —— 这六项在 [`需求基线.md`](需求基线.md) §5.1（标题即「**已全部落规格**」）与 §5.5 里**都已 ✅ 关闭**，规格各有落点。本表是「还有什么没定」的登记处，重开已关闭项会让读者去做已经做完的事，故删去。

| # | 未定 | 归属 |
| --- | --- | --- |
| `S-1` | 本文件的**契约测试**：`web/src/lib/product.contract.test.ts` + Go 侧 `contract_test.go`，断言「本文件登记的每个端点与错误码都已实现」 | 规格已定（§5.1 ✅ = 契约测试应当存在），**但测试本身尚未落地** —— 这是「已定规格、未实现」而非「未定」 |
| `N-5` | §4 四个 WS 事件的作用域与重放语义 | 本轮盘点新发现，**本轮唯一真正未定的一项**（`需求基线` §5.5 记 `⏳ 仍未定`；卡 `PR-8`） |

> 已关闭项的落点，查 [`需求基线.md`](需求基线.md)：`S-3` → `D4` 规则 4/5/6；`S-5` → `E6.2`；`S-6` → `A1` 规则 6；`S-7` → `C12`；`N-2` → `E6.3`。**不在本文件复述结论**（§3.8：派生文档只引一行结论 + 链接）。

---

## 6. 修订

| 版本 | 日期 | 变更 |
| --- | --- | --- |
| `v0.16` | 2026-09-15 | **`PR-6b3` 落地：§2.12 `sensitive/check` 已注册 ⇒ §1.4 的 15 条端点全部落地、`V-24` 清零；§3 增 `input_sensitive` 码值行；§4 的 `input_sensitive` 事件由「无发射方」改为「已有发射方」并写死**打码后**的载荷语义。** ① **本文件此前把两件事混在一个记号里**（见 §0.1 与 §1.4 的注）：§1.4 的 ✅ 既想说"形状定了"又想说"路由挂了"，于是四条词库/检测路由长期挂着 ✅ 而代码里没有（`V-24`）。本版起 §1.4 的状态列**只**表达存在性，§0.1 那句"§1.4 的 15 条里有 14 条已实现"随之作废（现在是 15 条全落地）。② **§2.12 只回答"这一句怎么打码"，与"拒"无关**（拒绝在三个回合入口）—— 这层分工写进了该节，因为把两者读成一件事会让实现者以为 `check` 返回 `hit: true` 就该拦，从而漏掉真正的判据（顺序：**命中 ⇒ 拒，且在广播与落库之前**）。③ **§4 的载荷语义是本次唯一的"新定"**：事件里的 `text` 是**已打码**文本，不是用户敲的原文 —— 它要回到输入框，而 `需求 D1` 的用意是"这句话不出去"，把原文还回去等于把要复现的话原样留着。④ **范围**：只覆盖用户手敲的正文；重试、机器生成文本、IM 通道不过门，理由记在 `开发计划` §PR-6b3 第 4 步（§3.10）。⑤ **本文件不新增端点、不新增集合**：§1.4 行数不变，第 12 行只是从 ⬜ 变 ✅。 |
| `v0.15` | 2026-09-14 | **`PR-6b2` 落地：§2.9 / §2.10 / §2.11 三条路由已注册（`V-24` 的三条清零），§1.4 的三行由 ⬜ 改 ✅；§2.12 `sensitive/check` 改归 `PR-6b3`。** ① **落点与 `v0.14` 预告的不同**：归档实现在 `internal/server`，落地在 `internal/productruntime`（§0.1 的落点纪律），且只有经 `MountAPI` 缝注册才继承产品门 —— 所以三条路由各自有**一条"不带 window token 必 403"的钉子**，这正是 `V-24` 那一类缺陷的判据形态。② **应答语义按本契约实现**：`PUT` 回的是**去重 + 归一化之后真正写进文件的那份**（可能与请求不同）、`import` 是**合并非覆盖**、`dryRun` **不落盘**；三条各有读**文件字节**的钉子。③ **归档里有两件刻意不带**：`datapath.Frozen()`（`PR-7` 的冻结点未落地 ⇒ 写失败按 `500` 报，见 §2.10 的「错误」行）与 `filterThinking`（`PR-6a` 的装饰器已覆盖思考面）。④ **本文件不新增端点、不新增错误码** ⇒ §1.4 行数不变，三条路由只是从"未挂"变为"已挂"。**范围（§3.10）**：本地 HTTP 契约；不影响中台契约与出包。 |
| `v0.14` | 2026-09-14 | **写 `PR-6b2` 方案时补齐四条词库/检测路由的「错误」面，并新增错误码 `invalid_word`（`V-75`）。** ① **§2.10 的两条备注**：应答体回的是「去重 + 归一化之后**真正写进文件的那份**」（可能与请求不同）、以及**写盘保留文件头注释块、词后注释不保留**（归档取舍，`P13` §4.4）—— 后者此前只存在于实现里，读者会当成 bug。② **§2.9 / §2.11 / §2.12 各补一行「错误」**，其中两条是**明写的「无业务错误」**：文件缺失＝用户层为空（不是错误、也不建文件）、导入里的空词/重复词/内置冲突词一律计入 `skipped`（`PUT` 对空词才是拒绝 —— 两条路由语义不同，这里写清才不会被"顺手统一"）。③ **§3 增 `invalid_word`**：业务级 ＋ 一个 `word` 兄弟字段，**刻意不进 `fieldLevelCodes`**（那张表映射输入框，而这个词是列表里的一个**值**）—— 理由与 `invalid_value` 同源，写在 §3 的表后说明里。④ **本文件不新增端点** ⇒ §1.4 的行数不变，四条路由的状态列仍是「未挂」（`PR-6b2` 落地时再改）。**范围（§3.10）**：本地 HTTP 契约；不影响中台契约与出包。 |
| `v0.13` | 2026-09-14 | **`PR-6b1` 落地：§2.6 `nickname` 与 §2.7 `prefs` 两条路由真挂上（`V-24` 的两条清零），并把 `invalid_value` 的信封从两种收成一种（`V-72`）。** ① **§2.5 的错误信封改写**：`{"fieldErrors": {"locale": "invalid_value"}}` → `{"field": "locale", "code": "invalid_value"}` —— 与 §2.7 一致。三条理由写在 §2.5：前端对这条路由**不解析应答体**、`fieldLevelCodes` 本来就不含 `invalid_value`（那张表转发的是**中台**的字段判断）、同一个值在两条路由上被拒该看到同一句话。实现上两条路由共用 `writeValueRefusal`。② **§3 的 `invalid_value` 行改写**：层级从"字段"改为"字段名 + 业务码"，并写明它**刻意不在** `fieldLevelCodes` 里 —— 那段说明是给下一个"顺手统一信封"的人看的（`V-72`）。③ **昵称格式规则以需求为 owner 收口**：`需求 20260906` 的 **2–16 字、中文/字母/数字/下划线**，Go 侧原为"非空且 ≤ 20 码点、不限字符"（`V-73`）—— 服务端比前端宽，于是 1 字、含空格/emoji、17–20 字**都曾被服务端接受**。④ **§2.6 的拒绝是业务级 `{"code": …}`**，虽然那两个码**同时在** `fieldLevelCodes` 里（那条路径服务登录）：复用 `writeFieldErrors` 会让前端 `body.code` 读到 `undefined` 并回落成「格式不正确」，即**命中敏感词被说成格式错误**（§2.2 的 `invalid_phone` 是同型）。⑤ 本文件**不新增端点、不新增错误码** ⇒ §1.4 无结构变化，只是两条既有编号从"未挂"变为"已挂"。 |
| `v0.12` | 2026-09-14 | **`turn_error` 加 `code` 字段（`PR-5d3`，`L-C4c` / `G3` / `V-55` / `V-58`）—— 本文件登记该事件，是因为它是「前端可见的失败载体」而此前只带 `error` 一句。** ① **§4 补 `turn_error` 一行**：字段 `{session_id, error, code?, input_rolled_back?}`，**有码才带该字段**（空串与"没有码"必须分得开）；**码值的 owner 是 `中台交付包` §3.2**，前端 `web/src/lib/turnError.ts` 是 `code → i18n key` 的**唯一** owner。② **为什么不是"把中文发下来"**：`C8` 规则 1 禁止客户端用中台的 `message` 当界面文案，而走到这里的 `402` 原文是**英文原始 JSON**（网关的扁平信封 `{"code":…,"message":…}` 与 OpenAI 嵌套形状不同，见 `中台交付包` §3.2）⇒ 码必须单独走一个字段。③ **§3 的 402 行同批更新**（`中台交付包` §3.2）：客户端**不得在本地预测**这个码（`PQ8`），本文件夹里 `Composer.svelte` 那行按余额判「积分不足」的提示已删（`V-58`）。④ 本文件**不新增端点、不新增本地错误码** ⇒ §1.4 / §3 无结构变化。 |
| `v0.11` | 2026-09-14 | **余额收敛：新增第 15 条端点 `GET /api/product/credits`（§2.15），并把 `credits` 对象从三个字段收成一个 —— 依据是人工 2026-09-14 拍板的新规则「余额必须从服务器刷新、服务器扣减、客户端只老实刷新余额」（`需求基线` `E9` 规则 2 已改写）。** ① **`credits: {balance, monthUsed, monthKey}` → `{balance}`**：中台账本只有 `balanceMicroCredits`，而 `E9` 规则 5 明令客户端不做跨月加总 ⇒ 后两个字段**既无来源又不许本地算**（§3.8，留着一个永远显示 0 的"事实"）。② **新端点与 §2.6 / §2.7 同形**（回 `{state}`）—— 不新增形状：`credits` 对象已在 §1.3 登记。③ **本条把"余额从哪来"从两个来源两套判据收成一条路径**（`V-11` 那条已被取代的"两条并列合法来源"），于是 `D-002`（SSE 终态帧）**不再是余额链路的阻塞项** —— 终态帧只是"该去拉一次"的触发器。④ **§4 的 `credits_update` 状态改为 ⬜ 无发射方**，触发时机归 `PR-8` / `N-5`（本轮不发明）。⑤ 本文件是 `credits` 对象字段表的 owner；判定理由在 `E9` 规则 2/7，不在此处复述。 |
| `v0.10` | 2026-09-13 | **`PR-4c` 落地，新增第 14 条端点 `GET /api/product/catalog`（§2.14）**：四值 `state`（`ready` / `absent` / `stale` / `unverifiable`）+ `retryable` + 缓存元数据（`catalogVersion` / `expiresAt`）。判定唯一 owner 在 `internal/productruntime` 的 `assessCatalog`，三输入＝缓存有无 / 是否过期 / 最近一次刷新的结局；优先级是**验签失败压过可用缓存**（不再"列表说一套、横幅说另一套"），其次无缓存，再次过期，最后 `ready`。前端只在 `chatMode.ts` 的 `catalogNoticeKey()` 一处做状态→文案映射。**四条 `B9` 文案新增三个 i18n 键**（`catalog.absent` / `catalog.stale` / `catalog.unverifiable`），第四条（分组空）沿用既有的 `mode.no_models`。**§1.4 增第 14 行。** 顺带把「能不能开新回合」（`canStartTurn()`，`ChatView.send()` 的第一道闸）的判据写清：只认 `state === "ready"`；**服务端侧强制归 `PR-5`**，读同一个判据、不新建第二个。**目录刷新改条件请求**：客户端带已有的 `catalogVersion` 作 `knownVersion`，`304` 与 `{"unchanged":true}` 两种"没变"都认、都不重写缓存；**已过 TTL 时中台不得回"没变"**（`中台交付包` §4.3 已写明，否则客户端会被永久卡在"需要联网更新"）。**周期性刷新仍为 `TODO`**（当前只有登录后强制 + 开选择器按需两条触发路径），已登记在 `开发计划`。 |
| `v0.9` | 2026-09-13 | **`PR-3`：前端假后端（`web/src/dev/devBackend.ts`）已拆除，§0.1 的「临时假后端」可信度来源随之作废。** ① §0.1 该行改为「已拆除，可信度＝无」，并写明**凡此前只标「形状取自假后端」的行，形状自本版起以 `internal/productruntime` 的实现为准**；② §2.4 `logout` 与 §2.5 `locale` 的 `{"ok": true}` 已**对 Go 实现逐字核对**（`runtime.go:411` / `:431`）并标注行号 —— 这两处此前是靠假后端「自称」的，属本文件里可信度最低的一类；③ 记下该替身为何危险（对**未注册路径回 `{}` + `200`**，`V-24` / `V-46` 因此长期不可见），性质属 §3.9 静默降级。 |
| `v0.8` | 2026-09-13 | **新增 §1.5「会话级路由」并登记 `PATCH /api/sessions/{id}/chat_mode`（`PR-4d1`，修 `V-46`）。** 起因是用户手工验收 `PR-4d` 时问"切换模型提示 404 Not Found"：前端 `setSessionMode` **先**发的 `PUT /api/sessions/{id}/chat-mode` 是**从未注册**的路由（只被 `devBackend.ts` 的 DEV 假后端接住），真机下第一步就 404，且**把后面的模型请求与两处本地 store 一起吃掉**。新节写清四件事：① 路径取**下划线**（与五个兄弟一致）且改为 `PATCH`；② **不过产品门**（不经 `MountAPI` 缝注册，只走 `requireAuth`）；③ 校验的 owner 是 `internal/chatmode`；④ 请求/应答/错误/副作用与"字段名四处逐字一致"的约束。**为什么不并进 §1.4 / §2**：那两处的编号是产品端点的编号，已被 `开发计划` §1.1 与闭环判据引用，插入会连带改号。 |
| `v0.7` | 2026-09-13 | **`PR-2e` 落地 `V-45`（`v0.6` 那条规则的更正与扩展）。** ① **§2.3 请求形状**：两个激活凭证从"首启必填/二次登录省略"改为**可选的一对**（要么都给、要么都不给），本地只查形状、是否首启由中台判（`V-45` / `PQ28`）—— 这条同时把"丢 `data/` 只能走客服"变成"只凭手机号+验证码登进已有账号"；② **业务级码表补 `activation_required` 一行**（`403`、不挂 `field`、动作＝**去激活**）：它按 `中台交付包` §4.2 是登录必备码，但客户端此前**没有 case、没有 i18n 键**，兜底又返回空串 ⇒ 平台真发它时横幅是**空白**；③ **§2.3 规则**改写 `V-44` 那条的用户可见半边：降 `activated` 仍在（数据），但前端**不再自动换表单**，改为"消息 + 「去激活」按钮"；④ 撤回国判据补第五个码 `activation_required`；⑤ 顺带把"业务级一律 400"改成"状态码取自 `中台交付包` §3.2"（`activation_required` 是 403，原文那句话会误导实现）|
| `v0.6` | 2026-09-13 | **`PR-2d` 落地 `V-44`：§2.3 补一条规则 —— 一次「没带激活凭证」的登录被平台以四个激活类码拒绝时，本地 `activated` 随之降为 `false`，应答码/信封/状态码不变。** 起因是用户手工验收 `PR-4c1` 时提问「退出登录后登录提示激活码不正确」：本地 `activated:true` ⇒ 表单只发两个字段 ⇒ 平台按激活失败答复（`activation_invalid`）⇒ 文案落在**没有该字段的表单**上（`BlockedView.svelte:34/144-145/276/198`），界面内的唯一出路不存在。**规则的两半都写进 §2.3**：降的两个条件（未带凭证 **且** 平台码属那四个）与**不降的清单**（传输失败/5xx/`invalid_code`/`code_not_sent`/`phone_mismatch`）；并写明判定在服务端、前端只重读状态（与 `V-22` 同源）。**§1.4 / §3 无新增**：不新端点、不新错误码。 |
| `v0.5` | 2026-09-12 | **新增第 13 条端点 `GET /api/product/control-plane`（`PR-2c`，服务 `L-B2`）—— 本轮改代码的同时改本文档。** ① §1.4 端点总表加第 13 行；② §2.13 写全：`{"configured","hasTrustedKeys"}`、判定位置仍是 `internal/productprofile`（唯一 owner，`productruntime` 只经 `Deps.ControlPlane` 转发）、**需登录＝否**、为什么不塞进 `ProductStateDTO`、四条前端消费规则（**`configured` 先判**，落地时经测试纠正）、文案约束、i18n 键名；③ §0.1 的「5 条已落地」改为「6 条」，并把 `README.md` / `需求基线.md` 的「12 个端点」改为 13。**注意**：本端点没有错误码、不写盘、不吃中台 —— 它是编译期事实的纯转发，因此 §3 错误码总表**无需**新增行。 |
| `v0.4` | 2026-09-12 | **`PR-2c` 落地 `L-A6`：补登 `unauthorized` 一行，并解释它为什么是全表唯一的异形码。** `V-21`：`ErrSessionExpired` 是**哨兵错误**、不含 `*productclient.Error`，`writePlatformError` 的 `errors.As` 落空后把它归成 `503 network_unavailable` —— 会话失效被报成「网络不通」，前端因此给一个**永远不可能成功**的重试按钮（被拒的 refresh token 不自愈）。本表此前**根本没有这一行**，正是该误分类无人发现的原因：契约没登记，就没有人核对。现已补登（业务级、**刻意不配 i18n 键**、本地应答 **401**，由前端 `noteSessionLost` 直接回拦截页），并注明**它的触发点在今天只有一处**（`PR-4b` 的目录拉取，`client.Bootstrap` 是首个走 `doAuthorized` 的调用），在此之前端到端到不了。§3 另加一段说明其形状差异**是有意的、不要在"统一信封"时抹平**。 |
| `v0.3` | 2026-09-12 | **全库核对发现本文件自身 4 处已过期（本轮只改文档，不动代码）。** ① **§5「未定项」重开了 6 个已关闭项** —— `S-1` / `S-3` / `S-5` / `S-6` / `S-7` / `N-2` 在 `需求基线` §5.1（标题即「已全部落规格」）与 §5.5 里**都已 ✅ 关闭**，本表却仍列为「未定」，会让读者去做已完成的事（§3.8：派生文档只引结论 + 链接）；已删去并改为一行落点索引，**只留 `N-5`**（本轮唯一真正未定项）。② **§0.1 说「`v1` 上还没有真后端」** —— `internal/productruntime` 已随 `PR-2b1` / `PR-2b2a` 落地并接进服务，12 条端点里**有 5 条真实存在**；已改为混合可信度，并把「已落地的 Go handler」列为**最高档**依据。③ **§2.3 说 `boxCode` 「后端尚无」** 并引用分支 `feat/activation-box-code` —— 后端已有（含 `TestFirstActivationReturnsWrappedStateWithServerBoxCode`），该分支已不存在且**无归档 tag**（内容已在 `v1`）；已改。④ **§1.4 给了一条已被推翻的实现指引**（「产品门按前缀白名单放行四条」）—— 已落地的门是**注册器的属性**、校验**窗口身份而非登录态**，没有白名单这回事（`开发计划` §4.1 第 12 行 / `D-006`）；已改为写明该层判据**本轮未实现**。⑤ 顺带收紧「标 `✅` ≠ 后端已验证」：只有那 5 条已实现端点算已验证。 |
| `v0.2` | 2026-09-11 | 自查修订（发现 1 处**事实错误** + 4 处**未标来源**）:① **`send-code` 的手机号错误走业务级 `{"code":"invalid_phone"}`，不是 `fieldErrors.phone`** —— 前端 `product.ts:177` 读的是 `body.code` 再自己落到 `phone` 字段；原 `v0.1` 写错了信封，照它实现会导致「不报错但文案错」。§2.2 改正，§3 增列第 3 处「同名不同信封」差异。② 新增 §0.1「依据与可信度」，说明 `v1` 上**没有真后端**、本文件是**从正面前端整理**而非反向后端提取，并给出三档可信度。③ §2.4 `logout` 与 §2.5 `locale` 的应答形状标注「取自假后端 / 本契约新定」，不再伪装成已验证。④ §2.8 标明 `catalogVersion` / `policyVersion` 是本契约新拟名，若中台已有版本字段则以中台为准。⑤ §1.4 补「需登录列是设计约定」，并写明应由产品门按**白名单**放行未登录可达的四条路由。⑥ §3 补「未知 code 应视为契约违约并记日志」，不得静默落到 `product.submit_failed`。 |
| `v0.1` | 2026-09-11 | 首版：登记 12 个本地端点、三类错误信封、错误码总表、产品相关 WS 事件；标出 `login` 加 `boxCode` 与 `chat-modes` 去 `fallback` 两处 🚧 |
| `v0.2.1` | 2026-09-12 | **§2.8 由 🚧 转 ✅（`PR-4d` 落地）**：去 `fallback`、加 `displayName` / `catalogVersion` / `policyVersion` 六条全部实现。**第 4 条按实际来源定稿** —— 两个版本字段都不是新拟名（`policyVersion` 来自中台信封、`catalogVersion` 来自 `catalog.version`）。**降级一条补"现状"**：`PR-4d` 保证的是 fail-closed 不回落这一半（`200` + 空列表 + 无 `fallback` 字段），**文案归 `PR-4c`**。（**版本号 `v0.2.1` 是 2026-09-14 补的**：本行此前与 `v0.1` 之后的 `v0.2` **复用同一个版本号**，表格里出现两行 `v0.2`、且顺序在 `v0.1` 之后 ⇒ 读者无法判断哪一行更晚，见 `需求基线` `V-69`。内容一字未改，只把这一行的标签与其"对 `v0.2` 的后续更正"性质对齐。） |
