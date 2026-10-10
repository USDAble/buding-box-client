# 开发客户端连接本地中台

写作参照：dev-docs/relay-runbook.md

开发构建可以在链接时固定本地中台地址，使用真实中台配置、统一逻辑模型和积分网关进行联调。正式及封闭测试构建仍读取原有配置；环境变量、用户配置和缓存不能改变部署地址。

## 构建中台地址明确的开发客户端

在 `cmd/octo-desktop` 执行下面的 PowerShell 命令。两个链接变量必须同时提供，只允许HTTP loopback地址，不允许远程域名、用户名、查询参数或片段。符号只编译进默认的developer构建。

```powershell
go build '-ldflags=-H windowsgui -X github.com/open-octo/octo-agent/internal/productprofile.DeveloperAPIHost=http://127.0.0.1:8000/api/v1 -X github.com/open-octo/octo-agent/internal/productprofile.DeveloperGatewayHost=http://127.0.0.1:8000' -o '../../tmp/client-preview/PuddingBox-local.exe' .
```

不提供链接变量时继续使用 `internal/productprofile/endpoints.json`。`product_production` 和 `product_test` 构建没有这些符号，不能因此覆盖正式或测试平台。模型目录的签名仍使用构建配置中的受信公钥；本地中台的签名Key必须与开发profile对应，不能通过跳过验证完成联调。

## 启动UI与本地服务

先运行真实中台后端8000，再从客户端 `web` 启动 `node node_modules/vite/bin/vite.js --host 127.0.0.1 --strictPort`。该服务在5173代理API和WebSocket到桌面进程的8088，不是中台后端。

启动开发桌面前设置现有开发数据根和UI地址；实际产品数据仍由 `internal/datapath` 处理。只有developer profile接受这些既有开发选项。

```powershell
$env:OCTO_DATA_ROOT='D:\workspace\AIBOX项目\buding-box-client\tmp\client-preview\data'
$env:OCTO_DESKTOP_DEV_URL='http://127.0.0.1:5173'
Start-Process -FilePath '.\tmp\client-preview\PuddingBox-local.exe' -WindowStyle Hidden
```

用 `http://127.0.0.1:5173/@vite/client` 和 `http://127.0.0.1:8088/api/version` 验证服务已启动。产品页面仍需要桌面窗口的身份令牌；直接访问产品API得到403是窗口边界，不应通过移除鉴权解决。已有登录态可能属于原平台，本地中台拒绝时通过正常登录重新取得本地会话。

Windows沙箱可能阻止Vite构建子进程或桌面通知的COM注册，导致进程启动后立即退出；需在已授权的开发启动环境运行，保留日志，不改产品鉴权。崩溃日志位于当前数据根的 `logs/crash.log`，服务日志位于 `logs/serve.log`。

## 网关重试只有一个所有者

模型目录继续把统一model ID投影为 `buding-gateway::<model ID>`；供应商和账号不会进入客户端模型选择。`GatewayEndpoint.Sender` 使用现有OpenAI适配器发往同一个 `/v1/chat/completions` 网关，用户积分和Token保持现有管线。

该网关的HTTP请求只尝试一次，中台Run负责最多3次渠道/账号切换。直接连接其他供应商的上游适配器保留原有重试策略。正文、推理或工具调用开始后不会由客户端透明重放。

验证使用 `internal/productruntime` 的真实HTTP夹具测试、`internal/productprofile` 的链接配置测试和生产tag测试。测试证明调用契约与边界，不等于实际供应商成功推理或真实付费验收。

## 非目标

本文不改变正式默认地址，不创建替代中台，不绕过签名或窗口身份验证，不为账号余额不足创建客户端扣费或备用模型逻辑。
