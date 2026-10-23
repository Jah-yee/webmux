## Why

WebMux 已能锁存 agent 的提问、授权、回合结束、错误和终端通知（`attention`），但用户必须打开页面才能看到；手机锁屏或关闭页面后，等待输入的 agent 会一直空等（#68）。Web Push 由系统推送服务（APNs / FCM）送达，页面关闭后仍可提醒。Web Push 只在安全上下文可用；仓库已内置 Cloudflare Tunnel，其中 Named Tunnel 提供稳定的 HTTPS Origin，适合作为推送订阅的安全上下文，无需在二进制内实现 TLS。

## What Changes

- **服务端注意力推送**：新增推送子系统，订阅现有会话列表事件，对 `attention` 做 `(会话, kind, since)` 去重差分。
  - 初始快照只建立基线；同一信号重复上报、spinner 输出、预览或客户端数变化都不推送；清除后重新置位才再次推送。
  - 启发式 `waiting`、attention 清除和进程退出不推送。
  - `done` 只代表 agent 回合结束，不等于进程退出。
  - 推送内容只含会话 ID、`kind`、`source`、`since` 与可选的会话标题，不含 cwd、命令、预览、通知正文或任何 PTY 内容。
- **VAPID 与订阅存储**：`state_dir/push.json` 保存 VAPID 密钥与设备订阅（私有权限、原子替换）；损坏时隔离并重建，不可写时只停用推送，不影响会话。
- **发送**：使用标准库实现 RFC 8291（`aes128gcm`）与 RFC 8292（VAPID）。显式设置 TTL 与 Urgency，有界队列和超时，禁止重定向；`404`/`410` 删除失效订阅；不记录 endpoint 或密钥。
- **REST API**：`GET /api/push`、`POST /api/push/subscriptions`、`DELETE /api/push/subscriptions/{id}`、`POST /api/push/test`，沿用现有 Token 与 Origin 检查。
- **安全上下文限定为 Cloudflare Named Tunnel**：
  - 只接受来自 Named Tunnel 公网 Origin（`https://<tunnel.hostname>`）或本机回环 Origin 的订阅。回环 Origin 是浏览器认可的安全上下文，供桌面浏览器与自动化测试使用。
  - Quick Tunnel 每次启动更换随机域名，旧订阅点击后会打开失效地址，因此 Quick Tunnel 来源 SHALL 拒绝订阅并给出改用 Named Tunnel 的操作提示。
  - 其他来源（局域网或 VPN 的 `http://` 地址、其他反向代理）同样拒绝，并提示改用 Named Tunnel 公网链接。
- **隧道关闭时仍发送**：推送经 APNs/FCM 送达，不依赖隧道；隧道关闭或失败期间照常发送，通知点击后能否打开页面取决于隧道是否恢复。
- **前端**：
  - 新增只处理 `push` 与 `notificationclick` 的 Service Worker，点击后聚焦或打开 `/s/{id}`。
  - 设置页新增“通知推送”卡片：本设备开启或关闭、测试通知、订阅设备数。
  - 卡片对非安全上下文、Quick Tunnel、不支持的来源、iOS 未添加到主屏幕、权限被拒绝、服务端停用分别给出分步操作提示。
- **Windows 托盘**：隧道区域新增推送提示行与“手机推送设置说明”。
  - Quick Tunnel 时提示配置 Named Tunnel 的步骤。
  - Named Tunnel 时提示在手机上开启推送的步骤，并显示已订阅设备数。
- **配置**：新增 `[push]` 配置节（`enabled`、`subject`、`ttl_seconds`、`kinds`、`include_title`），默认启用；`bell` 默认不推送。
- **本机状态**：`/local/v1/status` 与 `webmux status --json` 新增可选的 `push` 对象（是否启用、是否可用、订阅数、Named Tunnel 是否满足前置条件），供托盘提示使用；契约版本保持 v1。

不在本次范围：`webmux events --jsonl` / `webmux wait`（依赖 #83 的本机客户端，后续复用同一去重核心）、通用 webhook（ntfy / Bark / Telegram）、进程退出推送、按设备选择 kind、在二进制内实现 TLS，以及 tailscale serve / Caddy 等其他 HTTPS 入口。

## Capabilities

### New Capabilities

- `attention-push`：服务端注意力推送，涵盖 `[push]` 配置、VAPID 与订阅存储、`attention` 差分去重、Web Push 加密与发送、`/api/push*` 接口、Named Tunnel / 回环来源限定与推送内容最小化。
- `push-notification-client`：浏览器侧推送，涵盖 Service Worker 的通知展示与点击跳转、设置页“通知推送”卡片的能力检测、订阅与退订同步、测试通知，以及各种不可用状态的操作提示。
- `windows-tray-push-guidance`：Windows 托盘在隧道区域展示推送前置条件与操作说明。

### Modified Capabilities

- `server-local-api`：本机状态对象新增可选的 `push` 字段（不含 endpoint、密钥或 Token），版本保持 v1。

## Impact

- **后端**：
  - 新增 `internal/push`：配置映射、存储、tracker、加密与发送、分发器。
  - `internal/config` 新增 `[push]` 及其校验。
  - `internal/api` 新增 `/api/push*` 路由，并依据请求来源判定推送是否可用。
  - `internal/contract` 的 `StatusV1` 新增 `Push`。
  - `cmd/webmux` 负责接线与关闭顺序；`cmd/baseline/fixture.go` 若构造 `api.New` 需同步。
- **前端**：新增 `web/public/sw.js` 与 `web/src/push.ts`；修改 `web/src/schemas.ts`、`web/src/pages/Settings.svelte`。会话列表 WebSocket 与 `SessionEventSchema` 不变。
- **托盘**：`internal/tray/menuview.go`、`internal/tray/winui/menu.go` 与对话框。
- **依赖**：不新增 Go 或 npm 依赖；推送加密使用 Go 标准库 `crypto/ecdh`、`crypto/ecdsa`、`crypto/hkdf`、`crypto/aes`。
- **网络**：服务端需要能出站访问推送服务（如 `*.push.apple.com`、`fcm.googleapis.com`、`*.push.services.mozilla.com`）。中国大陆网络通常无法访问 FCM；无 Google Play 服务的 Android 设备不支持 Web Push。
- **文档**：`docs/protocol.md`（新接口与本机状态字段）、`docs/deployment.md`（`[push]` 配置与 Named Tunnel 前置条件）、`deploy/config.example.toml`。
