# 产品客户端与中台对接

> 本目录只定义当前客户端调用外部中台所需的合同、DTO 和测试方案。中台控制面、模型网关、账本、数据库、密钥管理和部署不在当前仓库实现。
>
> **命名已修正**：P0 不创建 `internal/backend` 或 `internal/productapp`。新建 `internal/productclient` 表示“客户端的产品服务 API client”；模型流在 `internal/productclient/gateway`，本地策略在 `internal/productpolicy`。详见[客户端架构演进评审](../P0-正式服务与Agent演进/客户端架构演进评审.md)。
>
> **上游约束**：`internal/server` 的通用运行时默认零改、不迁移也不重构；2026-09-08/09 已落入其中的布丁产品逻辑按 [P0-01A](../P0-正式服务与Agent演进/01A-既有产品逻辑迁出internal-server.md) 迁出。只在 [P0-00](../P0-正式服务与Agent演进/00-Server最小改动与生产Profile前置.md) 已逐项批准且无外部替代方案时做中性小改动。

## 文件清单

| 文件 | 内容 | 阅读对象 |
| --- | --- | --- |
| [开发计划.md](开发计划.md) | `productclient`、gateway、policy、credential store 的客户端契约与 server 最小挂点 | 客户端研发 |
| [中台交付包.md](中台交付包.md) | 可直接转发的外部接口、网关计费、能力矩阵、更新、文档版本和验收合同 | 中台/网关/运维/测试 |
| [P0 正式上线需求基线](../P0-正式上线需求基线.md) | 产品范围与不可突破的安全、计费、隐私边界 | 全部角色 |
| [P0 开发顺序与协作计划](../P0-正式服务与Agent演进/P0-开发顺序与协作计划.md) | `main` → `buding` 基线确认、并行分工、合并门和发布门 | 客户端/中台/测试 |
| [P0-00A 生产入口可见性与功能保留](../P0-正式服务与Agent演进/00A-生产入口可见性与功能保留.md) | 不删除既有配置、MCP、channel、工具和后台能力的前提下隐藏首发入口 | 客户端/产品策略 |
| [P0-01A 既有产品逻辑迁出](../P0-正式服务与Agent演进/01A-既有产品逻辑迁出internal-server.md) | 历史产品 HTTP/状态/门/词库迁出 `internal/server`，本地积分在 P0-05 删除 | 客户端核心 |

## 客户端与中台的边界

| 当前仓库做 | 中台做 |
| --- | --- |
| `productclient` remote/合同测试实现、gateway sender、签名验证、缓存、凭证安全保存、PEP、UI 状态与 Windows 验收 | 短信/账号、控制面、供应商路由、token 预留/结算、账本、KMS、服务端审计、sandbox 与运维 |

正式模型请求由中台网关在同一 `clientRequestId` 内鉴权、预留、调用、结算与记账。客户端仅消费最终 usage/余额投影；**没有**客户端 `amount` 扣费接口，也不保留本地固定积分兼容路径。供应商 endpoint/key 永不下发给标准客户端。

## 当前执行顺序

```text
P0-00 upstream/profile 基线
  -> P0-01 productclient / gateway / policy contract
  -> 合同测试实现 并行开发 P0-02/03/04/06/08/09/10
  -> sandbox 联调
  -> P0-05 production gateway 装配 + 删除固定积分
  -> P0-07 Windows 发布门
```
