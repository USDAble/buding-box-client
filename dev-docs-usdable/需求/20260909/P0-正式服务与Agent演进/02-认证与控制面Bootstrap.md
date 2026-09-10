# P0-02：客户端认证与控制面中台接入

| 项 | 内容 |
| --- | --- |
| 当前仓库主责 | 客户端 SDK/桌面组 |
| 中台协作方式 | 按[中台交付包](../产品客户端与中台对接/中台交付包.md)提供 OpenAPI、sandbox、签名公钥和错误码；不在本仓库实现服务端 |
| 可并行 | 与 P0-03、P0-04、P0-06、P0-07 并行 |
| 依赖 | P0-01 的 DTO、profile 和 `productclient` 装配方式；正式凭证落盘依赖 P0-08 的 credential store |
| 需求基线 | [P0 正式上线需求基线](../P0-正式上线需求基线.md) R1、R3、R4、R8；冲突时先更新基线 |

## 目标

让生产客户端在一次真实登录后，透过 P0-01 定义的 `AuthClient` 和 `ControlPlaneClient` 获得短期产品会话、账号/激活摘要、套餐/余额、模型目录版本、能力矩阵和词库版本。中台是外部控制面，不向客户端发送供应商 endpoint 或 API key。

## 发给中台的交付要求（本仓库不实现）

1. 交付 `sms/send`、`auth/login`、`auth/refresh`、`auth/logout`、`client/bootstrap` 的 OpenAPI 3.1、sandbox 与错误码注册表。
2. access token 必须限制 audience 为布丁 API/模型网关；refresh token 轮换、可撤销、不可写日志。登录携带随机 `installId`，禁止采用 MAC/硬盘序列号。
3. bootstrap 返回 policy envelope：`policyVersion`、`issuedAt`、`expiresAt`、`minClientVersion`、`keyId`、`signature`；客户端仅接受信任公钥验证通过且未过期的策略。
4. `phone_mismatch` 只回机器码和脱敏提示，客户端导向客服；中台客服后台的换号/撤销操作必须 RBAC + 审计。
5. 激活到期、套餐到期、额度不足使用不同字段和错误码，绝不复用“授权到期”。

## 当前仓库开发任务

1. 在 `internal/productclient` 实现 P0-01 的 `AuthClient` / `ControlPlaneClient`；外部请求/响应类型只在该包映射为客户端 DTO，server handler 不直接发 HTTP。
2. 实现 refresh 单飞：并发 401 只发一次刷新；失败后清本地短期会话并回登录页。
3. 将 access/refresh token、policy snapshot、catalog cache 分级保存；access token 只驻留内存，refresh token 只能通过 P0-08 的 credential store 保存，严禁写回 `product-state.json`；导出/诊断包一律排除凭证。
4. bootstrap 缓存必须带版本、过期时间、签名结果；离线只能使用未过期且已验证缓存，不可接受空策略扩权。
5. 将账号 panel、登录页、模型选择器读取的状态统一从 bootstrap DTO 派生，避免本地 product-state 与远端各自为准。
6. 在 sandbox 到位前，使用同一 OpenAPI fixture 的 fake client 做单元/集成测试；不得把临时 JSON 写进页面层，也不得将 fake 作为生产 local 服务。

## 验收

- 正常登录、刷新、登出、token 撤销、复制数据目录、手机不匹配、过期策略、离线缓存分别有 contract test。
- 伪造签名、降级 `policyVersion`、错误 audience、过期 token 均被拒绝。
- 日志、错误上报和诊断包中查不到 access token、refresh token、完整手机号。

## 合并方式

字段合同由 P0-01 的 B0 先冻结并发给中台；当前仓库随后以 generated/fake client 开发中台 client。中台提供 OpenAPI + sandbox 后，通过 contract CI 才合真实 HTTP 实现；不要让页面团队直接使用临时 JSON。
