# 本地 API 契约（`/api/product/*`）

> **状态**：`v0.5`（2026-09-12 `PR-2c`：`L-A6` + 新增第 13 条端点 `GET /api/product/control-plane`）
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

`v1` 上**后端的落地是部分的**（2026-09-12 更正，原文说"还没有真后端"已过期）：[`internal/productruntime`](../../../internal/productruntime) 已随 `PR-2b1` / `PR-2b2a` 落地并接进服务，**§1.4 的 13 条端点里有 6 条**（`state` / `send-code` / `login` / `logout` / `locale` / `control-plane`）**是真实实现、可反向核对**；其余 7 条仍是正向整理。所以本文件的可信度是**混合**的：

| 依据 | 位置 | 可信度 |
| --- | --- | --- |
| **已落地的 Go handler**（**最高**） | `internal/productruntime/`（`state` / `send-code` / `login` / `logout` / `locale` / `control-plane` 六条）+ `internal/productclient/dto.go` | **高** —— 已实现且经测试，与它不一致的是本文件而不是代码 |
| 前端调用与解析逻辑 | `web/src/lib/product.ts`、`api.ts`、`sensitive.ts`、`sensitiveDict.ts` | **高** —— 前端必须这样解析才能工作，字段名与信封形状是硬事实 |
| 临时假后端 | [`../2260906/技术方案/开发期假后端说明.md`](../2260906/技术方案/开发期假后端说明.md) + `web/src/dev/devBackend.ts` | **中** —— 它是替身，形状可被真后端改动 |
| 本文档的设计决定 | 本文件新定 | **待实现** —— 一律标 🚧 |

因此每行的状态要这样读：`✅ 现状` = **前端已依赖此形状**（改它要动前端，不是纯后端改动）；`🚧 修订` = 本契约新定，代码尚未实现。

**标 `✅` 不等于「后端已验证」**（2026-09-12 收紧）。上一句只保证"前端依赖它"。要算**后端已验证**，还须该行落在上表第一行的五条已实现端点里 —— 即 `state` / `send-code` / `login` / `logout` / `locale`。**其余 7 条即使标 `✅` 也没有后端**，它们的权威校验（去重、归一化、限流边界）仍未验证。

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
| `credits` | object | `{balance, monthUsed, monthKey}` |
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
| 6 | PUT | `/api/product/nickname` | 是 | `{state}` | ✅ |
| 7 | PUT | `/api/product/prefs` | 是 | `{state}` | ✅ |
| 8 | GET | `/api/product/chat-modes` | 否 | 否 | ✅ 新形状已实现（`PR-4d`，2026-09-12） |
| 9 | GET | `/api/product/sensitive/dict` | 否 | 否 | ✅ |
| 10 | PUT | `/api/product/sensitive/dict` | 是 | 否 | ✅ |
| 11 | POST | `/api/product/sensitive/dict/import` | 是 | 否 | ✅ |
| 12 | POST | `/api/product/sensitive/check` | 否 | 否 | ✅ |
| 13 | GET | `/api/product/control-plane` | 否 | 否 | ✅ 新增（`PR-2c`，见 §2.13） |

> **「需登录」列是设计约定，不是观测事实。** 前端对 `/api/product/*` 一律带 window token（`api.ts` 的 `withWindowToken`），**产品门是否拦某条路由由服务端决定**，前端不区分。本列表达的是**应有的门策略**：凡读写账号数据或改词库的都要登录；`state` / `locale` / `send-code` / `login` 必须在未登录时可达，否则登录页根本渲染不出来。
>
> **但这层「需登录」判据本轮尚未实现**（2026-09-12 更正，原文给了一条已被推翻的实现指引）：已落地的 `internal/server` 产品门是**注册器的属性**，对经 `MountAPI` 缝注册的**全部**产品路由**一视同仁** —— 它校验的是窗口身份（`X-Octo-Window-Token`），**不是登录态**，因此没有、也不应有"按前缀白名单放行四条"这回事（那条路线曾定为"宽门"，落地前被实测推翻，理由见 [`开发计划.md`](开发计划.md) §4.1 第 12 行与 [`待解决问题.md`](待解决问题.md) `D-006`）。**按激活态区分可达性需要服务端读 `internal/productstate`**，那是 `D-006` 记的未做项，不是门的事。

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
  - 业务级 `400 {"code": "<code>"}`：
    | code | 含义 | 用户要做的事 |
    | --- | --- | --- |
    | `code_not_sent` | 还没获取验证码 | 点「获取验证码」 |
    | `invalid_code` | 验证码错误或过期 | 重填 |
    | `activation_invalid` | 激活码校验失败（中台侧） | 核对激活码 |
    | `activation_code_used` | **该激活码已被使用过**（一码一用） | **联系客服**——这是唯一出路 |
    | `box_code_unknown` | 盒子编号不认识 | 核对编号 |
    | `box_code_mismatch` | 激活码与盒子编号不匹配 | **联系客服** |
    | `phone_mismatch` | 本目录已绑定另一手机号 | 随响应带 `phoneMasked` |
- **落点**：`internal/productruntime` → 中台 `POST /v1/auth/login`。
- **规则**（`E1`）：
  - `activationCode` 与 `boxCode` 是**两个独立凭证**：前者答「这份授权买过没」，后者答「属于哪台盒子」。服务端**分别校验**，因此错误码也必须分开——用户错一个字符要能看出错在哪一个。
  - **一个盒子编号可对应多个激活码**（一对多），所以**不存在** `box_code_already_bound`；实现时不要发明这个码。
  - **一个 `U盘激活码` 只能使用一次**。用掉即废，换机/重装一概不能复用，唯一补救是走客服。
  - **一次「没带激活凭证」的登录若被平台以上面四个激活类码拒绝，本地 `activated` 随之降为 `false`（`GET /api/product/state` 可见），应答码、信封与状态码不变（`V-44` / `PR-2d`）。** 判据两条**同时**成立才降：① 该次请求的 `activationCode` 与 `boxCode` 去空白后都为空；② 平台码属于 `activation_invalid` / `activation_code_used` / `box_code_unknown` / `box_code_mismatch`。**理由**：`activated` 的 owner 是平台（`E1` 规则 6），本地那份只是它上次的答复；而拦截页只有在 `activated:false` 时才渲染那两口凭证输入框（`BlockedView.svelte:34/276`），不跟随就会让用户在**没有该字段的表单**上读到「激活码不正确」。**反面同样成立**：传输失败、5xx、`invalid_code`、`code_not_sent`、`phone_mismatch` 一律**不降**（本地故障不是平台的答复）；`account` 与 `activation` 记录**保留**（`E7`）。判定在**服务端**（`internal/productruntime`），前端不在失败后自行推断形状，而是重读状态（与 `V-22` 同一原则）。
- **✅ 与本分支现状的关系**（2026-09-12 更正，原文写「后端尚无」且引用了分支 `feat/activation-box-code`，两者都已过期）：前端与**后端**都已有 `boxCode` 全链路 —— 后端见 `internal/productruntime` 的登录处理与 `TestFirstActivationReturnsWrappedStateWithServerBoxCode`（`boxCode` **取自服务端应答，不是表单值**）。原引用的分支 `feat/activation-box-code` **已不存在且无归档 tag**（内容已进 `v1`，见 `web/src/views/BlockedView.svelte` 的 `boxCode` 字段与 Go 侧同名 DTO），故不再作为落点引用。

### 2.4 `POST /api/product/logout` ✅

- **请求**：空体。
- **应答 `200`**：`{"ok": true}` —— ⚠️ 形状**取自假后端**（`devBackend.ts`），前端目前忽略应答体，因此真后端可改；改则须同步本行。
- **副作用**：清 `data/credential.json`，`productPhase → blocked`。
- **备注**：**绑定手机号与昵称仍留在 `data/`**，供二次登录表单预填与比对（`E2`）。注销 ≠ 清数据。

### 2.5 `PUT /api/product/locale` ✅

登录**之前**也要能保存界面语言。

- **请求**：`{"locale": "zh" | "en"}`
- **应答 `200`**：`{"ok": true}`（同上，形状取自假后端）
- **错误**：`400 {"fieldErrors": {"locale": "invalid_value"}}` —— 🚧 **本契约新定**。前端目前对非 2xx 只抛通用 `Error`、不解析应答体（`product.ts:220`），所以这个形状**尚无任何代码依赖**，现在定下来成本最低。
- **备注**：默认语言**跟随系统语言**，非中文落 `en`（`PQ18`）。首启时由桌面壳把系统语言带进来，不靠前端猜。

### 2.6 `PUT /api/product/nickname` ✅

- **请求**：`{"nickname": "<新昵称>"}`
- **应答 `200`**：`{"state": ProductStateDTO}`
- **错误**：`400 {"code": "nickname_format" | "nickname_sensitive"}`
- **备注**：应答里的 `state` **就是持久化后的真相**（服务端回读自己的存储），前端直接覆盖共享 store，不再本地乐观更新。

### 2.7 `PUT /api/product/prefs` ✅

- **请求**：`{"locale"?: "zh"|"en", "defaultChatMode"?: "<mode id>", "inputSensitiveCheck"?: true|false}`
  未变的字段可省略，默认「保持不变」。
- **应答 `200`**：`{"state": ProductStateDTO}`
- **错误**：`400 {"field": "<字段名>", "code": "invalid_value"}`
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

### 2.9 `GET /api/product/sensitive/dict` ✅

- **请求**：无体。
- **应答 `200`**：`{"builtin": ["…"], "user": ["…"]}`
- **落点**：`internal/sensitive`（词库唯一 owner）。
- **备注**：`builtin` 是**随包内置词（`go:embed`）**，只读；`user` 是 `data/` 下的用户词文件。

### 2.10 `PUT /api/product/sensitive/dict` ✅

整份覆盖用户词，且是「**改动即写盘**」。

- **请求**：`{"user": ["词1", "词2"]}`
- **应答 `200`**：`{"user": ["词1", "词2"]}`
- **备注**：服务端**权威去重 + 空白/符号归一化**；前端的 `normalizeWord`（`sensitiveDict.ts`）只用于即时提示，不构成契约。两边的归一化规则必须保持同步（前端注释已注明这一约束）。

### 2.11 `POST /api/product/sensitive/dict/import` ✅

- **请求**：`{"words": ["…"], "dryRun": true|false}`
- **应答 `200`**：`{"added": <int>, "skipped": <int>}`
- **备注**：`dryRun: true` 是**导入预览**（只统计不落盘）；`false` 才写盘。

### 2.12 `POST /api/product/sensitive/check` ✅

输入框的即时检测（`P8`）。

- **请求**：`{"text": "<待检文本>"}`
- **应答 `200`**：`{"hit": true|false, "masked": "<命中词被替换为 * 的文本>"}`
- **备注**：这是**给输入框做替换 + 提示**用的，不是权威。**服务端在聊天链路上必须再检一次**（前端可被绕过）——见 `P8-敏感词接入.md`。

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
| `invalid_value` | 字段 | `locale` / `prefs.*` | — |
| `code_not_sent` | 业务 | 未获取验证码 | `product.err_code_not_sent` |
| `invalid_code` | 业务 | 验证码错误/过期 | `product.err_invalid_code` |
| `activation_invalid` | 业务 | 中台拒激活码 | `product.err_activation_invalid` |
| `activation_code_used` | 业务 | 激活码已用（一码一用） | `product.err_activation_used` |
| `box_code_unknown` | 业务 | 盒子编号不认识 | `product.err_box_code_unknown` |
| `box_code_mismatch` | 业务 | 激活码与盒子编号不匹配 | `product.err_box_code_mismatch` |
| `phone_mismatch` | 业务 | 目录已绑其他手机号（带 `phoneMasked`） | `product.err_phone_mismatch` |
| `control_plane_unconfigured` | 业务 | 本构建没有配置中台地址（`developer` 构建未填 Sandbox / 正式构建漏配） | —（S-6/S-7 未定，暂由前端兜底文案） |
| `unauthorized` | 业务 | **会话失效**：中台**明确拒绝**了 refresh token（`productclient.ErrSessionExpired`）。本地应答 **401** | —（**刻意不渲染文案**：前端 `noteSessionLost` 收到 401 即回拦截页，见 `P4-拦截页.md` §4） |

**三处刻意保留的命名/信封差异（不要"统一"掉）**：
1. 字段级 `invalid_activation`（空/格式）与业务级 `activation_invalid`（中台校验失败）**语义不同**，故拼写不同。同理 `invalid_box_code`（字段）与 `box_code_unknown` / `box_code_mismatch`（业务）。
2. `invalid_code` 同时出现在两个层级，靠**信封类型**区分（有 `fieldErrors` 就是字段级）。
3. `invalid_phone` 在 `login` 里是字段级、在 `send-code` 里是业务级（前端自己落回 `phone` 字段）。**同一个 code 名、两种信封**，实现者"顺手统一"会造成不报错但文案错的结果（§2.2）。

**前端本地兜底（不是服务端契约）**：`BlockedView` 的 `businessErrorKey()` 有一个 `default` 分支渲染 `product.submit_failed`（「登录失败，请重试」）。这是**收到未知 code 时的兜底文案**，不代表服务端可以返回未登记的 code——**未知 code 应当视为契约违约并记日志**，而不是静默落到通用文案。

**信封层级由本表决定，不由中台决定（2026-09-11，`PR-2b` 实施时明确）。** 中台会在它的错误体里带 `field`（例如它把 `activation_invalid` 标成 `activationCode` 的错），但**本地该走字段级还是业务级，是本契约的决定** —— 上表把 `activation_invalid` / `box_code_unknown` / `box_code_mismatch` / `phone_mismatch` 都定为**业务级**，所以实现必须**忽略中台那个 `field`**，把它们送到顶部横幅而不是输入框下面。理由：用户改不动这些值（`activation_code_used` 只能找客服），落在输入框下面会误导成"改一下就能过"。
- **实现落点：`internal/productruntime/envelope.go` 的 `fieldLevelCodes` 表**（只有 3 个 code 是字段级：`invalid_code` / `nickname_format` / `nickname_sensitive`）。**本表与那张表必须同步改** —— 这与 §2.10 前端 `normalizeWord` 的同步约束是同一类要求：规范在文档，执行在代码，两处一起动。
- 其余 code 一律**业务级**（安全默认：宁可让用户在上方看到一条横幅，也不要让文案被静默丢进错误的输入框）。
- `invalid_phone` / `invalid_code` 在两层都出现，靠**上下文**区分，规则是：**格式问题在本地校验阶段就拦下**（`productruntime` 的 `validPhone`/`validCode`），所以**凡是从中台回来的同码，按业务级处理**。

**`phoneMasked` 的来源（2026-09-11 补）。** 它由**中台**在 `phone_mismatch` 的**错误体**里返回（`中台交付包.md` §3.2 已加该字段），不是本地状态里的号码。理由：触发这条的典型场景是「全新 `data/` + 已被绑定的激活码」，此时本地从没见过那个号码，只有中台知道它。

**`unauthorized` 为什么必须是独立一行（2026-09-12 补，`V-21`）。** 它是全表里唯一一个**形状不是中台错误信封**的 code：中台那次应答可能是 401，也可能是「某个已授权的调用」之后**刷新被拒**，客户端要到第二次往返才知道会话没了，所以 `productclient` 用**哨兵错误** `ErrSessionExpired` 表达它。这个形状差异曾造成一处真实缺陷：`writePlatformError` 先用 `errors.As` 取信封，取不到就归入「请求没到达中台」⇒ 会话失效被报成 `503 network_unavailable`，前端于是给了一个**永远不可能成功**的重试按钮。修法是在该函数**最前面**单独判 `errors.Is(err, productclient.ErrSessionExpired)`。**登记在表里就是为了让下一个"顺手统一信封"的人停一下**：这一行存在的理由是它的形状不同，不是漏改。**它的触发点在今天只有一处** —— `PR-4b` 的目录拉取（`client.Bootstrap` 是首个走 `doAuthorized` 的调用），在那之前这条路径在端到端上到不了。


---

## 4. 产品相关的 WebSocket 推送

同一份本地契约的另一半在 `/ws`（上游传输，本文件只登记产品新增的事件）。

| 事件 | 载荷 | 接收方 | 说明 |
| --- | --- | --- | --- |
| `credits_update` | `{ credits: {balance, monthUsed, monthKey} }` | `App.svelte` | 就地替换 `productState.credits` |
| `input_sensitive` | `{ session_id, text }` | `ChatView.svelte` | 服务端在聊天链路拦下敏感输入，前端把文本还回输入框 + 提示 |
| `datastore:lost` | 无 | `App.svelte` | 数据目录失联（U 盘拔出）→ 冻结遮罩，阻止所有输入 |
| `datastore:restored` | 无 | `App.svelte` | 同路径恢复 → 解冻 |

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
| `v0.6` | 2026-09-13 | **`PR-2d` 落地 `V-44`：§2.3 补一条规则 —— 一次「没带激活凭证」的登录被平台以四个激活类码拒绝时，本地 `activated` 随之降为 `false`，应答码/信封/状态码不变。** 起因是用户手工验收 `PR-4c1` 时提问「退出登录后登录提示激活码不正确」：本地 `activated:true` ⇒ 表单只发两个字段 ⇒ 平台按激活失败答复（`activation_invalid`）⇒ 文案落在**没有该字段的表单**上（`BlockedView.svelte:34/144-145/276/198`），界面内的唯一出路不存在。**规则的两半都写进 §2.3**：降的两个条件（未带凭证 **且** 平台码属那四个）与**不降的清单**（传输失败/5xx/`invalid_code`/`code_not_sent`/`phone_mismatch`）；并写明判定在服务端、前端只重读状态（与 `V-22` 同源）。**§1.4 / §3 无新增**：不新端点、不新错误码。 |
| `v0.5` | 2026-09-12 | **新增第 13 条端点 `GET /api/product/control-plane`（`PR-2c`，服务 `L-B2`）—— 本轮改代码的同时改本文档。** ① §1.4 端点总表加第 13 行；② §2.13 写全：`{"configured","hasTrustedKeys"}`、判定位置仍是 `internal/productprofile`（唯一 owner，`productruntime` 只经 `Deps.ControlPlane` 转发）、**需登录＝否**、为什么不塞进 `ProductStateDTO`、四条前端消费规则（**`configured` 先判**，落地时经测试纠正）、文案约束、i18n 键名；③ §0.1 的「5 条已落地」改为「6 条」，并把 `README.md` / `需求基线.md` 的「12 个端点」改为 13。**注意**：本端点没有错误码、不写盘、不吃中台 —— 它是编译期事实的纯转发，因此 §3 错误码总表**无需**新增行。 |
| `v0.4` | 2026-09-12 | **`PR-2c` 落地 `L-A6`：补登 `unauthorized` 一行，并解释它为什么是全表唯一的异形码。** `V-21`：`ErrSessionExpired` 是**哨兵错误**、不含 `*productclient.Error`，`writePlatformError` 的 `errors.As` 落空后把它归成 `503 network_unavailable` —— 会话失效被报成「网络不通」，前端因此给一个**永远不可能成功**的重试按钮（被拒的 refresh token 不自愈）。本表此前**根本没有这一行**，正是该误分类无人发现的原因：契约没登记，就没有人核对。现已补登（业务级、**刻意不配 i18n 键**、本地应答 **401**，由前端 `noteSessionLost` 直接回拦截页），并注明**它的触发点在今天只有一处**（`PR-4b` 的目录拉取，`client.Bootstrap` 是首个走 `doAuthorized` 的调用），在此之前端到端到不了。§3 另加一段说明其形状差异**是有意的、不要在"统一信封"时抹平**。 |
| `v0.3` | 2026-09-12 | **全库核对发现本文件自身 4 处已过期（本轮只改文档，不动代码）。** ① **§5「未定项」重开了 6 个已关闭项** —— `S-1` / `S-3` / `S-5` / `S-6` / `S-7` / `N-2` 在 `需求基线` §5.1（标题即「已全部落规格」）与 §5.5 里**都已 ✅ 关闭**，本表却仍列为「未定」，会让读者去做已完成的事（§3.8：派生文档只引结论 + 链接）；已删去并改为一行落点索引，**只留 `N-5`**（本轮唯一真正未定项）。② **§0.1 说「`v1` 上还没有真后端」** —— `internal/productruntime` 已随 `PR-2b1` / `PR-2b2a` 落地并接进服务，12 条端点里**有 5 条真实存在**；已改为混合可信度，并把「已落地的 Go handler」列为**最高档**依据。③ **§2.3 说 `boxCode` 「后端尚无」** 并引用分支 `feat/activation-box-code` —— 后端已有（含 `TestFirstActivationReturnsWrappedStateWithServerBoxCode`），该分支已不存在且**无归档 tag**（内容已在 `v1`）；已改。④ **§1.4 给了一条已被推翻的实现指引**（「产品门按前缀白名单放行四条」）—— 已落地的门是**注册器的属性**、校验**窗口身份而非登录态**，没有白名单这回事（`开发计划` §4.1 第 12 行 / `D-006`）；已改为写明该层判据**本轮未实现**。⑤ 顺带收紧「标 `✅` ≠ 后端已验证」：只有那 5 条已实现端点算已验证。 |
| `v0.2` | 2026-09-11 | 自查修订（发现 1 处**事实错误** + 4 处**未标来源**）:① **`send-code` 的手机号错误走业务级 `{"code":"invalid_phone"}`，不是 `fieldErrors.phone`** —— 前端 `product.ts:177` 读的是 `body.code` 再自己落到 `phone` 字段；原 `v0.1` 写错了信封，照它实现会导致「不报错但文案错」。§2.2 改正，§3 增列第 3 处「同名不同信封」差异。② 新增 §0.1「依据与可信度」，说明 `v1` 上**没有真后端**、本文件是**从正面前端整理**而非反向后端提取，并给出三档可信度。③ §2.4 `logout` 与 §2.5 `locale` 的应答形状标注「取自假后端 / 本契约新定」，不再伪装成已验证。④ §2.8 标明 `catalogVersion` / `policyVersion` 是本契约新拟名，若中台已有版本字段则以中台为准。⑤ §1.4 补「需登录列是设计约定」，并写明应由产品门按**白名单**放行未登录可达的四条路由。⑥ §3 补「未知 code 应视为契约违约并记日志」，不得静默落到 `product.submit_failed`。 |
| `v0.1` | 2026-09-11 | 首版：登记 12 个本地端点、三类错误信封、错误码总表、产品相关 WS 事件；标出 `login` 加 `boxCode` 与 `chat-modes` 去 `fallback` 两处 🚧 |
| `v0.2` | 2026-09-12 | **§2.8 由 🚧 转 ✅（`PR-4d` 落地）**：去 `fallback`、加 `displayName` / `catalogVersion` / `policyVersion` 六条全部实现。**第 4 条按实际来源定稿** —— 两个版本字段都不是新拟名（`policyVersion` 来自中台信封、`catalogVersion` 来自 `catalog.version`）。**降级一条补"现状"**：`PR-4d` 保证的是 fail-closed 不回落这一半（`200` + 空列表 + 无 `fallback` 字段），**文案归 `PR-4c`**。 |
