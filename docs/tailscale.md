# Tailscale Serve / Funnel 入口

WebMux 不内置 TLS。Tailscale 可以为节点自动签发浏览器信任的 HTTPS 证书：
Serve 只在 tailnet 内可达，Funnel 会把入口公开到互联网。WebMux 可以检测
Tailscale 状态，并按网页设置或 `[tailscale]` 配置托管一个精确的 Serve/Funnel
入口。

## 启用前

1. 安装并登录 Tailscale；确认 `tailscale status --json` 的 `BackendState` 为
   `Running`。
2. 在 Tailscale 管理台为节点启用 HTTPS 与 MagicDNS。`CertDomains` 必须包含
   节点 DNS 名，否则 WebMux 不会执行会等待证书的命令。
3. 使用 Funnel 时，还要为节点启用 `funnel` 属性；Funnel 是 beta，仅支持
   443、8443 与 10000 端口。
4. WebMux 的 `listen` 必须覆盖 IPv4 回环，推荐 `127.0.0.1:7681` 或
   `0.0.0.0:7681`。`[::1]` 或只绑定特定网卡地址时，托管会失败并提示修改。

## 网页设置

推荐在“设置 → Tailscale”中选择模式（关闭、Serve、Funnel）、端口与
provider，点击“保存并应用”。保存立即生效、无需重启，也不结束终端会话：
WebMux 先撤销旧来源、移除不再符合新设置的本实例入口，再应用新设置；页面在
应用期间显示“启动中”。

- 网页设置保存在 `<state_dir>/tailscale-settings.json`（0600、原子替换、
  独立锁），重启后继续生效，并优先于 TOML 的 `mode`、`port`、`provider`。
  文件损坏、不可读或被另一实例锁住时，网页设置不可写，托管入口保持关闭。
- 开启 Funnel 前会再次确认公网风险。登录 token 少于 43 个字符、或 `listen`
  无法托管时，服务端拒绝开启并在页面说明原因。
- 当前正通过该 Tailscale 地址访问时，替换入口前会提示页面可能断开。
- “恢复配置文件设置”删除网页设置并关闭当前入口；下次启动按 TOML 决定。
- CLI 路径 `command` 只能在 TOML 中设置。WebMux 会话内的进程不能通过本机
  API 修改 Tailscale 设置。

## 配置文件

```toml
[tailscale]
mode = "serve" # off | serve | funnel
port = 0 # 0 = 自动选择 443、8443、10000
provider = "auto" # auto | linux | windows
# command = "/usr/bin/tailscale"
```

- `mode = "off"` 是默认值，不创建入口，也不创建 `tailscale-ownership.json`。
- `mode != "off"` 视为网络入口：留空 token 时生成本次进程的 256 位 token；
  显式 token 少于 43 个字符时拒绝启动。
- 手工修改 TOML 后需要重启 WebMux；已有网页设置时，先“恢复配置文件设置”才会
  重新采用 TOML 值。
- 自动端口会按 443、8443、10000 选择第一个没有其他入口的端口；显式端口冲突
  直接失败，不会静默换端口。

## 托管命令与所有权

WebMux 使用 argv 执行，不经过 shell：

```sh
tailscale serve  --bg --https=<port> http://127.0.0.1:<实际监听端口>
tailscale funnel --bg --https=<port> http://127.0.0.1:<实际监听端口>
```

命令成功不代表生效。WebMux 会回读 `tailscale serve status --json`，确认目标
端口只有指向当前 target 的 `/` handler；Funnel 还要求
`AllowFunnel["<dns>:<port>"] == true`。确认后才登记 Host/Origin 来源。

`<state_dir>/tailscale.json`（0600、原子替换、独立锁）记录 provider、节点
DNS、模式、端口、target 与 `pending`/`active` 状态，是唯一的所有权依据：

- 启动时先对账，再绑定监听；应关闭或已变化的旧入口在监听前移除。
- 记录残缺或无法清理、且旧入口仍可能指向本进程时，WebMux 拒绝启动。无法读取
  Tailscale（例如已卸载）时，错误会给出状态文件路径；确认入口已不存在后删除
  该文件即可启动。
- 记录的端口已没有入口或改指其他 target 时，记录视为过期：WebMux 不改动该
  入口，删除记录后继续启动。
- 停止、失败或切换模式时先撤销来源，再只移除记录中的精确端口。
- 绝不调用 `tailscale serve reset` 或 `tailscale funnel reset`，它们会清除
  其他应用的入口。

## 手工移除与排错

精确关闭 WebMux 记录的端口：

```sh
tailscale serve --https=<port> off
```

`serve off` 同时移除该端口的 handler 与 Funnel 标记；重复执行返回的
`handler does not exist` 可视为已移除。不要先执行 `funnel off`，它会让随后的
`serve off` 报错。

常见原因：

| 现象             | 处理                                                  |
| ---------------- | ----------------------------------------------------- |
| HTTPS 未就绪     | 在 Tailscale 管理台启用 HTTPS，刷新检测后重新保存     |
| 缺少 funnel 属性 | 在管理台为节点启用 Funnel，刷新检测后重新保存         |
| 权限不足         | Linux/macOS 执行一次 `tailscale set --operator=$USER` |
| 端口冲突         | 换一个端口，或先手工移除冲突入口                      |
| 入口指向死端口   | 重启 WebMux 触发对账，或按上面的命令手工移除          |

`tailscale serve --bg` 的配置属于 tailscaled，不是 WebMux 的子进程。WebMux
崩溃或 WSL 关闭后入口会保留，短暂返回 502；下次启动对账是主要修复路径。

## WSL 与 Windows 主机

WSL 中通常只在 Windows 主机运行 Tailscale。WebMux 会区分三种 provider：

- `linux`：Linux/macOS 或 WSL 内原生 Tailscale。
- `wsl-native`：WSL 内的原生 Tailscale，对应配置值 `linux`。
- `windows`：Windows 主机上的 `tailscale.exe`。

`provider = "auto"` 只检测并优先使用正在运行的 WSL 原生 Tailscale，**不会**
代管 Windows Tailscale。要修改 Windows 的全局 Serve/Funnel 配置，必须显式
设置：

```toml
[tailscale]
mode = "serve"
provider = "windows"
```

并满足：

- WSL 网络模式为 `mirrored`。NAT 模式下 Windows 回环到 WSL 不保证可达，
  WebMux 会失败关闭，不执行任何 Windows 写命令。可改为在
  `%USERPROFILE%\.wslconfig` 的 `[wsl2]` 设置 `networkingMode=mirrored` 后
  执行 `wsl --shutdown`（会结束所有会话）；或在 WSL 内安装 Tailscale 并使用
  `provider = "linux"`；或继续使用 Windows 托盘转发并手工配置 Serve。
  WebMux 不会自动修改 `.wslconfig`、重启 WSL 或修改防火墙。
- URL、Host 与 Origin 使用 Windows 节点的 `Self.DNSName`，不是 WSL 主机名。
- 权限不足时，在 Windows 管理员终端执行一次
  `tailscale set --operator=<Windows 用户>`；WebMux 不会 `sudo` 或自动提权。
- Windows 的 `--bg` 入口跨 WSL 关闭/重启保留；重新启动后按状态文件对账，
  多个发行版共用端口冲突时失败关闭。

## 安全边界

- Funnel 必须由显式配置开启，不会因为检测到 Tailscale 就自动公开。
- `GET /api/tailscale` 只返回脱敏状态，不包含 token、其他入口的 target 或
  原始 CLI 输出。
- 公网入口仍要求 WebMux token；Funnel 链接与 token 一旦泄露，等同于命令执行
  权限。
- 本变更不把 Tailscale 加入 Web Push 来源；推送仍只接受 Cloudflare Named
  Tunnel 与回环地址。
