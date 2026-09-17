# 开发期假后端（**已退役**）

> ## ⛔ 该文件已于 2026-09-13（`PR-3`）删除 —— 本文件是它的历史说明，不是现状
>
> `web/src/dev/devBackend.ts` 与其安装块都已移除；`/api/*` 现在**一律**到达本地 Go 服务，DEV 构建下也一样（vite 的 `/api` 代理 → `localhost:8088`）。
> 保留本文件，是因为它记录了**这个替身当时顶替的是哪一层、为什么危险**；拆除的拍板理由、验收判据与两条"怎么自己验证"的证据见
> [`../../20260911/开发计划.md`](../../20260911/开发计划.md) 的 `PR-3` 一节，规则留存见 [`../../20260911/需求基线.md`](../../20260911/需求基线.md) §5.6 的 `G5`。
>
> **两处必须记住的教训（正文里原来的说法是错的）：**
> 1. **它从未被 tree-shake 掉。** 下表"上线影响"原文写"模块被 tree-shake（已对构建产物验证）"——**该断言从未对着产物验证过，且是错的**。静态导入的模块，其顶层语句会进生产包并在每次启动执行；对照构建可复现（`V-47`）。
> 2. **它把"路由根本没注册"变成了看不见。** 它对**任何未注册的 `/api` 路径回 `{}` + `200`** —— `V-24` 与 `V-46` 两条缺口都因此长期不可见。它顶替的是**本地 `/api` 边界**，与顶替中台的 `internal/productclient/clienttest` **不是一回事**（`V-10`）。

| 项 | 内容（均为**拆除前**的状态） |
|---|---|
| 状态 | ⛔ **已退役（`PR-3`，2026-09-13）** |
| 位置 | ~~`web/src/dev/devBackend.ts`~~（已删除） |
| 安装点 | ~~`web/src/main.ts`~~（安装块已删除，原位留一条说明与 `V-47` 的动态导入约束） |
| 作用域 | 开发 · 浏览器端 · 运行期（开发规范 §3.10） |
| 上线影响 | **原写"无"，实际有**：静态导入下夹具数据随产物分发、顶层语句每次启动都执行（`V-47`）。现已随删除消失 |

> ✅ **此段已过期，保留为历史。** 它记录的正是"先删会让谁失效"这个顾虑，而那个顾虑在实际拆除时被判定为**不成立**（桌面构建下那 5 条本来就已经 404，删它不改变任何路径的行为；拍板理由见 `PR-3` 一节）。原文如下：
>
> ⚠️ **（2026-09-12 的原文）拆除它的触发条件今天尚未达成（`V-24`）。** 它目前**掩盖了 6 条缺失的 Go 路由**：前端调用 11 条 `/api/product/*`，而 `Runtime.Mount`（`internal/productruntime/runtime.go:78-84`）**只注册 5 条**（`state` / `send-code` / `login` / `logout` / `locale`）；其余 `chat-modes`、`nickname`、`prefs`、`sensitive/check`、`sensitive/dict`、`sensitive/dict/import` **在真机构建下全部 404**。所以**先删它会让设置页、词库页与模式选择器当场失效** —— 正确顺序是「先把这 6 条挂上、再删」，归属见 [`开发计划.md`](../../20260911/开发计划.md) §1.1。

## 1. 它是什么

一份**假的后端**：在浏览器里拦下 `/api/*` 就地应答，让 P1–P13 的页面能点得通、流程能走完。

**它不是什么**：不是设计、不是接口草案、不是真后端的替代品。里面的任何实现细节都不要照搬进真实现。

## 2. 为什么需要它

P1–P13 的页面调用 `/api/product/*`，真实现是一组 Go handler（见 §3）。**这些代码不在本分支上**，于是：

- `GET /api/product/state` 404 → `refreshProductState()` 兜底把 phase 置成 `blocked`
- 发验证码 / 登录 404 → 永远停在登录页，进不了聊天页

结论：**没有应答方，流程一步都走不了**。这个文件只解决"让流程走通"这一件事。

## 3. 真后端会怎么替换它

> **⚠️ 落点已被 [`../../20260911/需求基线.md`](../../20260911/需求基线.md) `G1` 覆盖（2026-09-11）。** 本表原来把真实现放在 `internal/server/product_*.go`；`G1` 已定为**产品逻辑进 `internal/productruntime` 及其下游**（`productclient` / `productpolicy` / `productstate` / `credentialstore`），**不继续加进 `internal/server`**。下表落点列已按 `G1` 改写。

真后端落地后，下表路由交给真实现，**本文件整体删除**（见 §4）。

| 假后端应答的路由 | 真实现落点（见 [`../../20260911/需求基线.md`](../../20260911/需求基线.md) `G1`） |
|---|---|
| `/api/product/state`、`/locale`、`/logout` | `internal/productruntime`（+ `internal/productstate`） |
| `/api/product/send-code`、`/login` | `internal/productruntime`（门：`internal/productgate`） |
| `/api/product/nickname` | `internal/productruntime` |
| `/api/product/prefs` | `internal/productruntime` |
| `/api/product/sensitive/check` | `internal/productruntime`（+ `internal/sensitive`） |
| `/api/product/sensitive/dict`（GET/PUT）、`/import` | `internal/productruntime`（+ `internal/sensitive`） |
| `/api/product/chat-modes` | `internal/productruntime`；**应答形状改为目录投影**，见 `需求基线.md` `B1` / `B5`（不再是 `chat-modes.json` 的形状） |
| 其余（`config` / `sessions` / `endpoints` / …） | 上游既有 handler；本文件只给空壳 |
| WebSocket 事件流 | `internal/server` 的 ws handler；本文件用桩，只回 `subscribed` |

## 4. 怎么删除（✅ **已执行，2026-09-13 / `PR-3`**）

1. ✅ 删掉 `web/src/dev/devBackend.ts`（`web/src/dev/` 目录随之消失）
2. ✅ 删掉 `web/src/main.ts` 里的安装块

**实际结果与本节原稿有两点差异：** ① 原稿说"删掉两行"，实际当时已经改成**死分支内的动态 `await import(...)`**（`V-47`），所以删的是一整块 + 一条说明；② 原稿说"没有第三个引用点 —— `rg devBackend web/` 可自证"，**这句只对 `web/` 成立**：`internal/productclient/clienttest/server.go` 与 `cmd/productstub/main.go` 的注释里还有两处引用（内容是"夹具要与前端假后端一致"），已一并改正 —— 夹具现在**只有替身一个定义处**。

## 5. 它故意不实现什么

| 不实现 | 为什么 |
|---|---|
| 字段校验、手机号绑定/遮码 | 真后端的职责。在这里重写等于给出**第二份契约定义**，必然漂移（开发规范 §3.8） |
| 词库去重 / 导入预览 | 同上；`internal/sensitive/normalize.go` 是唯一 owner |
| 真实持久化 | 只在内存里，刷新即回初始态 |
| WebSocket 事件流 | 只回 `subscribed`（新会话首条消息靠它才放行）。其余事件用 `window.__dev.push(...)` 手动注入 |

凭据只认需求 §8 钉死的那两个（见 §6），其余一律拒绝 —— 保留错误路径可走。

## 6. 演示凭据

| 字段 | 值 |
|---|---|
| 手机号 | `13800001234` |
| 验证码 | `123456` |
| 昵称 | 任意 |
| 激活码 | `BUDING-DEMO-0001` |

`activated=false`（首次激活）才出现激活码输入框；登录过一次后转成"二次登录"形态。

## 7. 为什么不用真后端跑

真后端要整套 Go 服务加桌面壳：window token 由 `cmd/octo-desktop` 铸造（没有它产品门不生效，也就看不到登录页），Vite 侧的 `/api`、`/ws` 代理已配好指向 `:8088`。本批次只要"可交互的静态页面"，所以选了成本最低的浏览器内替身。
