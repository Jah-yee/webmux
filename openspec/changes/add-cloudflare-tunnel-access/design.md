## Context

现状（均已在代码中确认）：

- **鉴权**：`internal/api/router.go` 的 `authorized` 支持三种 Token 来源，按顺序检查：
  - `Authorization: Bearer`
  - `webmux_token` Cookie
  - 仅限 `/ws/` 路径的 `?token=` 查询参数（本变更移除）

  Token 为空时不鉴权。前端把 Token 存在 `localStorage["webmux.token"]`（`api.ts` 的 `store`/`stored`），HTTP 请求用 Bearer 头，WebSocket 用 `?token=`（`ws.ts`）。

- **Host 检查**：`internal/api/host.go` 的 `hostCheck` 在 `api.New` 时根据 `Listen` 与 `AllowedOrigins` 构建一次性白名单。
- **Origin 检查**：`originAllowed` 只在 Origin 与 `r.Host` 相同、且协议与 `r.TLS` 推断的协议一致时放行，否则必须精确匹配 `AllowedOrigins`。
- **启动流程**：`cmd/webmux/main.go` 依次执行以下步骤；目前只通过 slog 打印监听地址，不打印可点击的 URL。
  1. `loadConfig`：合并 TOML 与 `-config/-listen/-token` 参数
  2. `Validate`
  3. `net.Listen`
  4. `api.New`
  5. `server.Serve`
- **依赖**：`go.mod` 无二维码库；发布构建为 `CGO_ENABLED=0`。
- **路由片段**：前端唯一使用的路由片段是 `/settings#quick-commands`。

cloudflared 经隧道把请求转发到本地 origin 时：

- 连接是明文 HTTP，`r.TLS == nil`。
- 浏览器 Origin 是 `https://<公网主机>`。
- Host 头保持公网主机名，Origin 原样透传（已用 cloudflared 2026.10.2 的真实 Quick Tunnel 验证，见 Risks）。

因此不改动的话，经隧道的请求会同时被 Host 检查和 Origin 检查拒绝。

## Goals / Non-Goals

**Goals:**

- 一条 `webmux --tunnel` 命令即可得到受 Token 保护的 HTTPS 公网入口，以及可扫码打开的链接。
- Token 不出现在请求 URL（含 WebSocket）或前端写入的 Cookie 中，因此不进入服务器、Cloudflare 边缘或代理的 URL/请求日志，也不会被同一主机其他端口的页面读取；`Authorization` 头、WebSocket 子协议头与终端输入仍会经过 Cloudflare 边缘。
- 不启用隧道时不联网、不改 HTTP 鉴权语义；本地链接不再附带固定的 Token 片段，WebSocket 改用子协议传递 Token。

**Non-Goals:**

- 不集成 Cloudflare Access/Zero Trust 策略配置。Named Tunnel 用户可以在 Cloudflare 侧自行叠加。
- 不通过 API 创建或管理 Named Tunnel、DNS 记录。用户在 Cloudflare 控制台创建隧道并配置公共主机名指向本机 webmux 端口，webmux 只负责运行它。
- 不自动重启意外退出的 cloudflared。Quick Tunnel 重启会换新地址，自动重启的意义有限。
- 不持久化自动生成的 Token：会话本身也不跨重启。
- 不在网页 UI 中展示二维码或公网地址。
- 不自动更新 cloudflared：固定版本，随 webmux 发版升级。

## Decisions

### 1. 配置形态：`[tunnel]` 节 + `--tunnel` 开关，模式由 `token` 推断

```toml
[tunnel]
enabled = false
# 留空 = Quick Tunnel；填写 = Named Tunnel（需要 hostname）
token = ""
hostname = ""          # Named Tunnel 的公共主机名，如 term.example.com
cloudflared = ""       # 可选：显式 cloudflared 路径
# 可选：覆盖内置下载镜像前缀；[] 表示只用 GitHub 直连
# download_mirrors = ["https://ghfast.top", "https://gh-proxy.com"]
```

- `config.Config` 新增 `Tunnel TunnelConfig` 字段。
- `Validate` 校验以下内容：
  - Named Tunnel 必须配置 `hostname`，且它是纯主机名：不含协议、端口、路径、`@`，并通过 `authorityHostname` 同等解析。
  - `hostname` 必须与 Cloudflare 转发的 `Host` 一致：只接受 ASCII/punycode 形式，拒绝通配符与 IDN；末尾的点会被去掉。否则打印出的公网地址永远匹配不上 Host 检查。
  - `hostname` 统一转为小写。
- `--tunnel` 为布尔参数，显式出现时覆盖 `tunnel.enabled`，沿用 `loadConfig` 的 `explicit` 机制。
- 不设单独的 `mode` 字段：模式由 `token` 是否为空推断，避免出现 mode 与 token 矛盾的配置组合。
- **备选：仅命令行参数。** 不采用。Named Tunnel 凭据不适合放在命令行，会被 ps 看到。

### 2. 以子进程运行 cloudflared，新增 `internal/tunnel` 包

**备选：嵌入 cloudflared Go 库。** 不采用。它的依赖树巨大，会显著增大发布二进制，而且 cloudflared 未承诺稳定的库 API。

`tunnel.Start(ctx, opts)` 返回一个句柄，提供以下内容：

- `Ready() <-chan string`：发送公网 origin，例如 `https://abc.trycloudflare.com`。
- `Done() <-chan struct{}`：子进程退出时关闭；`Err()` 返回退出原因（`Close` 主动终止时为 nil）。
- `Close()`：发送 SIGTERM 并等待进程与日志读取 goroutine 结束。

启动参数（argv，不经过 shell）：

- **Quick**：`cloudflared tunnel --no-autoupdate --config /dev/null --url http://<本地地址>`。
- **Named**：`cloudflared tunnel --no-autoupdate --config /dev/null run`。凭据通过环境变量 `TUNNEL_TOKEN` 传入，不放在 argv 中；Quick 模式会从继承的环境中去掉 `TUNNEL_TOKEN`。
- `--config /dev/null` 屏蔽 `~/.cloudflared/config.yml`（见 Risks）。cloudflared 会记录一行“Configuration file /dev/null was empty”，只出现在 debug 日志中，不影响运行。

本地地址取实际监听地址，规则如下：

- 通配主机替换为 `127.0.0.1`。
- IPv6 主机加方括号。
- 端口取 `listener.Addr()` 的实际值，以支持 `:0`。

**就绪判定与地址获取**：逐行扫描子进程 stderr。

- **Quick**：只匹配横幅框内的地址 `\|\s+(https://[a-z0-9-]+\.trycloudflare\.com)\s+\|`，避免把错误信息中的 `https://api.trycloudflare.com` 误认为公网地址。
- **两种模式**：都等待出现 `Registered tunnel connection` 行后才视为就绪。
- **Named**：公网 origin 取自配置的 `https://<hostname>`。
- **超时**：60 秒仍未就绪时，记录错误日志，此时不打印公网 URL，本地服务继续运行。超时只报警：之后若才出现注册行，仍按就绪处理并打印地址，避免留下一个子进程仍在运行、公网 Host 却被永久拒绝的状态。
- **日志转发**：stderr 原样转发到 slog debug 级别。cloudflared 的日志不包含 webmux Token：Token 不进入请求 URL，WebSocket 凭据由子协议头携带。
- **备选：`--metrics` 的 `/quicktunnel` 与 `/ready` 端点。** 它是结构化接口，但需要先预分配 metrics 端口（存在 TOCTOU 竞争）或额外解析日志。复杂度更高，暂不采用；如果日志格式不稳定，可以退回此方案。

**生命周期**：

- 使用 `exec.CommandContext`，`Cancel` 发送 SIGTERM，`WaitDelay` 设为 5 秒，之后强制 kill。
- Linux 上设置 `Pdeathsig: SIGTERM`：webmux 被 SIGKILL 或 OOM 终止、来不及走关闭流程时，内核也会结束 cloudflared，隧道不会比服务活得更久。父进程死亡信号绑定创建子进程的线程，因此启动、扫描与 `Wait` 在同一个 `LockOSThread` 的 goroutine 中完成。macOS 没有等价机制，依赖 launchd 结束作业的全部进程。
- 由 main 的根 ctx 驱动；关闭顺序为：先停止接收信号，再终止隧道，最后 `m.Shutdown`。
- 句柄的 `Close` 会等待扫描 goroutine 结束，与“汇合连接 goroutine”的约定一致。
- 子进程意外退出时，记录日志并撤销公网 origin，见第 4 节；webmux 主进程继续运行。

**失败语义**：

- **启动前失败**：获取 cloudflared 失败、配置非法时，返回错误，webmux 以非零状态退出。用户明确要求了隧道，不应静默降级。
- **启动后失败**：子进程异常、超时时，只记录日志，不影响本地会话。

### 3. cloudflared 获取：PATH 优先，固定版本 + 内置 SHA-256，GitHub 直连后回退到镜像

按顺序查找：

1. `tunnel.cloudflared`：显式路径，失败即报错，不再继续查找。
2. `exec.LookPath("cloudflared")`。
3. 缓存 `<state_dir>/cloudflared/<版本>/cloudflared`：每次启动都重新计算 SHA-256 校验。
4. 下载。

下载规则：

- **来源顺序**：先 GitHub 直连 `https://github.com/cloudflare/cloudflared/releases/download/<版本>/<资产>`，失败后依次使用镜像，地址为 `<前缀>/https://github.com/cloudflare/cloudflared/releases/download/<版本>/<资产>`（gh-proxy 系镜像通用的前缀形式）。
  - 内置默认镜像：`https://ghfast.top`、`https://gh-proxy.com`。2026-10-20 从国内直连实测（不经代理）下载固定版本 `cloudflared-linux-amd64`：ghfast.top 约 1.8 MB/s、gh-proxy.com 约 2.5 MB/s，SHA-256 均一致；`ghproxy.net` 前缀形式可用但只有约 10 KB/s，40 MB 需要一小时以上，且该速度不会触发空闲超时，所以不放入默认列表。`ghproxy.com`、`mirror.ghproxy.com` 已失效，不采用。
  - `tunnel.download_mirrors` 非 nil 时完整替换默认列表，`[]` 表示只用直连。TOML 解码后需区分“未配置”（nil）与“空数组”。
  - `Validate` 校验每个前缀：必须是 `https://`，不含凭据、查询参数或片段；末尾的 `/` 统一去掉。
- **按来源快速失败**：每个来源使用独立的请求 ctx，满足以下任一条件即放弃，转到下一来源：
  - 拨号加响应头超过 10 秒（`Transport.ResponseHeaderTimeout` 与拨号超时）
  - 读取空闲 20 秒（每次 `Read` 返回数据就重置计时器，超时则取消 ctx）
  - 状态码不是 2xx（重定向由 `http.Client` 跟随；GitHub 本身会 302 到 `objects.githubusercontent.com`）
  - 内容超出大小上限，或 SHA-256 不符

  不设单个来源的总时长上限，以免在慢速但仍在推进的链路上误判失败；整个下载过程仍受 webmux 根 ctx 控制（Ctrl-C 可中断）。

- **校验是信任边界**：任何来源的内容都必须先通过内置 SHA-256 校验，才会写入缓存并执行。因此镜像只影响可用性，无法替换产物。
- **日志**：每个来源失败都用 slog 以 warn 级别记录来源主机与原因；成功时记录来源主机。全部失败时，返回的错误汇总每个来源的原因，并提示可配置 `tunnel.cloudflared` 或手动安装。
- **代码内置**：固定版本号，以及每个平台的资产名和 SHA-256，覆盖 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`。
  - Linux 资产是裸二进制。
  - macOS 资产是 `.tgz`，用标准库 `archive/tar` + `compress/gzip` 解出 `cloudflared`。
  - **校验对象**：Linux 校验二进制本身，macOS 校验下载的 `.tgz` 归档。release notes 中 darwin 的校验值对应解包后的二进制，与资产摘要不同，所以实现时以 GitHub API 资产的 `digest` 字段为准。
- **HTTP 客户端**：基于 `http.DefaultTransport` 克隆，保留 `HTTPS_PROXY` 支持，只额外设置响应头超时；响应体上限 100 MiB。
- **写入方式**：先写到同目录下的临时文件；校验通过后 `chmod 0755` 并 `rename`，保证原子替换；校验失败则删除临时文件。
- **并发**：同一 `state_dir` 不允许多实例并发（workspace registry 已有同样约束）。所以下载不加文件锁，原子 rename 已能避免写入半截文件。

**备选：要求用户自行安装。** 用户选择了自动下载。PATH 优先保留了自管理的路径。

**备选：每次下载最新版本。** 不采用。无法内置校验值，存在供应链风险。

**备选：镜像优先，或并发竞速。** 不采用。海外用户本可直连，镜像优先会让他们无谓地经过第三方；竞速会同时向多个第三方发请求，实现也更复杂。直连快速失败只会在国内首次下载时多等 10–20 秒，下载结果之后会缓存。

### 4. 公网 origin 在运行时进入 Host/Origin 白名单

- `internal/api` 新增并发安全的 `PublicOrigin`：内部用 `atomic.Pointer[url.URL]` 保存，提供 `Set(origin)`、`Clear()` 与只读判断方法。
- `api.New` 新增显式参数 `public *PublicOrigin`，nil 表示无隧道。main 负责构造并注入，测试传 nil。这符合“构造函数显式注入依赖”的约定。
- 判定规则：
  - `hostCheck` 返回的闭包在静态白名单之外，再检查 `public` 当前主机名。
  - `originAllowed` 额外放行一种情况：Origin 精确等于当前公网 origin（`https://<host>`），且 `r.Host` 的主机名等于公网主机名。这样只为隧道主机放宽“协议必须与 `r.TLS` 一致”的限制，不影响直连路径。
- `Set` 时机：隧道就绪时。`Clear` 时机：子进程退出时。
- **备选：用 cloudflared 的 `--http-host-header` 改写 Host 为本地地址。** 不采用。改写后 Origin 仍是 https 公网地址，Origin 检查照样会失败，还会让 `originAllowed` 的同源判断失真。
- **备选：启动时写入 `AllowedOrigins`。** 不采用。Quick Tunnel 的地址启动后才知道，而 `Config` 是值类型、构造后只读。

### 5. 隧道模式自动生成 Token

- **生成时机**：在 `loadConfig` 之后、`api.New` 之前。若 `Tunnel.Enabled && Token == ""`，生成 Token 并写入 `c.Token`。
- **生成方式**：`crypto/rand` 32 字节，`base64.RawURLEncoding` 编码，得到 43 个字符。URL 安全，无需百分号编码。
- **已配置的 webmux Token**：隧道模式下也必须达到同样的 43 字符宽度，否则拒绝启动并提示留空自动生成，或使用 `openssl rand -base64 32`。隧道是公网入口，不能沿用只适合受信网络的短 Token。
- 生成后的 Token 不写回配置文件。
- 只在隧道模式下生成：非隧道模式保持“空 Token = 不鉴权”，符合用户的选择。

### 6. 访问 URL 与二维码打印

- **新增函数**：`accessURL(base, token string) string`。Token 非空时追加 `#token=` + `url.QueryEscape(token)`，再把 `+` 替换为 `%20`。`PathEscape` 不转义 `&`，不满足规格；替换后空格编码为 `%20`、`+` 为 `%2B`，前端用 `decodeURIComponent` 可以无歧义还原。
- **本地 URL**：`net.Listen` 成功后立即打印。主机规则同第 2 节的本地地址。只有隧道模式附带 `#token=`：非隧道模式下固定 Token 不应因为 stdout 被 service manager 捕获而进入 journal 或日志文件，用户本来也知道自己配置的 Token。
- **公网 URL**：隧道就绪时打印 URL，紧跟二维码。
- **输出方式**：直接 `fmt.Fprintln(os.Stdout, …)`，不走 slog。避免 Token 混入结构化日志的字段，也便于用户直接点击和复制。slog 的“webmux listening”日志保持不变，且不包含 Token。
- **二维码库**：使用 `rsc.io/qr`，纯 Go、BSD 许可、无传递依赖、支持 `CGO_ENABLED=0`。
  - 纠错级别 L：URL 约 90 字节，可得到较小的码。
  - 用 Unicode 半块字符 `▀ ▄ █` 和空格，每两行模块合并为一行字符。
  - 四周留 4 模块静区（QR 规范要求的最小值）。
  - 亮模块画成实心块，适配常见深色终端背景。
- **备选：`github.com/mdp/qrterminal`。** 不采用。它额外依赖另一个编码库，而渲染逻辑只需二十余行。

### 7. 前端消费 `#token=` 片段

- **新增函数**：在 `api.ts` 新增 `consumeTokenFragment(): boolean`，在 `App.svelte` 的 `onMount` 中、首次 `check()` 之前调用。逻辑如下：
  1. 只处理形如 `#token=<值>` 的片段，其他片段（如 `#quick-commands`）原样保留。
  2. 用 `decodeURIComponent` 解码；解码失败视为无效，并把片段移除。
  3. `store("token", value)`，同时更新组件内的 `token` 状态。
  4. `history.replaceState(history.state, "", location.pathname + location.search)`。
- HTTP 请求继续使用 `Authorization: Bearer`。WebSocket 握手无法设置请求头，前端在每次 `connect()` 时从 `stored("token")` 计算子协议列表 `["webmux", "webmux.token." + base64url(token)]`，覆盖手动输入 Token 的情形；服务端只选择 `webmux`，不回显 Token。`ws.ts` 不再拼接 `?token=`，服务端也不再接受 WebSocket 的 query Token。
- Token 无效时走现有的 401 → `webmux:auth` → 表单流程。
- **备选：前端把 Token 写入 `webmux_token` Cookie。** 不采用。Cookie 按主机而非端口隔离：`127.0.0.1` 上任意端口的其他页面（开发服务器、Jupyter 等）都能通过 `document.cookie` 读到 Token，并在请求中把它发给这些服务；同机多个 webmux 实例还会互相覆盖。`localStorage` 按完整 origin（含端口）隔离，子协议头只发往本 origin。
- **备选：先用 Bearer 换取一次性 WebSocket 票据再放进 URL。** 不采用。每次连接与重连都要多一次异步请求，`connect()` 的同步语义与现有测试都要重写；子协议头同样不进入 URL，且无需服务端状态。
- **备选：用 `?token=` 换取 HttpOnly Cookie 后 302。** 用户选择了片段方案；HttpOnly Cookie 同样不按端口隔离。

## Risks / Trade-offs

- **[风险] Token 会落入 journal 或日志文件**：隧道模式的公网 URL 与二维码打印到 stdout，以 systemd/launchd 运行时 Token 会持久化到 journal 或 `StandardOutPath`。
  - **缓解**：只有隧道模式打印 Token 片段；非隧道模式不打印固定的已配置 Token。部署文档说明日志风险，并建议 journal 访问权限只限本用户；自动生成的 Token 每次重启都会轮换。
- **[风险] cloudflared 日志格式变化**：Quick Tunnel 地址的日志格式或 `Registered tunnel connection` 文案可能变化，导致就绪判定失败。
  - **已验证（2026.10.2）**：地址出现在横幅框行 `INF |  https://<名称>.trycloudflare.com  |`，约 4 秒后出现 `INF Registered tunnel connection connIndex=0 ...`。新主机名的 DNS 可能还要几秒才能解析。
  - **缓解**：固定 cloudflared 版本，升级时用真实隧道回归；超时只影响打印，不影响本地服务；必要时可切换到 metrics 端点方案。
- **[已验证] 用户已有 cloudflared 配置会干扰 Quick Tunnel**：在临时 HOME 中放入带 ingress（兜底为 `http_status:404`）的 `~/.cloudflared/config.yml` 后，Quick Tunnel 仍能注册，但所有请求都返回 404，origin 收不到请求。
  - **处理**：两种模式都传 `--config /dev/null`；实测请求随即正常到达 origin。没有采用 `state_dir` 下的空配置文件，因为 `state_dir` 可能不可写。
- **[已验证] Host 头保留**：经 Quick Tunnel 到达 origin 的请求 Host 为公网主机名，Origin 原样透传，并带有 `X-Forwarded-Proto: https`。第 4 节的判定成立。
- **[风险] 固定版本过时**：固定的 cloudflared 版本会过时，或被 Cloudflare 弃用（Cloudflare 会拒绝过旧客户端）。
  - **缓解**：PATH 优先，用户可自行安装新版；每次发版检查并升级固定版本与校验值。
- **[取舍] 下载来源**：自动下载依赖 GitHub 或第三方镜像的可用性，并信任 Cloudflare 发布的产物；内置 SHA-256 能防住镜像篡改、传输篡改和缓存篡改，但不能防上游被攻破。
- **[风险] 公益镜像会频繁失效**：gh-proxy 类镜像都是公益服务，随时可能失效、限速或被墙。
  - **缓解**：可以通过 `tunnel.download_mirrors` 自行替换，无需等待发版；全部失败时报错信息会给出手动安装路径；每次发版顺带复查默认列表。
- **[风险] 国内到 Cloudflare 边缘的连通性**：下载问题解决后，cloudflared 在国内连接 Cloudflare 边缘时仍可能不稳定，例如 QUIC 被干扰；`trycloudflare.com` 在部分网络下也可能无法访问。
  - **缓解**：cloudflared 默认会从 QUIC 回退到 HTTP/2；本变更不处理边缘连通性，需要时用户可改用 Named Tunnel 自定义域名，或使用 VPN 方案。
- **[取舍] Quick Tunnel 的限制**：Quick Tunnel 没有 SLA，有并发请求上限，地址每次重启都会变化。适合临时访问，长期使用应选择 Named Tunnel。
- **[取舍] 二维码颜色**：亮模块按深色背景绘制，浅色背景终端上二维码是反色的。主流扫码器能识别反色码，所以不提供配置开关。
- **[风险] 片段 Token 可能残留在浏览器中**：浏览器可能把带片段的原始 URL 记入地址栏自动补全或同步历史。
  - **缓解**：已用 `replaceState` 清除当前历史项；这是片段方案的固有限制，在文档中说明。

## Migration Plan

- 不配置 `[tunnel]` 且不带 `--tunnel` 时，有两处安全修正：本地链接不再附带固定的 `#token=` 片段；WebSocket 不再接受 `?token=` 查询参数。
- **BREAKING**：自行拼接 `/ws/...?token=` 的第三方客户端需要改用 `Authorization: Bearer` 头、`webmux_token` Cookie 或 `webmux.token.<base64url>` 子协议。内置前端随同一二进制发布，无需迁移。
- 回滚：去掉 `--tunnel` 或把 `tunnel.enabled` 设为 `false`。可选择删除 `<state_dir>/cloudflared/` 缓存。
