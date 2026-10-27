# HTTP API：注意力推送（Web Push）

## 注意力推送（Web Push）

| 方法   | 路径                           | 说明                                                                                                                                                                                                              |
| ------ | ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/push`                    | `{enabled, available, reason?, publicKey?, subscriptions}`；`publicKey` 为 base64url 未压缩 P-256 VAPID 公钥，仅可用时出现                                                                                        |
| POST   | `/api/push/subscriptions`      | `{endpoint, expirationTime?, keys: {p256dh, auth}}`（即 `PushSubscription.toJSON()`，`expirationTime` 接受但不保存）→ 200 `{id}`；同一 endpoint 幂等返回原 ID；400 非法订阅，409 `{error, reason}` 或已满 32 台   |
| DELETE | `/api/push/subscriptions/{id}` | 204，ID 不存在同样 204                                                                                                                                                                                            |
| POST   | `/api/push/test`               | 无请求体；同时向全部已登记设备发送测试通知并等待推送服务应答 → 200 `{devices, accepted, removed: [订阅 ID], errors: [信息]}`；`removed` 为推送服务报告失效并已删除的订阅，`errors` 只含推送服务主机名；409 不可用 |

`reason` 取值：`disabled`（`[push] enabled = false`）、`storage`（`<state_dir>/push.json` 不可读写）、`quick_tunnel`、`unsupported_origin`。来源按请求 `Origin` 判定（没有时由 `Host` 推断，经隧道到达的视为 `https`）：只有当前有效 Named 设置的 `https://<hostname>`（Web 覆盖优先，与隧道是否运行无关）和 `127.0.0.1` / `::1` / `localhost` 回环来源可以订阅；更改 Named 主机名、切换 Quick 或恢复配置默认后，后续请求立即使用新的有效主机名判定，不继续使用启动时 TOML 的旧值。当前 Quick Tunnel 公网来源为 `quick_tunnel`，其余（VPN、局域网、其他反向代理、本机套接字）为 `unsupported_origin`。订阅来源资格不等于公网 Host/Origin 授权：后者仍只在当前隧道 ready 后授予，停止/失败或重配时撤销。

服务端订阅会话列表，以 `(会话 ID, kind, since)` 去重：启动或重新订阅时快照中已有的 attention 只作为基线，同一信号的 source 变化、输出、spinner 与重连都不再推送；清除后再次置位或种类升级才推送。只推送 `[push] kinds` 中的种类，启发式 `waiting`、attention 清除与进程退出不推送。内部订阅积压断开后按保留的基线比较新快照，补推断开期间新出现的信号一次；500 ms 内置位又清除的信号不推送，不重放断线期间已清除的信号。

推送消息按 RFC 8291 `aes128gcm` 加密、RFC 8292 VAPID 认证，`TTL` 为 `[push] ttl_seconds`，question / permission / error 的 `Urgency` 为 `high`，其余 `normal`；单次请求 10 秒超时，不跟随重定向，推送服务返回 404 / 410 时删除订阅，其他失败只记日志（订阅 ID 与推送服务主机名）且不重试。发送经 64 条有界队列与 2 个 worker，队满丢弃，不阻塞 PTY、会话列表或 HTTP。送达是尽力而为。负载（≤ 4096 字节）：

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
  "webmux": {
    "v": 1,
    "sessionId": "abcdefgh",
    "kind": "permission",
    "source": "agent",
    "since": "2026-01-01T00:00:00Z"
  }
}
```

`title` 为去除控制字符、截断到 80 字符的会话标题，为空或 `include_title = false` 时为 `WebMux`；`body` 按种类固定，形如“等待授权 · Agent 请求执行工具操作”：前半是与会话列表一致的短标签（需要回答、等待授权、本轮完成、出错停止、有通知、响铃提醒），后半是可执行的下一步；`navigate` 为登记时的来源加 `/s/<ID>`。负载不含 cwd、命令、预览、OSC 正文或终端输出。测试通知的标题为 `WebMux 测试通知`、正文为 `若看到此消息，说明推送已生效`，`webmux` 为 `{"v": 1, "test": true}`。

前端 Service Worker `/sw.js` 只处理 `push` 与 `notificationclick`，不拦截请求、不持有 Token；点击时只用 `webmux.sessionId`（须匹配 `^[a-z2-7]{8}$`）与自身来源拼出 `/s/<ID>`，否则打开 `/`。
