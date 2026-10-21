## Why

WebMux 目前只建议绑定回环地址或可信 VPN；想临时从手机或外网访问时，用户需要自行部署 Tailscale/WireGuard 或反向代理，并手动输入 Token。Cloudflare Tunnel 无需开放入站端口即可提供 HTTPS 公网入口，配合强制 Token 与“带 Token 的访问链接 + 二维码”，可以把“安全地临时暴露并在另一台设备上打开”缩短为一次启动、一次扫码。

## What Changes

- **新增 Cloudflare Tunnel 接入**：
  - 新增 `[tunnel]` 配置节与 `--tunnel` 启动参数。
  - 默认使用 Quick Tunnel：无需账号，每次启动获得随机的 `*.trycloudflare.com` 地址。
  - 配置 `tunnel.token` 与 `tunnel.hostname` 后使用 Named Tunnel，获得固定域名。
  - webmux 以子进程方式运行 `cloudflared`，随服务启动与关闭。
- **自动获取 cloudflared**：
  - 查找顺序：配置的显式路径 → `PATH` → `state_dir` 下已校验的缓存。
  - 以上都没有时，下载 webmux 固定版本的 Cloudflare 官方 Release 产物：先 GitHub 直连，失败或停滞时依次回退到 gh-proxy 类镜像（默认 `ghfast.top`、`gh-proxy.com`，可用 `tunnel.download_mirrors` 覆盖），以解决国内无法直连 GitHub 的问题。
  - 无论来自哪个来源，内容都必须通过内置 SHA-256 校验后才会缓存并执行，镜像无法替换产物。
- **隧道模式强制鉴权**：
  - 启用隧道且未配置 `token` 时，每次启动生成高强度随机 Token；显式配置的 Token 必须至少 43 个字符，否则拒绝启动。
  - 非隧道模式行为不变：空 Token 仍表示不鉴权。
- **隧道公网地址加入白名单**：运行时把隧道公网 HTTPS 地址加入 Host 与 Origin 白名单，使经隧道访问的页面通过现有 Host/Origin 检查；其他未授权 Host 仍被拒绝。
- **URL 携带 Token 快速认证**：
  - 前端识别 `#token=<值>` 片段：保存 Token，并立即用 `history.replaceState` 从地址栏和当前历史记录中移除片段，然后完成认证。
  - WebSocket 以 `webmux.token.<base64url>` 子协议携带 Token。片段与 WebSocket 请求 URL 都不包含 Token，因此不会进入服务器、Cloudflare 边缘或代理的 URL/请求日志；前端也不把 Token 写入 Cookie（Cookie 不按端口隔离，同一主机其他端口的页面可读取）。
- **启动时打印访问地址**：
  - 所有模式都会在终端打印本地 URL；只有隧道模式附带 `#token=` 片段，避免固定的已配置 Token 进入服务日志。
  - 隧道模式在隧道就绪后打印公网 URL，并在终端以字符画形式打印对应二维码。
- **BREAKING**：WebSocket 不再接受 `?token=` 查询参数；自行拼接该参数的第三方客户端需改用 Bearer 头、`webmux_token` Cookie 或 Token 子协议。HTTP 鉴权方式（Bearer、Cookie）与非隧道模式的其他默认行为不变，本地链接不再附带固定 Token 片段。

## Capabilities

### New Capabilities

- `tunnel-exposure`：通过 Cloudflare Tunnel（Quick / Named）把 webmux 暴露到公网，覆盖以下行为：
  - cloudflared 的获取与校验
  - 子进程生命周期
  - 隧道模式的强制 Token
  - 公网地址白名单
  - 就绪后打印 URL 与二维码
- `access-link`：带 Token 的访问链接，覆盖以下行为：
  - 启动时打印本地访问 URL
  - 前端从 URL 片段读取 Token、持久化并清除片段的认证流程

### Modified Capabilities

（无：`openspec/specs/` 下尚无已归档的能力规格。）

## Impact

- **后端**：
  - `internal/config`：新增 `[tunnel]` 配置节及其校验。
  - `cmd/webmux/main.go`：处理 `--tunnel` 参数；生成随机 Token；打印访问地址；连接隧道生命周期。
  - 新增 `internal/tunnel` 包：获取、校验与下载 cloudflared；管理子进程；解析公网 URL。
  - `internal/api/host.go` 与 `router.go`：支持在运行时追加隧道公网来源。
- **前端**：
  - `web/src/App.svelte` 与 `web/src/api.ts`：在首次健康检查前消费 `#token=` 片段。
  - `web/src/ws.ts`：WebSocket 改用子协议认证，不再把 Token 拼进请求 URL。
- **依赖**：新增一个纯 Go 的二维码编码库，不使用 CGO。cloudflared 是运行时外部程序，不打包进 webmux 二进制。
- **网络与安全**：
  - 启用隧道且需要下载时，webmux 会访问 `github.com`，失败时访问所配置的第三方镜像（镜像能看到请求者 IP 与所下载的文件；`download_mirrors = []` 可关闭）；运行时还需访问 Cloudflare 边缘。
  - 隧道模式打印的 URL 与二维码包含 Token；以 systemd/launchd 运行时，它们会进入 journal 或日志文件。非隧道模式不打印固定 Token。
- **文档与部署示例**：`deploy/config.example.toml` 新增 `[tunnel]` 示例；`docs/deployment.md` 补充隧道用法与安全提示。
