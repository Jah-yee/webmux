# Design

## Context

- 会话 attention 已锁存在 `session.Info.Attention`（`internal/session/attention.go`）。`raiseAttentionLocked` 只在种类变化或清除后重新置位时更新 `since`，同种类重复上报只刷新 `source`，低优先级信号被忽略。因此 `(会话, kind, since)` 天然就是去重键。
- `Manager.Subscribe()` 先给出 `snapshot`，之后由 500 ms 的 `updates()` 循环发布 `created`/`updated`/`removed`。订阅者缓冲 256 条，积压时被标记为失败并关闭 `Slow`，不会阻塞 PTY。
- 会话不跨重启存在：服务端启动时快照为空，推送子系统可以从第一条事件起完整跟踪。
- Cloudflare Tunnel 已内置（`cmd/webmux/tunnelctl.go`、`internal/api/public.go`）：
  - Named Tunnel 的公网主机名来自 `tunnel.hostname`，启动前已知；
  - Quick Tunnel 的主机名运行时才确定，记录在 `PublicOrigin` 中；
  - 两者都在边缘终止 TLS，再以明文 HTTP 回源，浏览器看到的是 `https://` 安全上下文。
- Token 存在 `localStorage`，HTTP 使用 Bearer 头，WebSocket 使用子协议；`manifest.webmanifest` 已声明 `display: standalone`。
- Windows 托盘通过本机 API 读取 `StatusV1.Tunnel`，`internal/tray/menuview.go` 把它转换为菜单项。
- 依赖面很小（toml、websocket、pty、qr、systray），发布构建 `CGO_ENABLED=0`。

## Goals / Non-Goals

**Goals:**

- 去重状态机是纯函数，后续 `webmux events`/`wait` 及 Windows 客户端可以直接复用。
- 推送路径与 PTY、会话列表、HTTP 处理完全解耦，推送服务故障只影响推送。
- 安全上下文规则只有一处实现（服务端判定来源），前端和托盘只展示结果。

**Non-Goals:**

- 不修改 `/ws/sessions` 帧格式与 `SessionEventSchema`。
- 不实现 `pushsubscriptionchange` 自动重登记：Service Worker 不持有 Token，改由应用启动与设置页同步兜底。
- 不支持旧的 `aesgcm` 编码与 `WebPush` 认证方案；当前 APNs、FCM 与 Mozilla 推送服务都接受 `aes128gcm` 和 `vapid`。

## Decisions

### 1. 新包 `internal/push`，由 `cmd/webmux` 显式构造

推送功能拆为四部分：

- `Store`：读写 `push.json`，管理密钥与订阅；
- `Tracker`：纯状态机，输入 `session.Event`，输出待推送的 `Alert`；
- `Sender`：加密与发送 HTTP 请求；
- `Service`：订阅 Manager，运行 Tracker，把 Alert 送入有界队列并交给 worker 发送。

`serve()` 在 `session.NewManager` 之后构造 `push.New(cfg.Push, cfg.StateDir, m, logger, nil)`，作为参数注入 `api.New`、`api.NewLocal` 与本机状态；关闭时在 `m.Shutdown` 之前调用 `Close`，先停止发送再清理会话。`[push] enabled = false` 时 `push.New` 返回 `nil`：API 把 `nil` 视为 `disabled`，本机状态仍报告 `enabled: false`。最后一个参数是发往推送服务的 `http.RoundTripper`，`nil` 使用默认 Transport 的克隆；测试注入信任 `httptest` TLS 证书的 Transport。

`api.New` 的签名改为 `New(c, m, workspaces, public, push, codes...)`，`api.NewLocal` 改为 `NewLocal(c, m, workspaces, codes, push, local)`：本机套接字提供与 TCP 相同的 `/api/push*`，但来源一律判为 `unsupported_origin`。`cmd/webmux`、`cmd/baseline/fixture.go` 与各测试调用方都要更新，不需要推送的传 `nil`。

备选方案是在 `Manager.updates()` 中直接比较 `previous` 并回调推送。这样会把推送耦合进会话核心，并在 `m.mu` 下执行回调。订阅方式只需要复用现有公共接口，因此采用订阅。

### 2. Tracker 语义

Tracker 为每个会话保存最近一次已知的 `key = (kind, since)`，并记录基线是否已经建立：

- **首个 `snapshot`**：只记录 key，不输出 Alert。
- **`created` / `updated`**：当前 attention 非空，key 与已保存的不同，且 kind 在配置的 `kinds` 中时，输出 Alert。无论是否推送，都把已保存的 key 更新为当前值；attention 为空时保存为空。
- **`removed`**：删除该会话的记录。
- **积压后的 `snapshot`**：`Slow` 关闭后，Service 重新订阅。新快照按普通更新逐会话比较，补推断开期间新出现的 key；快照中不存在的会话视为已移除。

Alert 只携带 `sessionId`、`kind`、`source`、`since` 与 `title`。

attention 在一个 500 ms 周期内置位又被清除时，Tracker 看不到这条信号。这与 issue 中“不承诺持久队列”的边界一致。

### 3. 来源判定只在服务端实现

`api` 层新增 `pushOrigin(r)`，判定方式如下：

1. 有 Origin 时解析 Origin；没有 Origin（例如同源 GET）时，用 `Host` 推断 `scheme://host`。经隧道到达的请求用 `https`，否则按 `r.TLS` 判断。
2. 配置了 Named Tunnel，且来源恰为 `https://<tunnel.hostname>` 时，判为 `eligible`。这里使用配置值，而不是当前 `PublicOrigin`，因此隧道暂时关闭不影响判定；隧道关闭期间该 Host 本身会被拒绝，不会产生误判。
3. 来源主机为 `127.0.0.1`、`::1` 或 `localhost` 时，判为 `eligible`。
4. 来源等于当前 `PublicOrigin`，且实例未配置 Named Tunnel 时，判为 `quick_tunnel`。
5. 其余来源判为 `unsupported_origin`；本机套接字请求同样归入此类。

登记时保存的来源用于组装 `navigate` 绝对 URL。

不接受 tailscale serve 或 Caddy 等其他 HTTPS 来源。这是本变更的明确取舍：只有 Named Tunnel 主机名是在配置中声明、可以校验的稳定来源。以后要放宽时，只需在这里增加一条规则，例如读取 `[push] origins`。

### 4. 标准库实现 RFC 8291 / RFC 8292，不引入 webpush-go

所需算法均在 Go 1.26 标准库中：

| 算法 | 标准库 |
| --- | --- |
| ECDH P-256 | `crypto/ecdh` |
| HKDF-SHA256 | `crypto/hkdf` |
| AES-128-GCM | `crypto/aes` + `crypto/cipher` |
| ES256 签名 | `crypto/ecdsa`，转换为 64 字节 `r‖s` 的 JWT 签名 |

实现分为两个纯函数：

- `encrypt(payload, p256dh, auth, salt, ephemeralKey)`：只生成单条记录，记录大小为 4096，使用 `0x02` 填充分隔符；
- `vapidHeader(endpoint, subject, key, now)`：JWT 的 `aud` 为 endpoint 的 scheme 加 host，`exp` 为 12 小时后。

随机数与时间都通过注入传入，测试可以直接使用 RFC 8291 附录 A 的测试向量。

`SherClockHolmes/webpush-go` 会额外引入 `golang-jwt` 等依赖，而且只用到其中很小一部分。这是安全敏感的单二进制项目，倾向于用可以对照 RFC 向量验证的自有实现。代价是需要自行维护约 200 行加密代码，并补齐测试向量。

### 5. 负载同时兼容 Declarative Web Push 与 Service Worker

负载格式如下：

```json
{
  "web_push": 8030,
  "notification": {
    "title": "deploy",
    "body": "等待授权",
    "navigate": "https://term.example.com/s/abcdefgh",
    "tag": "webmux-abcdefgh",
    "silent": false
  },
  "webmux": {"v": 1, "sessionId": "abcdefgh", "kind": "permission", "source": "agent", "since": "..."}
}
```

- iOS/iPadOS 18.4 及以上可以不经 Service Worker 直接显示通知。即使 Service Worker 被系统清除，订阅也不会因为没有显示通知而被撤销。
- 其他浏览器由 `sw.js` 解析同一份 JSON 并调用 `showNotification`。
- 点击时 `sw.js` 只使用 `webmux.sessionId` 和自身 origin 拼出路径，忽略 `navigate`，从而保证不会打开其他来源。

各种类的 body 文案固定，在 Go 中定义一张表：

| kind | 文案 |
| --- | --- |
| question | 需要回答 |
| permission | 等待授权 |
| error | 出错停止 |
| done | 回合结束 |
| notify | 有新通知 |
| bell | 响铃提醒 |

### 6. 发送与队列

- 队列容量 64，2 个 worker。
- 每个 Alert 依次发给当前全部订阅。订阅数最多 32，同一 Alert 内串行发送，避免对同一推送服务突发并发。
- 共用一个 `http.Client`：总超时 10 秒，`CheckRedirect` 返回 `http.ErrUseLastResponse`，Transport 从 `http.DefaultTransport` 克隆。
- 状态码处理：
  - 2xx 视为成功；
  - 404 / 410 时通过 Store 删除订阅；
  - 413 / 400 记为负载错误，属于实现缺陷，记录日志；
  - 429 / 5xx 只记录日志，不重试。
- 日志只记录订阅 ID 与推送服务的主机名，不记录完整 endpoint。

### 7. 存储格式

`push.json` 的结构：

```json
{
  "v": 1,
  "vapid": {"private": "<base64url d>"},
  "subscriptions": [
    {"id": "...", "endpoint": "...", "p256dh": "...", "auth": "...", "origin": "https://term.example.com", "createdAt": "..."}
  ]
}
```

- 公钥由私钥推导，不单独存储。
- 写入方式与仓库清单相同：先写 0600 临时文件，`fsync` 后 `rename`。
- Store 用自己的互斥锁保护内存副本，写盘在锁内完成。文件很小，登记与删除频率很低。
- 解析失败时把原文件改名为 `push.json.corrupt-<unix 时间>` 后重建。
- 订阅 ID 为 128 位随机 base32。

### 8. 前端结构

- `web/public/sw.js`：手写，不经 Vite 打包，由 `web/dist` 原样嵌入。静态服务对非 `assets/` 路径已经返回 `Cache-Control: no-cache`，浏览器可以及时检查更新。脚本内的会话 ID 正则与 `schemas.ts` 保持一致。纯函数（`parsePush`、`clickPath`、`chooseClient`）只在 Service Worker 作用域内注册监听器，单测在 Node 中直接求值脚本。
- Service Worker 在 `install` 时 `skipWaiting()`、`activate` 时 `clients.claim()`：它没有 `fetch` 处理器，接管不改变任何请求，但让注册前已打开的页面成为受控窗口，点击通知时 `WindowClient.navigate()` 才能复用该窗口。点击逻辑放在 `openNotification(notification)` 中，`focus()` 被拒绝（无用户手势）时仍继续导航。
- `web/src/push.ts`，提供以下纯函数与小型 API 封装：
  - `detectSupport()`：返回 `secure`、`serviceWorker`、`pushManager`、`notification`、`ios`、`standalone`、`permission`；
  - `advise(support, status)`：纯函数，给出卡片显示哪类提示；
  - `urlBase64ToUint8Array`；
  - `enable` / `disable` / `sync` / `sendTest`：通过现有 `api.ts` 发请求，用 Zod 校验响应。
- 本机保存的订阅 ID 存为 localStorage 中的 `webmux.push.subscriptionId`。
- `schemas.ts` 新增 `PushReasonSchema`、`PushStatusSchema` 与 `PushSubscriptionResultSchema`；`ErrorSchema` 增加可选 `reason`，`APIError` 携带它，前端据此在登记被拒时切换到对应提示。
- 卡片是独立组件 `components/PushSettings.svelte`，由 `Settings.svelte` 引用，用现有 `Card`、`Switch`、`Button`、`Badge` 与 `InlineNotice` 组合实现，不新增 shadcn 组件。`support` 与 `loadStatus` 可注入，浏览器组件测试无需 Service Worker 或后端。判定优先级为：
  1. 非安全上下文（此时浏览器整体隐藏 Service Worker 与 Push API，必须先于能力判断）
  2. iOS 非独立显示模式（iOS Safari 标签页同样不暴露 Push API）
  3. 浏览器能力缺失
  4. 服务端给出的原因
  5. 权限被拒绝
  6. 可开启
- 应用启动时的同步放在 `App.svelte` 认证成功之后。只有通知权限已授予且 `navigator.serviceWorker.getRegistration()` 返回已有订阅时才继续，因此 Storybook、浏览器测试与 E2E 默认不会产生全局副作用。
- E2E 无法连接真实推送服务：`PushManager` 在页面中被替换为返回真实 P-256 密钥、不可解析 endpoint 的假订阅；推送事件经 DevTools `ServiceWorker.deliverPushMessage` 以可信事件送达。chrome-headless-shell 即使授权也报告通知权限为 `denied`，因此该用例使用新版 headless Chromium（`channel: "chromium"`），CI 安装完整 Chromium 而不再只装 shell。macOS CI 上新版 headless Chromium 收到推送后 `getNotifications()` 始终为空，通知展示与点击用例只在 Linux 运行；开启、测试与关闭用例在两个平台都运行。

### 9. 本机状态与托盘

- `contract.StatusV1` 新增 `Push *PushStatus`，`localStatus` 从 `push.Service` 读取。服务为 `nil` 时只填 `enabled: false`。
- `tray.PushEntry(snap)` 是独立的纯函数，返回 `Show`、`Info`（提示行）与 `Guide`（说明对话框全文）；只在隧道项显示且状态含 `push` 时显示。Named Tunnel 是否配置由 `push.publicOrigin` 是否存在判断。
- `winui/menu.go` 在 `copyPublic` 下增加两项，说明对话框复用 `dialog.go` 的 `messageBox`。托盘不读取 Token，也不修改配置文件。

## Risks / Trade-offs

- [iOS 主屏幕应用与 Safari 不共享 localStorage，需要再次输入 43 字符 Token] → 提示中写明，可以从托盘复制链接后粘贴 Token。本变更不增加新的登录方式。
- [Named Tunnel 主机名与订阅来源绑定，修改 `tunnel.hostname` 后旧订阅点击失效] → 旧订阅仍能收到通知，但打开的是旧地址。在新地址重新开启后，可以在旧设备上关闭推送，或等待推送服务返回 410 后自动清理。文档说明修改主机名需要在各设备重新开启。
- [VAPID 私钥丢失或存储重建后，所有订阅失效] → 应用启动时的同步会发现公钥不一致，并提示重新开启。
- [推送服务不可达，例如中国大陆网络无法访问 FCM] → 只记录日志，不影响会话；卡片的送达说明已注明。
- [500 ms 内置位并清除的 attention 不会推送] → 用户已经输入，说明正在关注会话，不需要提醒。
- [标准库自行实现加密可能出错] → 使用 RFC 8291 附录 A 测试向量做确定性单测，并用 E2E 在真实 Chromium 中解密验证。
- [Chromium 在 CI 中无法连接真实推送服务] → E2E 只验证订阅、登记、退订与 Service Worker 的展示和点击逻辑（通过 `ServiceWorkerRegistration.showNotification` 或向 SW 注入 push 事件）。APNs/FCM 真实送达列为手动验收。
- [Quick Tunnel 用户无法使用推送] → 这是按用户决定作出的取舍：设置页和托盘都给出改用 Named Tunnel 的步骤，避免产生点开即失效的订阅。

## Migration Plan

- 默认启用推送，首次启动会在 `state_dir` 生成 `push.json`。没有设备订阅时不产生任何出站请求。
- 不需要迁移数据。回滚到旧版本时，`push.json` 会被忽略；之后再升级会继续使用该文件。
- 旧版托盘不认识 `push` 字段，会直接忽略；新版托盘遇到旧版服务端（没有 `push` 字段）时隐藏推送项。

## Open Questions

- iOS 18.4+ Declarative Web Push 在已安装 Service Worker 时，是否总把 push 事件交给 Service Worker 处理：需要真机确认，不影响负载格式与规格。
