# 本地 API 契约

写作参照：`dev-docs/endpoint-custom-headers-design.md`

> 本文件定义 Web UI 与本地 Go 服务之间的产品接口：`/api/product/*`、相关会话路由及产品 WebSocket 事件。中台接口由 `中台接口契约.md` 定义；两份契约不重复字段或错误码的论证。

## 写作参照与结构取法

参照 `dev-docs/endpoint-custom-headers-design.md`：先界定范围、术语和注册边界，再给出稳定的数据形状、端点总表、逐类行为、错误与验证。本文把“影响范围”替换为实际的路由组装/窗口门模型，把详细设计落在请求、响应、持久化副作用和 WebSocket 作用域上；不引入该示例中配置字段改造或版本历史等无关内容。

## 范围与事实源

本地运行时是接口形状的实现事实源，前端类型和调用方必须与本文件同步。一次接口变更同时更新：本文件、handler 与测试、前端类型与测试；不以“接口已注册”或“前端能调用”代替契约验证。

`ProductStateDTO`、端点和错误码各只有一个定义位置：状态对象在本文件，平台传输契约在 `中台接口契约.md`，用户可见文案在 `web/src/lib/i18n.ts`。前端不接收中台 bearer token，也不从中台错误 message 直接生成界面文案。

## 非目标

- 不把本地服务与中台之间的请求、签名或认证字段复制到这里。
- 不用版本表、端点“完成状态”或 PR 编号表达当前接口。
- 不为普通浏览器访问伪造桌面产品门能力；`octo serve` 不提供桌面组装的 `MountAPI`，因此默认不挂载这组产品端点。

## 通用约定

请求与应答使用 `application/json; charset=utf-8`。桌面壳为 webview 创建内存态 window token；前端保存到 `sessionStorage`，每次产品 HTTP 请求携带 `X-Octo-Window-Token`。产品门校验窗口身份，不能替代平台会话或业务状态判断。

### 注册与访问模型

`internal/productruntime.Runtime.Mount` 是唯一的产品路由清单。桌面壳先组装 `productstate`、凭证存储、平台客户端、目录缓存和敏感词引擎，再把 `Mount` 交给 `server.Config.MountAPI`。服务端通过专用注册器把这些路由置于既有 API 认证与 `Cache-Control: no-store` 之下；若配置了窗口令牌，还会以常量时间比较额外校验 `X-Octo-Window-Token`。

这三层不能混写：

| 层 | 实际行为 | 不负责什么 |
| --- | --- | --- |
| 路由挂载 | 仅桌面组装成功且已生成窗口令牌时挂载；状态或凭证存储无法打开时整组路由不存在，客户端看到 404 而不是伪造空状态 | 不判断用户是否登录 |
| 服务通用认证 | 沿用所有 `/api` 路由的认证/loopback 规则 | 不识别“哪个桌面窗口” |
| 产品门 | 仅覆盖经 `MountAPI` 注册的路由；桌面端令牌缺失或错误时为 `403 {"error":"product_gate"}`；未配置令牌时关闭 | 不覆盖上游 `/api` 路由，也不等同于账号登录 |

`Runtime.Handler()` 只是测试和独立宿主使用的裸 `http.Handler`，故意不施加窗口门。不要把它当成桌面端暴露面的安全模型。当前 handler 也没有“已登录才允许调用”的统一中间件；下表的“运行时前提”只说明一次调用要取得有意义结果所需的状态或外部依赖，不能误读为 HTTP 登录授权。

本地错误使用以下形状：

| 类别 | 形状 | 用途 |
| --- | --- | --- |
| 产品门 | `403 {"error":"product_gate"}` | 窗口令牌无效时前端回到产品门。 |
| 字段级 | `400 {"fieldErrors":{"<field>":"<code>"}}` | 能定位到输入框的校验失败。 |
| 业务级 | `400`、`403` 或 `409` 加 `{"code":"<code>"}` | 不能归属单一输入框的业务结果；`phoneMasked` 可随 `phone_mismatch` 返回。 |
| 限流 | `429 {"retryAfterSec":<int>}` | 验证码发送与产品反馈使用。 |

`{"field":"<field>","code":"invalid_value"}` 是本地值校验的专用形状，不属于 `fieldErrors`。旧端点的 `{"error":"..."}` 或 `{"message":"..."}` 仅作兼容读取，不是新增接口的默认信封。

请求体不能解码时，运行时返回 `400 {"code":"invalid_request"}`。本地存储、依赖或写入失败返回 `500 {"code":"internal_error"}`；平台不可达、没有控制面配置或平台 5xx 会按平台错误映射为 503 级业务码（例如 `network_unavailable`、`upstream_unavailable`、`control_plane_unconfigured`），而不是伪装成字段错误。刷新令牌被明确拒绝时是例外：返回 `401 {"code":"unauthorized"}`，运行时清除本地会话，前端进入 blocked 状态。

## 产品状态

所有返回状态的端点使用 `ProductStateDTO`。它是已持久化产品状态的公开投影：

| 字段 | 形状 | 约束 |
| --- | --- | --- |
| `schemaVersion` | number | 只增不减。 |
| `loggedIn` / `activated` | boolean | 已登录时 `activated` 必为 `true`。 |
| `activation` | object \| null | `{activatedAt, expiresAt, boxCode?}`；盒子编号不是密钥。 |
| `account` | object \| null | `{phoneMasked, nickname, lastLoginAt}`；不返回明文手机号。 |
| `credits` | object | 只含 `{balance}`；余额只从账本刷新。 |
| `plan` | object | `{name}`。 |
| `prefs` | object | 当前为 `{locale, inputSensitiveCheck}`；会话保护边界不作为账户默认偏好保存。 |
| `suppressOnboarding` | boolean | 控制桌面首启向导。 |

状态中永不返回 token、明文手机号或一次性激活码。`ProductStateDTO` 不承载编译期控制面配置、目录可用性等运行时事实；这些由专用端点给出。

## 端点总览

| 方法 | 路径 | 运行时前提 | 结果 |
| --- | --- | --- | --- |
| GET | `/api/product/state` | 状态存储已组装 | 直接返回 `ProductStateDTO`。 |
| POST | `/api/product/send-code` | 已配置平台客户端 | 发送登录验证码。 |
| POST | `/api/product/login` | 已配置平台客户端 | 登录或激活并返回 `{state}`。 |
| POST | `/api/product/logout` | 凭证与状态存储；平台撤销为尽力而为 | 清本地凭证并尝试撤销平台会话。 |
| PUT | `/api/product/locale` | 状态存储 | 保存界面语言。 |
| PUT | `/api/product/nickname` | 状态存储与敏感词引擎 | 保存昵称并返回 `{state}`。 |
| PUT | `/api/product/prefs` | 状态存储 | 更新偏好并返回 `{state}`。 |
| GET | `/api/product/models` | 最后一次已接受的签名目录缓存 | 返回厂商/模型只读投影；不触发网络、不混入本地 endpoint。 |
| GET | `/api/product/box` | 平台客户端与可用平台会话 | 返回当前帐号唯一已绑定盒子的安全投影；不读本地缓存、不探测局域网。 |
| GET / PUT | `/api/product/sensitive/dict` | 数据根与词库文件 | 读取或覆盖用户敏感词。 |
| POST | `/api/product/sensitive/dict/import` | 数据根与词库文件 | 合并导入敏感词。 |
| POST | `/api/product/sensitive/check` | 敏感词引擎可选 | 输入框即时检测。 |
| GET | `/api/product/privacy/rules` | 无；注册表编译进运行时 | 返回内置个人信息规则的只读版本和稳定 ID。 |
| POST | `/api/product/privacy/transform` | 产品运行时已组装个人信息引擎 | 在本机生成自动脱敏文本和提示摘要；不是发送安全边界。 |
| GET | `/api/product/control-plane` | 无平台请求 | 返回本构建的控制面可用性。 |
| GET | `/api/product/catalog` | 目录缓存；需要刷新时还依赖平台会话 | 返回目录可用性。 |
| GET | `/api/product/credits` | 平台客户端与可用平台会话 | 从账本刷新余额并返回 `{state}`。 |
| POST | `/api/product/feedback` | 平台客户端与可用平台会话 | 校验并转发用户主动填写的产品反馈，返回最小回执。 |
| POST | `/api/product/check-updates` | 桌面壳、产品门 | 用户主动触发一次只读版本查询，返回 `{latest,available}`；不下载、不安装。 |

所有已挂载的产品端点都先经过上节的窗口门，但没有本表以外的 handler 级“登录要求”。例如 `state` 必须在登录前可读，`locale` 必须能在登录页保存；而 `logout`、偏好或词库路由的处理器本身不先检查 `loggedIn`。前端应根据状态和接口结果组织流程，后端涉及平台的调用则由平台会话决定是否能成功。

## 账户与偏好

### 读取状态与发送验证码

`GET /api/product/state` 直接返回 `ProductStateDTO`。读取失败时前端进入可恢复的产品门状态，不应无限加载。

`POST /api/product/send-code` 接收 E.164 形式的 `{"phone":"+<国家码><号码>"}`，成功返回 `{"cooldownSec":60}`。为兼容既有中国用户，11 位大陆手机号或无 `+` 的 `86` 前缀在本地归一为 `+86` 后再发送；界面支持空格、短横线、括号与全角数字输入。手机号格式错误使用业务级 `invalid_phone`，不是 `fieldErrors.phone`；冷却期返回 `retryAfterSec`。验证码响应可以预留人机校验字段，但当前接口不把它当作必填流程。

### 登录与激活

`POST /api/product/login` 接收 `phone`、`code`、`nickname`，并可选地接收一对 `activationCode` 与 `boxCode`。两个凭证要么同时提供、要么同时省略；本地只校验这个形状，是否已有授权由中台决定。

成功返回 `{"state": ProductStateDTO, "dictionaryNotice"?: ...}`。词库同步降级不阻断成功登录，前端只把运行时给出的降级或恢复状态映射为文案。

字段级错误是 `invalid_phone`、`invalid_code`、`nickname_format`、`nickname_sensitive`、`invalid_activation` 与 `invalid_box_code`。业务级错误包括 `code_not_sent`、验证码过期或错误的 `invalid_code`、`activation_invalid`、`activation_code_used`、`box_code_unknown`、`box_code_mismatch`、`activation_required` 与 `phone_mismatch`。

`invalid_activation` 表示输入格式，`activation_invalid` 表示中台验证失败，不能合并。一次未带激活凭证的登录被中台以激活类错误拒绝时，本地状态应反映该账号未激活；前端可以刷新状态，但不应在用户阅读错误时自动替换表单。登录和激活表单始终允许由用户主动互相切换。

### 登出与偏好

`POST /api/product/logout` 成功返回 `{"ok":true,"revoked":<bool>}`。本地凭证与产品登录态无论平台撤销成功与否都要清理；`revoked:false` 明确告诉前端平台会话未被撤销。登出不删除已绑定手机号、昵称或激活信息。

`PUT /api/product/locale` 接收 `{"locale":"zh"|"en"}`，成功返回 `{"ok":true}`。默认语言跟随系统，非中文使用 `en`；非法值返回 `{"field":"locale","code":"invalid_value"}`。

`PUT /api/product/nickname` 接收昵称并返回 `{"state": ProductStateDTO}`。`nickname_format` 与 `nickname_sensitive` 是业务级码，不能错误地包装为 `fieldErrors`。

`PUT /api/product/prefs` 当前接收任意子集：`locale`、`inputSensitiveCheck`，未给出的字段保持不变。成功返回持久化后的 `{state}`；非法值使用 `{"field":"<field>","code":"invalid_value"}`。个人信息保护和私密会话通过会话创建或保护策略接口写入，不进入账户默认偏好。

## 模型目录、词库与输入检测

`GET /api/product/models` 返回：

```json
{
  "state":"ready",
  "catalogVersion":"…",
  "vendors":[{"id":"vendor-a","displayName":"厂商 A","models":[{"id":"model-a","displayName":"模型 A","compositeId":"gateway::model-a","confidential":true,"confidentialPriority":100}]}]
}
```

`state` 为 `ready`、`absent`、`stale` 或 `unverifiable`。只有 `ready` 返回目录模型；其他状态返回空 `vendors`，且绝不回退本地清单。展示名按当前产品语言投影。旧 `/api/product/chat-modes`、三组会话字段和默认模式偏好均已退出运行时契约。

敏感词接口由词库所有者统一归一化：

- `GET /api/product/sensitive/dict` 返回 `{"builtin":[...],"user":[...]}`；用户词文件不存在等同于空用户词，且不创建文件。
- `PUT /api/product/sensitive/dict` 用 `{"user":[...]}` 完整替换用户词，返回实际持久化后的 `{"user":[...]}`。归一化后为空的词返回 `{"code":"invalid_word","word":"<原词>"}`，整次写入不得改变文件；重复词和内置词被去重。
- `POST /api/product/sensitive/dict/import` 用 `{"words":[...],"dryRun":<bool>}` 合并导入，返回 `{"added":<int>,"skipped":<int>}`。空词、重复词和内置同形词计入 `skipped`；`dryRun:true` 绝不写盘。
- `POST /api/product/sensitive/check` 接收 `{"text":"..."}`，始终返回 `{"hit":<bool>,"masked":"..."}`。这是输入提示，不是安全边界；实际发送链路必须再次检测。
- `GET /api/product/privacy/rules` 返回 `{"ruleVersion":"builtin-1","rules":["cn_resident_id","..."]}`。它是 `internal/pii` 内置注册表的只读投影，供“设置 → 安全与隐私”的个人信息规则页展示；不得返回正则、命中原文、样例或逐规则开关，也不依赖个人信息引擎是否已组装。

## 运行时可用性与余额

`GET /api/product/control-plane` 返回 `{"configured":<bool>,"hasTrustedKeys":<bool>,"allowEnvironmentModelSource":<bool>}`。这些值是构建 profile 的事实，不属于 `ProductStateDTO`。前端只用 `allowEnvironmentModelSource` 决定是否合并本地模型、显示本地模型管理入口和允许本地私密测试标记；不得再从 profile 名称、host 或本地列表是否为空推断。

现有本地 endpoint 接口继续负责可真实调用的本地模型，不被 `/api/product/models` 取代。目标 model 对象在原有 `model`、`vision` 之外增加 `confidential`：

```json
{"model":"local-model-id","vision":true,"confidential":true}
```

`POST /api/config/endpoints` 的嵌套 `models`、`POST /api/config/endpoints/{id}/models` 和 `GET /api/config/endpoints`/CRUD 响应使用同一形状。对同一 endpoint 和 model ID 再次 POST 仍沿用现有 upsert 语义，用于修改 `vision` 或 `confidential`，不新增一条平行的“私密模型”接口。字段缺失按 `false` 读取。开发 Profile 保存成功后失效对应 sender 缓存并刷新统一模型投影；下一回合使用新配置，不要求重启。正式产品模式虽然不为隐藏入口删除这些上游路由实现，但前端不读取或调用它们，本地 endpoint 不进入模型投影或回合路由，只允许中台 gateway。

`GET /api/product/catalog` 返回：

```json
{"state":"ready|absent|stale|unverifiable","retryable":<bool>,"catalogVersion":"...","expiresAt":"<RFC3339 或空串>"}
```

目录状态由本地运行时单点判定：`ready` 为有效缓存，`absent` 为没有缓存，`stale` 为缓存过期且刷新失败，`unverifiable` 为验签失败。`unverifiable` 不可由用户重试恢复，因此 `retryable:false`；其他缺失或过期状态可触发一次条件刷新。前端只映射状态到文案，不能重新推断状态。

`GET /api/product/credits` 从中台账本刷新余额、写入状态投影并返回 `{"state": ProductStateDTO}`。登录和 WebSocket 只可触发这条刷新路径，不能成为余额的第二个写入来源。会话失效清凭证并回产品门；其他平台失败不覆盖已有余额。

## 产品反馈由本地运行时代理

`POST /api/product/feedback` 是“帮助与反馈”页使用的桌面产品路由，而不是浏览器直连中台。它接收：

```json
{"category":"bug|suggestion|other","title":"用户填写的标题","content":"用户主动填写的详细描述","reproduction":"可选复现步骤","expected":"可选期望结果","impact":"low|normal|high","contact":"可选联系方式","idempotencyKey":"uuid"}
```

`category` 必须为三个枚举之一；`title` 去除首尾空白后为 1–120 个 Unicode 字符，`content` 为 1–4000；`reproduction`、`expected` 各至多 2000，`contact` 至多 200；`impact` 必须为 `low`、`normal` 或 `high`。`idempotencyKey` 必须为 UUID，单次点击重试必须复用同一值。成功返回控制面已接受的最小回执：

```json
{"feedbackId":"fb_...","acceptedAt":"2026-09-19T00:00:00Z"}
```

运行时以当前平台会话调用控制面 `POST /feedback`，将 `idempotencyKey` 原样置入 `Idempotency-Key`，但不把 access token、安装标识或平台响应中的诊断 `message` 返回给 Web UI。账户身份、时间戳和请求元数据由控制面从认证与标准请求头取得；前端不得提交或伪造它们。

该接口只传递上述 JSON 字段：`contact` 是用户自愿输入的唯一可选联系方式，不能由帐户手机号补填。不得读取、派生或附加聊天内容、会话历史、附件、OCR、上传文件、工具调用、模型输入输出、日志、数据根路径、手机号、token、原始设备标识或任何“默认诊断包”。运行时不持久化正文，也不在失败后后台重发；界面可在内存中保留表单，交由用户明确再次提交。

本地字段错误使用 `400 {"fieldErrors":{"category":"invalid_value"}}`、`{"fieldErrors":{"title":"invalid_length"}}`、`{"fieldErrors":{"content":"invalid_length"}}` 或相应可选字段错误；错误 JSON 或非法 idempotency key 为 `400 {"code":"invalid_request"}`。平台拒绝映射为稳定的 `401 unauthorized`、`403 feedback_not_allowed`、`409 idempotency_conflict`、`429 feedback_rate_limited`（含 `retryAfterSec`）或 `503 network_unavailable` / `upstream_unavailable`；未知平台业务码不透传，返回 `503 upstream_unavailable`。成功、失败或重试均不得改变 `ProductStateDTO`。

## 盒子投影不泄露设备连接细节

`GET /api/product/box` 返回当前帐号唯一已绑定盒子；当前版本没有“未绑定”的成功空对象。中台没有该资源、帐号无权访问或会话失效分别通过稳定平台错误映射给界面，界面显示可行动错误而不造一个离线盒子。

```json
{
  "id":"box_...",
  "displayName":"我的布丁盒子",
  "state":"online|offline|degraded|attention|unknown",
  "boundAt":"RFC3339",
  "lastSeenAt":"RFC3339（可选）",
  "softwareVersion":"可选展示版本",
  "management":{"canView":true},
  "capabilities":{
    "privateModels":{"state":"available|unavailable|coming_soon","count":1},
    "tools":{"state":"...","count":0},
    "knowledge":{"state":"...","count":0},
    "automation":{"state":"...","count":0}
  }
}
```

它是平台授权后的展示投影，不是桌面到盒子的管理协议。不得出现 `boxCode`、activation code、内网/公网地址、端口、硬件序列号、日志、模型 endpoint、密钥、token 或任何可用来绕过控制面的字段。每次读取都经当前平台会话；本地不缓存状态，用户点击刷新才发起下一次读取。开发替身可以返回固定数据，但该夹具只存在于 `productstub`，正式产品没有 Mock 降级。

## 会话路由与 WebSocket

`PATCH /api/sessions/{id}/protection` 接收 `personal_info_protection`、`confidential_session` 和可选 `model_id`，只允许在保护策略锁定前更新。开启私密会话且当前模型不合格时，服务端从当前可信投影自动选择排序最高的合格私密模型；客户端也可用 `model_id` 明确指定。服务端在同一会话锁中校验并原子保存模型绑定与版本化 `protection_policy`。成功返回完整策略和有效模型绑定；锁定后返回 `409 session_policy_locked`，没有合格模型返回 `409 confidential_model_required`。创建会话接口原子接收同样的用户可选字段和初始模型绑定；`version` 与 `locked` 只能由服务端写入。旧 `/api/sessions/{id}/chat_mode` 已删除，旧历史字段仅在 JSON 读取边界被忽略，不迁移为安全保证。详细竞态和生命周期语义见 [模型选择与私密会话](模型选择与私密会话.md)。

`POST /api/product/privacy/transform` 接收 `{"text":"..."}`，返回 `{"hit":<bool>,"masked":"...","matches":[{"category":"...","count":1}],"ruleVersion":"..."}`。前端命中后不请求确认，直接以 `masked` 继续发送并展示“已自动脱敏”提示。它与服务端发送入口使用同一个 `internal/pii` 引擎，标准占位符在重复处理时保持不变；响应使用 `Cache-Control: no-store`，路由不得记录请求或响应 body。发送入口仍对旧客户端或绕过 transform 的请求执行权威脱敏；运行时未组装引擎或处理失败时返回 `500 privacy_transform_failed`，不返回原文。

产品 WebSocket 事件如下：

| 事件 | 载荷 | 语义 |
| --- | --- | --- |
| `credits_update` | 无 | 提醒所有窗口调用余额刷新；不携带余额，也不重放。 |
| `input_sensitive` | `{session_id, text}` | 拒绝敏感输入前返回已打码文本；被拒内容不入库、不广播、不发模型请求。 |
| `privacy_applied` | `{session_id,categories,count,rule_version}` | 服务端发送入口对旧客户端原文或遗漏命中执行了权威脱敏；不携带原值，也不作为历史事件重放。 |
| `datastore:lost` | 无 | 数据目录失联时冻结输入；新连接必须重放冻结状态。 |
| `datastore:restored` | 无 | 同一路径恢复时解除冻结；不重放。 |
| `turn_error` | `{session_id, error, code?, input_rolled_back?}` | 有码时由前端按码取文案；原始 `error` 只作未知错误兜底。 |

每个事件只有一个发射方和一个重放策略。事件消费方不能从缺失的 `session_id` 猜测会话范围：缺失表示全局事件；`input_sensitive` 与 `privacy_applied` 是会话级事件。`privacy_applied` 是瞬时提示，不进入历史重放；重新打开会话只展示已经持久化的脱敏文本。

## 错误码

用户文案不在本表；未知码是契约违约，应记录并走明确兜底，不能悄悄渲染为空。

| code | 形状或场景 |
| --- | --- |
| `product_gate` | 无效窗口令牌。 |
| `invalid_phone` / `invalid_code` | 可为字段级格式校验，也可在验证码发送/登录中是业务级；由信封区分。 |
| `nickname_format` / `nickname_sensitive` | 昵称校验；昵称更新时使用业务级信封。 |
| `invalid_activation` / `invalid_box_code` | 激活凭证输入格式。 |
| `invalid_value` | 本地枚举值校验，使用 `{field, code}`。 |
| `invalid_word` | 词库替换中归一化后为空的列表项，使用 `{code, word}`。 |
| `code_not_sent` / `activation_invalid` / `activation_code_used` | 登录或激活的业务结果。 |
| `box_code_unknown` / `box_code_mismatch` / `activation_required` / `phone_mismatch` | 需要用户处理、重新激活或联系客服的业务结果。 |
| `input_sensitive` | 本地敏感输入被拒；REST 返回打码文本，WebSocket 使用同名事件。 |
| `privacy_transform_failed` | 产品 transform 路由未组装引擎，或受保护输入处理失败；本次输入不得广播、落库或调用模型。普通服务端发送路径默认使用内置规则，不把依赖缺失降级为明文发送。 |
| `session_policy_locked` | 首个用户回合已经落库，保护策略不可再改变。 |
| `confidential_model_required` | 请求开启私密会话或切换模型时，提交的模型不具备私密资格。 |
| `confidential_model_unavailable` | 已锁定私密会话在回合开始前失去可用的合格模型；不得回退普通模型。 |
| `feedback_not_allowed` | 当前已认证账户不可提交反馈；不结束本地会话。 |
| `idempotency_conflict` | 同一反馈幂等键被用于不同正文；保留表单，要求用户重新发起一次提交。 |
| `feedback_rate_limited` | 控制面对反馈节流；可携带 `retryAfterSec`，界面禁用提交至该时刻。 |
| `unauthorized` | 会话刷新被明确拒绝；本地以 401 回产品门。 |

## 验证与变更

每次改动端点、状态对象、错误码或事件时，至少验证请求形状、成功应答、每种可见失败、持久化副作用、产品门与登录边界，以及前端的消费行为。契约测试应枚举本文件登记的端点与错误码，阻止“文档有形状、路由未注册”或“实现新增码、前端无处理”的漂移。
