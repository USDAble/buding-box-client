# 运行时 Profile 配置

写作参照：`dev-docs/endpoint-custom-headers-design.md`。沿用其“范围 → 事实源 → 约束 → 失败结果 → 验证”的结构；本文以 `internal/productprofile`、桌面组装和发布守卫为事实源。

> Profile 是随桌面二进制编译进去的不可变控制面配置和信任锚。它回答“这个构建可以连接谁、信任谁、是否必须通过产品网关”，而不保存用户、会话或服务端动态数据。

## 范围与非目标

Profile 提供控制面地址、签名公钥和开发能力开关。桌面组装读取它一次，进而构造中台客户端、签名目录验证与模型网关策略。账户、refresh token、词库、目录缓存、余额和偏好属于数据根或中台响应，不能回写 Profile。

不做以下事情：

- 不从 `config.yml`、环境变量、命令行、数据根、Web UI 或缓存选择生产控制面。
- 不在地址或可信密钥缺失时回退到默认 host、localhost 或未验证目录。
- 不将私钥、提供方 key、用户 bearer/refresh token 或用户偏好放入 Profile。
- 不用 Profile 承载既有 channels、tools、MCP 或后台任务的启动、入口隐藏或授权策略；这些能力仍由各自模块拥有。

## 接口地址的编译期来源

开发与生产 Profile 共用 [endpoints.json](../internal/productprofile/endpoints.json)：

```json
{
  "apiHost": "http://127.0.0.1:8000/api/v1",
  "gatewayHost": "http://127.0.0.1:8000"
}
```

`apiHost` 包含目标服务的 API 前缀；`gatewayHost` 为模型网关基址。修改后重新编译并重启客户端，Vite 热更新不会加载 Go 嵌入的地址。开发与生产构建共用此文件，原有 Profile JSON 只保存权限和验签配置，不能再填写地址。签名公钥仍须与目标服务匹配。

测试 Profile 不读取该文件：它要么使用 `testing.json` 中固定的本机替身地址，要么由发布负责人将该文件改成已批准的 HTTPS 测试中台地址和测试签名公钥后重新打包。这样，测试中台切换不会意外改变开发或生产包的地址。

## 构建选择与不可变性

`internal/productprofile.Current()` 由 build tag 选择权限 JSON；开发与生产构建再合并 `endpoints.json`，测试构建直接使用 `testing.json` 中的封闭地址。首次读取后经 `sync.Once` 缓存；同一进程内不会因主机状态改变：

| 构建条件 | 嵌入文件 | `name` | 用途 |
| --- | --- | --- | --- |
| 默认（无 `product_production`） | `profiles/developer.json` | `developer` | 本地开发、桌面热重载和本地测试/Sandbox（含 `productstub`）联调。 |
| `-tags product_test` | `profiles/testing.json` | `testing` | 可交给测试人员的测试包；同样关闭开发入口和环境模型来源。 |
| `-tags product_production` | `profiles/production.json` | `production` | 生产桌面包。 |

默认构建是开发 Profile，因此“能编译、能运行”不代表“可发布”。生产打包命令选择 `product_production`，测试打包命令选择 `product_test`；两者都不能意外退回 developer Profile。`make release-profile-check` 继续保护生产命令，`make test-profile-check` 验证测试 Profile 与替身签名锚。

Profile JSON 解析或校验失败会在首次读取时 panic，构建不能靠猜测继续运行。它是发布配置错误，不是可由最终用户修复的运行时降级。

## 字段、所有者与当前消费者

| 字段 | 含义 | 当前实现中的消费者 |
| --- | --- | --- |
| `schemaVersion` | 当前唯一支持版本为 `1`。 | `Profile.Validate`；其他版本直接拒绝。 |
| `name` | `developer`、`testing` 或 `production`。 | 决定校验规则与 `IsProduction`。 |
| `apiHost` | 控制面 API 基址。 | `productclient.New`；开发/生产来自 `endpoints.json`，测试来自 `testing.json`。 |
| `gatewayHost` | OpenAI 兼容模型网关基址。 | `GatewayEndpoint`；开发/生产来自 `endpoints.json`，测试来自 `testing.json`。 |
| `trustedKeyIDs` | `keyId → base64 Ed25519 公钥` 信任表。 | 目录/策略签名验证。 |
| `allowDevWebview` | 是否允许 `OCTO_DESKTOP_DEV_URL` 改变桌面 webview 地址。 | 仅桌面开发 Profile 使用。 |
| `allowEnvironmentModelSource` | 是否允许不经产品控制面的模型来源。 | 反向导出为 `RequireGateway`；目标设计还用同一事实控制本地模型列表与管理入口。 |
| `allowDataRootOverride` | 声明开发 Profile 允许数据根覆盖。 | **当前没有运行时代码读取此字段**；不能把它当作对 `OCTO_DATA_ROOT` 的实际生产限制。 |
| `startLocalStandin` | 测试包是否启动固定 loopback 中台替身。 | 仅 `product_test` 桌面壳读取；远程测试中台包必须为 `false`。 |

`allowDataRootOverride` 是重要的当前事实：schema 有字段不等于该字段已经形成运行时控制。文档不能宣称生产构建已通过它禁用环境覆盖；本轮明确暂不接入该行为，未来若要接入，需要先讨论发布兼容性和验证方案。

## 地址与签名信任边界

控制面需要两类独立事实：地址决定请求发往何处，可信密钥决定签名响应是否可以被接受。两者不能互相替代。

`apiHost` 与 `gatewayHost` 都必须是绝对 HTTP(S) URL，且不能包含 userinfo。生产 Profile 要求两者非空并使用 HTTPS；开发 Profile 允许为空或本机 HTTP，以支持本地替身。地址中路径版本的责任由客户端决定：当前 `apiHost` 应带 `/v1`，而 `gatewayHost` 是否带 `/v1` 都能被网关客户端规范化，不能强行把后者写成另一条硬规则。

`trustedKeyIDs` 的每个 key ID 非空，值必须是 base64 解码后的 32 字节 Ed25519 公钥。空表在结构上合法，却不意味着“接受未验证响应”：它表示没有任何签名目录可被信任。

统一地址文件当前保留本地 HTTP 地址。生产构建要求先将其改为 HTTPS，否则首次读取配置时拒绝启动。测试包仅在 `startLocalStandin: true` 时允许其固定的 `127.0.0.1:8788` HTTP 地址；关闭替身后同样要求 HTTPS。 `ControlPlaneConfigured()` 将空、解析失败或 `.invalid` 域名都判为未配置；不会发生网络 fallback。空可信键表则使 `HasTrustedKeys()` 为 false。

```text
Profile
  ├─ apiHost + gatewayHost ──→ 是否存在真实控制面
  ├─ trustedKeyIDs ─────────→ 是否可验证签名策略/目录
  └─ 两者共同 ──────────────→ 桌面是否能安全使用产品网关
```

## 两种 Profile 的实际差异

开发 Profile 同时承担日常开发和本地测试/Sandbox：当前统一地址连接本地中台 `http://127.0.0.1:8000`，控制面地址带 `/api/v1`；如需使用 `productstub`，在统一入口改回 `8788` 和 `/v1`，网关地址为裸 host，并信任其开发签名键。它允许 `OCTO_DESKTOP_DEV_URL` 将真实桌面窗口指向 Vite；见 `开发与联调.md`。

测试 Profile 是可分发的封闭变体，不允许开发 WebView、环境模型来源或数据根覆盖。其默认文件开启 `startLocalStandin`，且只接受固定的 `127.0.0.1:8788` 地址；测试版桌面进程在本地先启动与 `productstub` 同一份 `clienttest` 契约替身，再打开窗口。因此测试人员在中台尚未就绪时无需额外启动服务，也不会连接构建机。

当测试中台可用时，发布负责人将 `testing.json` 的 `startLocalStandin` 改为 `false`，填入测试中台的两个 HTTPS 地址和其签名公钥，再用同一测试打包命令重建。地址和公钥仍只在编译期读取，测试人员不能通过环境变量、配置文件或界面改连任意服务。

developer Profile 允许本地模型来源，因此它验证的是本地客户端、控制面契约和开发模型闭环，不等同于生产 Profile 的“只允许可信目录和 gateway”限制。发布前仍必须用 production Profile 的自动检查和嵌入式桌面验收验证产品边界。

生产 Profile 禁止 `allowDevWebview`、`allowEnvironmentModelSource` 与 `allowDataRootOverride` 三个开发能力；校验层面只要任一为 true 就拒绝启动。桌面壳据 `allowDevWebview` 决定是否读取 `OCTO_DESKTOP_DEV_URL`，生产构建忽略该变量并始终加载进程内服务的嵌入 Web UI。

模型路径的判断不靠调用方比较 `name`：`RequiresControlPlane()` 由 `allowEnvironmentModelSource` 反向得出。桌面把它传给服务端的 `RequireGateway`，使生产构建不能在控制面/网关不可用时悄悄改走环境或用户配置的模型来源。

同一个事实还应投影到 Web UI：开发 Profile 显示本地 endpoint 管理、本地模型和本地私密测试标记；产品 Profile 隐藏管理入口并从可选列表排除本地模型。前端不能自行比较 Profile 名称或 host，避免 UI 与服务端 `RequireGateway` 形成两套环境判断。

## 用户可见的失败结果

桌面组装把 `ControlPlaneConfigured()` 和 `HasTrustedKeys()` 投影为本地 `GET /api/product/control-plane` 的两个布尔值。`登录、访问控制与拦截页.md` 定义其用户结果：

| 配置事实 | 拦截结果 | 不能做什么 |
| --- | --- | --- |
| 未配置控制面 | 显示“本包未配置”的拦截说明页。 | 不显示可用但必定失败的登录表单，不回退 host。 |
| 已配置但无可信密钥 | 显示“缺少信任锚”的拦截说明页。 | 不接受未签名或无法验证的目录。 |
| 两者齐备 | 显示正常登录/激活流程。 | 不保证网络可达或账号有效。 |

网络不可达、上游故障、账户受限和会话失效不是 Profile 配置错误；它们由中台与本地运行时的失败分类处理。把“读不到配置事实”误报成构建损坏同样错误，前端会保留登录页而不作断言。

## 修改与发布规则

修改 Profile 等于改变发布物的信任边界，应按发布变更处理：

1. 先确认实际 API/gateway 地址、签名 key ID、公钥、受众和轮换兼容性；不把占位符或私钥写入仓库。
2. 开发与生产地址只更新 `endpoints.json`；测试地址只更新 `testing.json`，并同步设置 `startLocalStandin`。权限和公钥更新对应 Profile；规则变更时同步更新测试、`中台接口契约.md` 中受影响的请求与签名说明，以及本文件。
3. 用生产 build tag 运行 Profile 与桌面壳相关测试，并运行 `make release-profile-check release-config-check`；真实中台联调另行进行，不能放进自动化测试。
4. 若变更会使旧版本不再信任新目录或会改变数据根/模型来源行为，先给出兼容、迁移、回滚和用户可见失败方案，等待确认后再实施。

密钥轮换通过发布新的信任表完成。旧 key 的保留时长、双签名期间和撤销时机属于需要确认的发布策略，不能由 Profile 代码猜测。

## 验证

- `go test ./internal/productprofile`：默认开发 Profile 的选择、schema、URL、公钥与占位地址判定。
- `go test -tags product_production ./internal/productprofile`：生产 tag 下的嵌入选择和约束。
- 在 `cmd/octo-desktop` 嵌套模块运行对应的默认与 `product_production` 测试，验证开发 webview 覆盖被允许、生产包忽略它。
- `make release-profile-check release-config-check`：防止开发 Profile 混入交付，并检查生产 Profile 内容。

## 相关文档

- `中台接口契约.md`：控制面、网关、token 与签名协议。
- `登录、访问控制与拦截页.md`：两个配置事实如何转化为拦截页。
- `开发与联调.md`：开发 Profile、Vite 桌面联调和 `productstub`。
- `便携打包.md`：生产产物和发布前检查。
