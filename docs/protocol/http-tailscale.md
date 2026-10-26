# HTTP API：Tailscale 状态与设置

所有端点需要认证并通过 Host/Origin 检查。

| 方法与路径                       | 说明                                                                              |
| -------------------------------- | --------------------------------------------------------------------------------- |
| `GET /api/tailscale`             | 检测与运行状态；`?refresh=1` 绕过检测缓存，只影响检测，不触发写命令               |
| `PUT /api/tailscale/settings`    | 保存网页设置并立即应用，应用中返回 202，否则 200                                  |
| `DELETE /api/tailscale/settings` | 删除网页设置并关闭本实例入口；本进程不按 TOML 自动开启，下次启动按 TOML 生效，200 |

```ts
interface TailscaleStatus {
  provider: "linux" | "wsl-native" | "windows" | "none";
  configuredProvider: "auto" | "linux" | "windows";
  backendState:
    | "running"
    | "stopped"
    | "needs-login"
    | "unavailable"
    | "unknown";
  dnsName?: string; // 去掉末尾点
  httpsReady: boolean; // CertDomains 包含节点 DNS 名
  funnelAvailable: "available" | "unavailable" | "unknown";
  mode: "off" | "serve" | "funnel";
  port: number; // 0 表示自动
  source: "config" | "web"; // 当前设置来自 TOML 或网页
  storageAvailable: boolean; // 网页设置能否保存
  storageError?: string;
  canEnable: boolean; // 本进程能否开启 Serve/Funnel
  enableBlockedReason?: string; // 例如登录 token 不足 43 字符
  state: "off" | "starting" | "ready" | "failed";
  publicUrl?: string; // 仅 ready，不含 token
  blockedReason?: string; // 例如 WSL NAT、provider 未显式选择或监听地址不满足回环
  error?: string;
  entries: { host: string; port: number; public: boolean }[]; // 不含其他入口的 target
  wsl?: {
    networkingMode: "mirrored" | "nat" | "unknown";
    windowsDetected: boolean;
    windowsRunning: boolean;
    windowsDnsName?: string;
  };
}

interface TailscaleSettingsUpdate {
  mode: "off" | "serve" | "funnel";
  port: number; // 0 自动；Funnel 仅 0/443/8443/10000
  provider: "auto" | "linux" | "windows"; // windows 仅 WSL
  confirmFunnel?: boolean; // 从非 Funnel 切到 Funnel 时必须为 true
}
```

检测只执行 `tailscale status --json --peers=false`、`tailscale serve status --json` 及必要的 `funnel status --json`，每个 CLI 每次检测最多读取一次节点状态，短超时、无交互 stdin、缓存并合并并发请求；缺失字段按未知处理。响应永远不包含 WebMux token、其他 Serve 入口的代理 target 或原始 CLI 输出。

`state` 与命令回读一致：只有命令成功且回读确认 target、唯一 `/` handler 与 `AllowFunnel` 后才为 `ready`，失败时先撤销来源。托管入口记录在 `<state_dir>/tailscale.json`，网页设置保存在 `<state_dir>/tailscale-settings.json`（均为 0600、原子替换、独立锁），后者优先于 TOML 的 `mode`、`port`、`provider`；CLI 路径只来自 TOML，请求体中的未知字段返回 400。保存后先撤销来源、移除不再符合新设置的本实例入口，再应用新设置；移除失败时状态为 `failed` 且不应用新设置。

状态码：认证失败 401，Host/Origin 拒绝 403；设置无效或未确认 Funnel 400；登录 token 不足 43 字符、监听地址无法托管或服务正在退出 409；设置存储不可用或未提供控制器 503。本机 API 中来自 WebMux 会话内的进程访问 `/api/tailscale/` 下的写端点返回 403。错误使用 `{"error":"..."}`，与网络 API 的其他端点一致。

契约与实现：[线上字段](../../internal/contract/tailscale.go)、[状态转换](../../internal/wire/wire.go)、[HTTP 处理器](../../internal/api/tailscale.go)、[状态码映射](../../internal/api/errors.go)、[设置存储](../../internal/tailscalesettings/store.go)、[检测服务](../../internal/tailscale/)、[控制器与对账](../../cmd/webmux/tailscalectl.go)、[设置页](../../web/src/pages/TailscaleSettingsPage.svelte)、[Tailscale 指南](../tailscale.md)。
