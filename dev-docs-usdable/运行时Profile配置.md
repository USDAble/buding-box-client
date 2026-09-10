# 运行时 Profile 配置

| 项 | 内容 |
| --- | --- |
| 状态 | P0 全局工程约束 |
| 适用范围 | 所有桌面发布、打包脚本、启动装配和产品专属模块 |
| 关联 | [P0 正式上线需求基线](需求/20260909/P0-正式上线需求基线.md)、[P0-00](需求/20260909/P0-正式服务与Agent演进/00-Server最小改动与生产Profile前置.md)、[P0-00A](需求/20260909/P0-正式服务与Agent演进/00A-生产入口可见性与功能保留.md) |

## 1. Profile 不是部署环境

`production` 和 `developer` 是**编译进桌面二进制的运行时 Profile**，不是由环境变量、用户数据或 `data/config.yml` 选择的“正式/开发环境”。

| Profile | 使用场景 | 本地 provider 与用户模型配置 | 运行时能否切换 |
| --- | --- | --- | --- |
| `production` | 标准签名发布包 | 配置功能保留；P0-05 后正式会话只允许受信目录与中台网关 | 否 |
| `developer` | 本地开发、自动化测试、开发构建 | 允许，受现有开发工具约束 | 否；须重新构建 |

`developer` 是唯一的非生产 Profile；标准包不得因本地研发数据、旧缓存、环境变量或手工修改配置而获得开发能力。

## 2. 全局配置载体与边界

P0 新建 `internal/productprofile/`，并将下列两份**只读、嵌入二进制**的配置作为唯一 Profile 事实来源：

```text
internal/productprofile/profiles/production.json
internal/productprofile/profiles/developer.json
```

构建标签选择其中之一：标准发布脚本必须使用 `product_production`；未指定该标签的本地构建使用 `developer`。安装包签名覆盖最终二进制，因此用户不能修改 Profile 配置而不破坏签名。

**`product_production` 是本仓库唯一的反向标签，作用范围不止 Profile 本身。** 目前有两处代码由它决定是否编译：

| 包 | 被排除的内容 | 排除后 production 拿到什么 |
|---|---|---|
| `internal/productprofile` | `profile_developer.go`（`developer.json` 嵌入） | `profile_production.go`（`production.json` 嵌入） |
| `internal/provider/local` | 确定性回复引擎、含敏感词的演示文案、`data/logs/local-provider.jsonl` 写入器 | 恒失败的桩（同导出面），见 [P11 §7.1](需求/2260906/技术方案/P11-本地确定性模型通道.md) |

因此**被该标签排除的代码必须单独编译与测试**，否则它会静默腐烂到出包才暴露：`make test-production`（= `go build/vet/test -tags product_production`），CI 在 `go.yml` 的 Ubuntu job 跑同一组命令。`package-portable.mjs` 还会对**已构建的 exe** 断言 P11 假模型 marker 不存在（`checkProductionBinary()`）—— `release-profile-guard` 只证明标签写进了构建命令，它证明标签到达了编译器。

`data/config.yml` 仍是上游的用户偏好与模型配置，供 developer、自动化测试和既有功能兼容使用，**绝不**保存或覆盖 Profile。production 是否显示相关入口由能力矩阵决定；中台下发的目录、策略、词库和协议版本也不是 Profile 配置：它们各自验签、缓存并受版本语义约束。

## 3. 配置 schema

两个 JSON 使用同一 schema。配置不保存用户令牌、供应商 key、余额或可由中台动态决定的模型目录。

```json
{
  "schemaVersion": 1,
  "name": "production",
  "allowDevWebview": false,
  "allowEnvironmentModelSource": false,
  "allowDataRootOverride": false,
  "startup": {
    "channels": true,
    "tools": true,
    "mcp": true,
    "backgroundTasks": true
  }
}
```

`production.json` 必须把所有 `allow*` 值设为 `false`。这组字段只约束开发 WebView、环境模型来源与数据根覆盖，不能被 WebView、模型输出、环境变量或本地配置开启。用户模型配置和 local provider 不在 Profile schema 中：它们是既有功能，正式会话的模型来源边界由 P0-05 的 gateway sender 实现，不能用未接入的 Profile 字段假装关闭。`startup` 保留上游已有能力；菜单显示、用户资格和实际执行权限分别由 P0-04 的能力矩阵与本地 PEP 决定，不能将隐藏误当成删除。`developer.json` 明确列出其允许的开发入口，避免“未设置即默认开放”。

新增、删除或改变字段时，必须同时更新本文件、两份 JSON、解析/校验测试，以及受影响的打包脚本；任何安全语义变化还须回写 P0 需求基线。

## 4. 装配规则

桌面进程在读取数据根、`serve.env` 或构造上游 server 前加载当前 Profile：

1. `production` 忽略开发 WebView URL、provider/model/key 环境变量、`serve.env` 和 `OCTO_DATA_ROOT`。
2. `production` 保留 `config.yml`、MCP、channel、工具和后台任务的既有功能；P0-00A 只隐藏首发不开放的 UI 入口。`config.yml` 保留的是**功能**，不因此成为 production 正式会话的模型来源；它在 developer/test 可正常配置第三方 endpoint/URL/key。`server.New` 不再种入任何 endpoint（`ensureLocalEndpoint()` 已于 2026-09-11 删除）。
3. P0-05 完成后，正式桌面会话的模型请求只经中台目录与网关；这不删除 developer/test 对已有配置的使用。
4. 产品 HTTP、认证、目录、策略、词库和模型网关逻辑由 `internal/productruntime` 及其下游 `productclient`、`productpolicy`、`credentialstore` 持有；不得继续加入 `internal/server`。
5. 若上游运行时确实无法在 desktop 装配层接入，只能新增一个无布丁业务名词的窄扩展端口；每一处例外都须在 P0-00 登记、单独评审并有黑盒回归。

## 5. 发布验收

- 生产构建使用 `product_production`，配置解析成功且 Profile 名称为 `production`。所有出包路径（`make desktop-portable` / `desktop-app` / `desktop-appimage`、`portable.yml`、`release.yml`、`desktop.yml`、`windows-installer-check.yml`）都必须带该标签，由 `make release-profile-check`（CI `release-profile-guard`）固定；任一出包路径漏掉标签即 CI 失败（`.goreleaser.yaml` 只构建 `cmd/octo` CLI，不在范围内）。
- 预设所有开发环境变量和用户 `config.yml` 后，生产桌面仍不加载开发 WebView 或数据根 override；`config.yml`、MCP、channel、工具和后台能力仍保持既有功能。
- developer 构建保留本地开发闭环；它不能通过运行时输入伪装成已签名的 production 包。
