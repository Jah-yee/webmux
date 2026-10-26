## Why

本变更对应 #144。WebMux 不内置 TLS。需要浏览器安全上下文（系统剪贴板、Service Worker、PWA、部分浏览器 API）时，用户只能在 WebMux 外配置 Cloudflare Tunnel、Caddy 或手工执行 `tailscale serve`。Tailscale 能自动签发浏览器信任的 HTTPS 证书：Serve 默认只在 tailnet 内可达，Funnel 可显式公开到互联网。当前 WebMux 只把 Tailscale 当作地址类型（100.64.0.0/10），没有检测、配置、生命周期或来源授权集成；用户必须自己执行命令、把 Origin 加入 `allowed_origins`，并自行处理端口、证书、权限和已有 serve 配置。

WSL 中的情况更特殊：按 Tailscale 官方建议，通常只在 Windows 主机运行 Tailscale，WSL 发行版没有 `tailscaled`；Windows 的 `tailscale serve/funnel` 只能代理 `127.0.0.1`，还要求 Windows 能访问 WSL 中的监听端口。直接照搬 Linux 的 CLI 检测或生命周期会误判“未安装/未登录”，甚至修改 Windows 上全局的 serve 配置。本变更把只读检测、托管 Serve、托管 Funnel 三个步骤写成可实现的规格，并把 WSL/Windows provider 作为一等场景。

## What Changes

- 只读检测：新增 `GET /api/tailscale` 与设置页 Tailscale 卡片/页面，展示 provider（Linux、WSL 内、Windows）、运行状态、节点 DNS 名、HTTPS 证书准备情况、已配置模式和端口、现有 serve/funnel 入口、公开 URL 与阻塞原因。检测只运行 `status`、`serve status`、`funnel status`，SHALL NOT 执行 `serve`、`funnel`、`set` 或 `reset`，也不自动修改配置。
- 配置驱动托管：新增 `[tailscale]`，`mode = "off" | "serve" | "funnel"`，`port` 默认 `0` 表示自动（Serve 可显式指定任意有效端口，Funnel 仅支持 443/8443/10000），`provider = "auto" | "linux" | "windows"`，可选 `command`。Serve 只在 tailnet 内提供 HTTPS，Funnel 必须显式开启且公开到互联网；两者都要求至少 43 字符的 WebMux Token（留空时按现有规则生成本次进程 Token）。
- Serve 生命周期：在监听开始后用 argv 执行 `tailscale serve --bg --https=<port> http://127.0.0.1:<实际端口>`；只移除自己创建的精确目标（`--https=<port> off`），绝不调用 `serve reset`；记录所有权并在启动时对账，进程崩溃或 WSL 关闭后留下的入口在下次启动时修正。Tailscale 的 `--bg` 配置属于 tailscaled，不是 WebMux 的子进程，退出时不能按 cloudflared 的方式处理。
- Funnel 生命周期：复用同一套所有权与对账，额外校验 Funnel 端口、HTTPS 证书、节点 `funnel` 属性和公开风险；从 Serve 切到 Funnel 或反向切换时先关闭旧目标的精确端口，再应用新目标，避免旧端口继续公开。失败时撤销来源并确保不残留本次创建的公开入口。
- 来源授权：把单值的 `api.PublicOrigin` 扩展为可按来源注册多个 HTTPS origin 的注册表，支持 443 的无端口 authority 与任意非 443 端口的带端口 authority；Host 必须精确匹配 authority，Origin 必须精确匹配对应 HTTPS origin。Tailscale 托管来源与 Cloudflare Named/Quick 可并存，互不清除；托管模式下无需手工写 `allowed_origins`，手工部署仍支持。
- WSL 特殊处理：检测 WSL 与 `wslinfo --networking-mode`；Windows 上的 Tailscale 必须通过显式 `provider = "windows"` 才允许修改，`auto` 只检测不代管 Windows；托管要求 mirrored 网络模式，NAT 时失败关闭并给出 mirrored、WSL 内安装 Tailscale 或托盘转发三种替代；Windows 节点 DNS 名用于 URL/Host/Origin，权限不足时提示在 Windows 管理员终端设置 operator，不尝试 `sudo` 或自动提权。Windows 的 serve 状态跨 WSL 重启保留，按状态文件对账。
- 安全与失败关闭：`[tailscale]` 非法值在启动时失败；运行期缺少 CLI、未登录、证书未就绪、端口冲突、权限不足或 Funnel 前置条件不满足时只把 Tailscale 状态标为 `failed`，本地 HTTP、已有会话和其他服务继续运行。唯一的例外是本实例遗留入口无法清理：此时 MUST 拒绝启动，避免旧入口在新进程开始监听后继续暴露。不自动重试，不自动 sudo，不修改 `.wslconfig` 或 Windows 防火墙，不调用任何 `reset`。
- 网页设置：`/settings/tailscale` 可修改 `mode`、`port`、`provider` 并立即生效，持久化后优先于 TOML；开启 Funnel 需要风险确认，`command` 仍只由 TOML 管理。
- 不在本次范围：自签名 TLS；Tailscale 作为推送来源（当前仍按 Cloudflare Named Tunnel 与回环判定，后续变更单独设计并同步托盘说明）；PTY、回放、会话生命周期、Cloudflare Tunnel 现有行为。

## Capabilities

### New Capabilities

- `tailscale-exposure`：Tailscale 运行环境检测、`[tailscale]` 配置与模式、托管 Serve/Funnel、WSL/Windows provider、端口感知的 Host/Origin 授权、所有权对账与失败关闭、设置页状态展示。

### Modified Capabilities

- 无。本变更只新增能力；推送来源、Cloudflare 托盘说明和自签名保持现状。

## Impact

- 后端：新增 `internal/tailscale` Service（CLI 检测、状态解析、Serve/Funnel 应用与精确移除、WSL/Windows provider）；`cmd/webmux` 新增 tailscale 控制器与启动装配；`internal/config` 增加 `[tailscale]` 与校验；`internal/contract` 增加 `TailscaleStatus` 等线上类型；`internal/api` 增加 `GET /api/tailscale`、`PublicOrigins` 与端口感知的 Host/Origin 判定；`internal/wire` 转换；`internal/archtest/layers_test.go` 登记新包。
- 前端：`web/src/pages/Settings.svelte` 增加 Tailscale 卡片；新增 `/settings/tailscale` 页面与组件；`web/src/App.svelte` 路由；`web/src/schemas.ts`、`web/src/api.ts` 与浏览器测试。
- 状态：新增 `<state_dir>/tailscale.json`（0600、版本化、原子替换）与独立锁；记录 provider、节点 DNS 名、端口、模式、target 与所有权；损坏或锁冲突时失败关闭，不删除未知入口。
- 配置：`deploy/config.example.toml` 增加 `[tailscale]` 注释示例；`README.md` 的状态目录与设计说明在实现阶段按实际需要同步。
- 文档：新增 `docs/tailscale.md`；更新 `docs/deployment.md` 的 HTTPS、手机访问与 WSL 段落，`docs/cloudflare-tunnel.md` 的入口对比，`docs/protocol.md` 的新端点，`SECURITY.md` 的公网 shell 风险，`docs/architecture.md` 的包表与装配说明，以及 `docs/user-stories.md`（新增 Tailscale 故事组，仅在实现阶段）。
- 测试：Go 单元、控制器、API 与架构测试使用 fake `tailscale`/`tailscale.exe`/`wslinfo`；前端单元与浏览器测试；E2E 不连接真实 Tailscale；真实 Tailscale Serve/Funnel 与 WSL Windows 环境检查作为手工验收。
- 不涉及：自签名证书、Web Push 来源扩展、Cloudflare 现有设置页与运行时开关、会话/PTY/回放、Windows 托盘自动修改 Tailscale 配置。
