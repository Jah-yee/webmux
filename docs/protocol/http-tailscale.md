# HTTP API：Tailscale 状态

`GET /api/tailscale` 返回只读的 Tailscale 检测与运行状态，需要认证并通过 Host/Origin 检查；没有写方法，配置由 `[tailscale]` TOML 与重启管理。`?refresh=1` 绕过检测缓存，只影响检测，不触发写命令。

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
```

检测只执行 `tailscale status --json`、`tailscale serve status --json` 及必要的 `funnel status --json`，短超时、无交互 stdin、缓存并合并并发请求；缺失字段按未知处理。响应永远不包含 WebMux token、其他 Serve 入口的代理 target 或原始 CLI 输出。

`state` 与命令回读一致：只有命令成功且回读确认 target、唯一 `/` handler 与 `AllowFunnel` 后才为 `ready`，失败时先撤销来源。托管入口记录在 `<state_dir>/tailscale.json`（0600、原子替换、独立锁）。

状态码：认证失败 401，Host/Origin 拒绝 403，未提供控制器 503，成功 200。请求错误使用 `{"error":"..."}`，与网络 API 的其他端点一致。

契约与实现：[线上字段](../../internal/contract/tailscale.go)、[状态转换](../../internal/wire/wire.go)、[HTTP 处理器](../../internal/api/tailscale.go)、[检测服务](../../internal/tailscale/)、[控制器与对账](../../cmd/webmux/tailscalectl.go)、[设置页](../../web/src/pages/TailscaleSettingsPage.svelte)、[Tailscale 指南](../tailscale.md)。
