# P0-03：客户端模型网关 Sender 与 Token 账本状态接入

| 项 | 内容 |
| --- | --- |
| 当前仓库主责 | 客户端 provider/Agent 组 |
| 中台协作方式 | 网关/计费组实现服务端状态机；当前仓库只按[中台交付包](../产品客户端与中台对接/中台交付包.md)接入 SSE 和状态查询 |
| 可并行 | 与 P0-02、P0-04、P0-06、P0-07 并行 |
| 依赖 | P0-01 的 `ModelGatewaySender` 契约；目录模型 ID 约定 |
| 需求基线 | [P0 正式上线需求基线](../P0-正式上线需求基线.md) R4、R5、R10；客户端不定义或兼容扣费接口 |

## 目标

废弃客户端“每条消息扣一分”的做法。当前仓库实现的 `ModelGatewaySender` 只负责发起请求、消费 SSE、处理取消/恢复和呈现最终状态；外部中台模型网关才是唯一正式云端模型出口，并在同一 `clientRequestId` 中完成鉴权、模型路由、最大额度预留、流式转发、token 结算/释放和账本记录。

## 发给中台的交付要求（本仓库不实现）

1. 实现 `POST /v1/ai/chat/completions` SSE、`GET /v1/ai/requests/{clientRequestId}`、`GET /v1/credits/ledger`，字段以[中台交付包](../产品客户端与中台对接/中台交付包.md) §5 为准。
2. 客户端只上传 `modelId`、脱敏消息副本、`clientRequestId`、会话 ID、最大输出和已批准的 tool policy；不得上传 `amount`、供应商 base URL/key 或价格。
3. 账本状态仅允许 `received → reserved → streaming → settled|reversed|reconciliation_pending`；同 key 的第二次请求不调用供应商。
4. 按 `modelId + pricingVersion + plan + token 分类` 计算整数 `microCredits`；供应商缺 usage 时标记估算或待对账，不能让客户端补扣。
5. 使用服务端密钥管理系统存供应商 key，按模型 allowlist 路由；网关 access token 的 `aud` 与 scope 必须校验，不能把 token 转发给供应商。

## 当前仓库开发任务

1. 实现 `ModelGatewaySender` 作为 `agent.Sender` 的客户端 adapter；生成一次 UUID 并贯穿 HTTP、SSE、会话事件、诊断与状态查询。取消、断网、重启只查询状态，不新建 key 重发。
2. SSE 必须区分 `meta`、`delta`、`tool_call`、`usage`、`error`、`done`；仅 `usage.state=settled` 更新最终余额。
3. 将远端错误码、余额不足、`reconciliation_pending` 映射为明确的会话/UI 状态，不能依据客户端猜测 token 价格或自行扣减。
4. 余额不足不回退 local/provider；只有目录显式允许的免费或套餐额度模型可继续调用。
5. sandbox 未到位时以 fake SSE 和请求状态 fixture 覆盖事件顺序、取消、重复 request ID 和恢复路径。

## 验收场景

- 同一请求重复、并发重复、客户端取消、浏览器断网、供应商 429/5xx、供应商已消耗但连接中断。
- 每个场景仅一笔终态账本，可用 `clientRequestId` 同时查到客户端、网关和 ledger。
- 抓包与配置审查确认客户端绝不获得供应商 key/base URL。

## 合并策略

当前仓库先用 fake SSE/状态查询完成 sender 协议测试，再接中台 sandbox。账本 schema、价格配置、供应商调用和服务端迁移由中台在其仓库独立交付；本仓库不创建相关 PR，也不将其与 UI 修改混合。
