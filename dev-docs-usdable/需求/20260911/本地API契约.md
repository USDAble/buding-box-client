# 本地 API 契约（`/api/product/*`）

> **状态**：`v0.2`（2026-09-11 自查修订，**待确认**）
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

`v1` 上**还没有真后端**（`internal/productruntime` 等包未落地），所以本文件**不是从 Go handler 反向提取的**，而是从以下三处正向整理的：

| 依据 | 位置 | 可信度 |
| --- | --- | --- |
| 前端调用与解析逻辑 | `web/src/lib/product.ts`、`api.ts`、`sensitive.ts`、`sensitiveDict.ts` | **高** —— 前端必须这样解析才能工作，字段名与信封形状是硬事实 |
| 临时假后端 | [`../2260906/技术方案/开发期假后端说明.md`](../2260906/技术方案/开发期假后端说明.md) + `web/src/dev/devBackend.ts` | **中** —— 它是替身，形状可被真后端改动 |
| 本文档的设计决定 | 本文件新定 | **待实现** —— 一律标 🚧 |

因此每行的状态要这样读：`✅ 现状` = **前端已依赖此形状**（改它要动前端，不是纯后端改动）；`🚧 修订` = 本契约新定，代码尚未实现。

**标 `✅` 不等于「后端已验证」**。后端侧的权威校验（去重、归一化、限流边界）只有真后端写完、契约测试跑起来才算数。

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
| 8 | GET | `/api/product/chat-modes` | 否 | 否 | 🚧 去 `fallback`（`S-4`） |
| 9 | GET | `/api/product/sensitive/dict` | 否 | 否 | ✅ |
| 10 | PUT | `/api/product/sensitive/dict` | 是 | 否 | ✅ |
| 11 | POST | `/api/product/sensitive/dict/import` | 是 | 否 | ✅ |
| 12 | POST | `/api/product/sensitive/check` | 否 | 否 | ✅ |

> **「需登录」列是设计约定，不是观测事实。** 前端对 `/api/product/*` 一律带 window token（`api.ts` 的 `withWindowToken`），**产品门是否拦某条路由由服务端决定**，前端不区分。本列表达的是**应有的门策略**：凡读写账号数据或改词库的都要登录；`state` / `locale` / `send-code` / `login` 必须在未登录时可达，否则登录页根本渲染不出来。实现时应由产品门按**前缀白名单**放行这四条，而不是逐路由判断。

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
- **🚧 与本分支现状的差异**：前端已实现 `boxCode` 全链路（`feat/activation-box-code`）；后端尚无。

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

### 2.8 `GET /api/product/chat-modes` 🚧（去 `fallback`）

模式与模型选择器的数据源。

- **请求**：无体。
- **应答 `200`（新形状）**：
  ```json
  {
    "modes": [
      {
        "id": "privacy",
        "displayName": { "zh": "隐私模式", "en": "Privacy" },
        "models": [{ "id": "…", "displayName": {"zh": "…", "en": "…"}, "compositeId": "endpoint::model" }],
        "defaultModel": "endpoint::model"
      }
    ],
    "catalogVersion": "…",
    "policyVersion": "…"
  }
  ```
- **🚧 与现状的差异**（`S-4`，必须一起改）：
  1. **删掉 `fallback: boolean`**。现在的类型注释写「`chat-modes.json` 不可读时用内置默认」——这正是 `B1` 已作废的行为。**顺着旧形状实现，会把「内置名单」带回来**。
  2. 加上 `displayName`（中英双语）。**模型展示名来自中台签名目录的 `displayName`，前端不得保留 id→名称映射表**（`B6`，§3.8）。
  3. `id` 保持**英文 ASCII、不做本地化**（它是数据键，等同 `buding-*` 那类固定标识）。
  4. `catalogVersion` / `policyVersion` 是**本契约新拟的字段名**（🚧），取名的目的是让前端能判断「目录是否换了一版」而不必比对内容。**若中台的信封里已有版本字段，以中台为准并回改本行**（§3.8：同一个事实只有一个 owner）。
  5. `modes[].displayName` 同样来自中台的**模式分组 `internal/chatmode`**，不是本契约拟定。
- **降级（§3.9）**：目录不可用时的行为是**显示「目录暂不可用 + 重试」，不给内置名单**，也就是 fail-closed。不许回落本地 provider（`A1`/`B1`）。
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

| # | 未定 | 归属 |
| --- | --- | --- |
| `S-1` | 本文件的**契约测试**：`web/src/lib/product.contract.test.ts` + Go 侧 `contract_test.go`，断言「本文件登记的每个端点与错误码都已实现」 | 与本文件同批落地 |
| `S-3` | 敏感词三类落点具体路径（用户词文件、服务端词库缓存、内置词）与量级 | [`需求基线.md`](需求基线.md) §5.1 |
| `S-5` | `product-state.json` 的升级与缺字段规则 | 同上 |
| `S-6` | `ControlPlaneConfigured()==false` 时用户看到什么 | 同上 |
| `S-7` | 控制面「连不上中台」vs「鉴权失败」的面板口径 | 同上 |
| `N-2` | `data/credential.json` 的字段表与写盘方式 | 本轮盘点新发现 |
| `N-5` | §4 四个 WS 事件的作用域与重放语义 | 本轮盘点新发现 |

---

## 6. 修订

| 版本 | 日期 | 变更 |
| --- | --- | --- |
| `v0.1` | 2026-09-11 | 首版：登记 12 个本地端点、三类错误信封、错误码总表、产品相关 WS 事件；标出 `login` 加 `boxCode` 与 `chat-modes` 去 `fallback` 两处 🚧 |
| `v0.2` | 2026-09-11 | 自查修订（发现 1 处**事实错误** + 4 处**未标来源**）:① **`send-code` 的手机号错误走业务级 `{"code":"invalid_phone"}`，不是 `fieldErrors.phone`** —— 前端 `product.ts:177` 读的是 `body.code` 再自己落到 `phone` 字段；原 `v0.1` 写错了信封，照它实现会导致「不报错但文案错」。§2.2 改正，§3 增列第 3 处「同名不同信封」差异。② 新增 §0.1「依据与可信度」，说明 `v1` 上**没有真后端**、本文件是**从正面前端整理**而非反向后端提取，并给出三档可信度。③ §2.4 `logout` 与 §2.5 `locale` 的应答形状标注「取自假后端 / 本契约新定」，不再伪装成已验证。④ §2.8 标明 `catalogVersion` / `policyVersion` 是本契约新拟名，若中台已有版本字段则以中台为准。⑤ §1.4 补「需登录列是设计约定」，并写明应由产品门按**白名单**放行未登录可达的四条路由。⑥ §3 补「未知 code 应视为契约违约并记日志」，不得静默落到 `product.submit_failed`。 |
