## Context

动机见 proposal.md。本变更建立在 `add-cli-server-control`（PR #83）之上，客户端/服务端分工与通讯方案见该变更 design.md 决策 0；以下“现状”包含该前置变更提供的能力：

- WSL 中的 WebMux 在 `<state_dir>/run/webmux.sock` 上提供本机 API（同一用户免 Token）：与 TCP 相同的 `/api`、`/ws`，以及 `/local/v1/status`（契约与 `status --json` v1 相同：`state`、`pid`、`launcher`、`sessions`、`urls`、`drift`、`warnings`、`logFile` 等）、`/local/v1/shutdown`（systemd/launchd 实例需 `force`）、`/local/v1/token`。`webmux bridge` 把该套接字原样桥接到标准输入输出，服务未运行时不写 stdout 并以退出码 3 结束。
- 引导与兜底子命令：`serve --log-file`、`status --json`（服务未运行时读运行时记录与配置）、`stop --json`（本机 API 无响应时退回信号）。环境变量 `WEBMUX_LAUNCHER=windows-tray` 让服务把启动方记为 `windows-tray`，且不进入会话环境。
- WSL 中的 `webmux config init` 可在配置不存在时生成带随机 Token 的 0600 配置。
- 无依赖的 `internal/contract` 定义了 `StatusV1`/`StopV1`/`ConfigV1` 与健康分类；`internal/localclient` 给定拨号器即可构造 HTTP/WebSocket 客户端，二者都可交叉编译到 Windows，托盘直接 import。`internal/client` 与 `internal/api` 依赖只支持 Unix 的 `internal/session`，托盘不能 import。
- 服务端 `hostCheck` 允许以 `127.0.0.1` 访问回环/通配监听实例；`/api/health` 未带 Token 时返回 401，可用于判断“可达且是 WebMux”。

WSL 侧需要特别处理的事实：

- `wsl.exe --list` 默认输出 UTF-16LE；设置 `WSL_UTF8=1` 后为 UTF-8（新版 WSL），两种都要解析。
- 发行版在没有 `wsl.exe` 客户端连接时会在空闲超时后被终止，即使启用了 systemd；需要一个长期存在且独立于托盘的 `wsl.exe` 进程保活。
- 从 Windows 创建 `wsl.exe` 时若不加 `CREATE_NO_WINDOW` 会闪出控制台窗口。
- `wsl.exe --exec` 不经 shell，因此 `~` 不会展开；`--cd ~` 由 wsl.exe 自身解析为用户主目录。
- Windows 环境变量只通过 `WSLENV` 列出的名称传入 WSL。
- NAT 网络模式下 Windows 经 localhostForwarding 访问 WSL 回环/通配监听，但局域网与 Windows 上的 Tailscale 流量不会转发进 WSL；mirrored 模式下 WSL 与主机共享网卡；用户关闭 localhostForwarding 时 Windows 侧不可达。WSL 内 `wslinfo --networking-mode` 可报告当前模式（新版 WSL）。

## Goals / Non-Goals

**Goals:**

- 托盘是 WebMux 的普通客户端：WSL 侧的配置解析、链接推导、进程识别与停止都由 `webmux` 自己完成，托盘只负责 Windows 侧的进程创建、可达性检查与界面。
- 平台无关逻辑（设置、argv 构造、发行版列表解析、启动方式选择、状态机、退避）可在 Linux CI 上 `go test -race`；Windows API 只出现在 `//go:build windows` 适配层。
- `go build/vet/test ./...` 在 Linux/macOS 上继续通过；Windows 目标保持 `CGO_ENABLED=0`。

**Non-Goals:**

- WebMux 原生运行在 Windows（ConPTY）。
- 安装器、代码签名、自动更新；在托盘中安装或升级 WSL 内的 `webmux`。
- 托盘内的会话列表、会话级操作、“需要关注”通知（#68 的手机推送在服务端单独推进；托盘以后若要做 Windows 通知，走数据面 `/ws/sessions` 订阅）。
- 在终端中启动 agent 并纳入服务端管理：属于 #67，走数据面（`POST /api/sessions` + `/ws/s/{id}`），另立变更；Windows 控制台客户端未排期。
- 修改已有 WebMux 配置的单个字段；转发的 TLS 终止；在 Windows 防火墙中自动添加规则。
- 带 Token 的链接、二维码。
- 图形化设置窗口；管理 WSL 本身（安装发行版、修改 `.wslconfig`、启用 systemd、安装 systemd 单元）。
- 多发行版同时托管。

## Decisions

### 0. 已与用户确认的产品决策

| # | 问题 | 结论 |
|---|---|---|
| Q1 | “开机启动”的含义 | Windows 用户**登录后**启动，使用 HKCU `Run` 启动项 |
| Q2 | WSL 内 WebMux 的托管方式 | **两种都支持**：托盘托管与 systemd 用户服务，默认“自动”，可在菜单切换 |
| Q3 | 托盘技术栈 | Go + `fyne.io/systray`，与前置变更共享 `internal/contract` 契约类型 |
| Q4 | 带 Token 的链接 | 不做；Token 单独复制 |
| Q5 | 设置界面 | 子菜单（发行版、启动方式）+ 记事本编辑设置 JSON、保存即热加载 |
| Q6 | v1 扩展（会话列表、通知、安装升级） | 都不做 |
| Q7 | 交付形式 | `make release` 产出两个 exe，无安装器与签名 |
| Q8 | 退出托盘 | 每次询问：仅退出托盘 / 停止并退出 / 取消 |
| Q9 | 客户端角色 | 托盘负责 WSL 网络特殊情况的转发、配置（初始化 / 打开 / 漂移提示）、服务启动与管理；在终端中启动 agent 由 #67 的 CLI 承担（走数据面） |
| Q10 | PR 拆分 | 前置变更独立为 PR #83，本变更留在 PR #82 |
| Q11 | 通讯方案 | 采用 #83 的本机套接字 API + `wsl.exe --exec webmux bridge`；状态、停止、取 Token、会话订阅都经桥接 |
| Q12 | 会话内进程的本机 API 权限 | 由 #83 规定：可创建 / 列出 / 附着会话，不能停止服务或取 Token；托盘不受影响（不在会话内运行） |
| Q13 | 实施顺序 | #83 合入后实施，与 #67 并行；Windows 控制台客户端（PR #84）在 #67 之后，二者共用 `internal/wslclient` |

### 1. 托盘只做客户端（与前置变更的分工）

| 职责 | 归属 | 理由 |
|---|---|---|
| 配置解析、默认值、链接推导、配置漂移 | 服务端 `/local/v1/status`（未运行时 `webmux status`） | 与服务端同一份代码，#67 的 `open` 也复用 |
| 停止 | 服务端 `/local/v1/shutdown`；无响应时 `webmux stop` 发信号 | 正常路径与 SIGTERM 同一清理流程；卡死时信号最可靠 |
| 会话数与会话变化 | 服务端 `/ws/sessions` 订阅 | 实时、无需轮询 |
| 桥接拨号器、目标发行版与 `webmux` 路径解析 | `internal/wslclient`（与 `add-windows-console-client`，PR #84 共用） | 把 `wsl.exe --exec webmux bridge` 的 stdio 包装为 `net.Conn` 交给 `internal/localclient` |
| 输出重定向、避免 SIGPIPE | `webmux serve --log-file` | 不需要 shell 包装脚本 |
| 发行版列表、启动方式检测、`wsl.exe` 进程创建与保活 | 托盘 | 只有 Windows 侧能做 |
| Windows 侧可达性 | 托盘 | 只有 Windows 侧能观察 |
| 剪贴板、通知、登录启动项、单实例 | 托盘 | Windows API |
| 配置初始化与校验 | `webmux config init` / `validate` | 与服务端同一份模板与校验 |
| WSL 网络特殊情况的转发 | 托盘 | 只有 Windows 主机能监听其网卡 |

上一版草案中托盘自带的“固定 shell 脚本 + pidfile + TOML 解析 + 链接推导”整体移除。

### 2. 代码组织

```
cmd/webmux-tray/
  main_windows.go      // 入口：-H=windowsgui，显式组装依赖；version 子命令
  main_other.go        // !windows：打印“仅支持 Windows”并退出 1
internal/tray/          // 平台无关，Linux CI 可测
  settings.go          // 设置结构、默认值、JSON 读写与校验
  wsl.go               // Runner 接口；wsl.exe argv 构造；--list 解析（UTF-16LE/UTF-8）
  mode.go              // auto/tray/systemd 选择与条件探测
  supervisor.go        // 状态机、退避、外部实例判定；依赖 Runner / contract.StatusV1 / 时钟接口
internal/tray/forward/  // 平台无关，Linux CI 可测：Host/Origin 校验 + httputil.ReverseProxy 中转
internal/tray/winui/    // //go:build windows
  tray.go menu.go notify.go dialog.go clipboard.go autostart.go process.go singleinstance.go
```

### 3. WSL 调用（全部为 argv，`CREATE_NO_WINDOW`，环境含 `WSL_UTF8=1`）

| 用途 | argv（`[-d <distro>]` 省略） | 生命周期 |
|---|---|---|
| 列出发行版 | `wsl.exe --list --quiet`（另用 `--list --verbose` 取默认与版本） | 短 |
| 本机 API 桥接 | `wsl.exe --cd ~ --exec <webmux> bridge [--config <c>]`，stdin/stdout 作为一条连接 | 每条连接一个；常驻一条承载 `/ws/sessions`，另按需建立承载 `/local/v1/*` 的 keep-alive 连接 |
| 未运行时的状态 | `wsl.exe --cd ~ --exec <webmux> status --json [--config <c>]`（仅当 bridge 退出码为 3） | 短，超时 10 秒 |
| 托盘托管启动 | `wsl.exe --cd ~ --exec <webmux> serve --log-file <log> [--config <c>]`，环境 `WEBMUX_LAUNCHER=windows-tray`、`WSLENV` 追加 `WEBMUX_LAUNCHER` | 长，detached |
| 托盘托管停止 | 经桥接 `POST /local/v1/shutdown`；15 秒未退出或无响应时 `wsl.exe --cd ~ --exec <webmux> stop --json --timeout 5s [--config <c>]` 强制结束 | 短 |
| 初始化配置 | `wsl.exe --cd ~ --exec <webmux> config init [--config <c>]` | 短 |
| 网络模式 | `wsl.exe --exec wslinfo --networking-mode`（失败时按 NAT 处理） | 短，启动与状态刷新时 |
| systemd 探测 | `wsl.exe --exec cat /proc/1/comm`；`wsl.exe --exec systemctl --user cat webmux.service` | 短 |
| systemd 启停 | `wsl.exe --exec systemctl --user start\|stop\|restart webmux.service` | 短 |
| systemd 保活 | `wsl.exe --exec sleep infinity` | 长，detached |
| systemd 日志 | `wsl.exe --exec journalctl --user -u webmux.service -n 1000 --no-pager` → 写临时文件后用记事本打开 | 短 |

`~/` 处理：`wsl.exe --exec` 不展开 `~`，因此对 `webmux` 路径，托盘用 `--cd ~` 并把 `~/x` 改写为相对路径 `./x`；对 `--config`、`--log-file` 的值则原样传 `~/...`，由前置变更中 `webmux` 的 `~/` 展开处理。其他路径原样作为单个 argv 元素。

桥接拨号器：`DialContext` 启动 `wsl.exe [-d D] --cd ~ --exec <webmux> bridge`，stdin/stdout 包装为 `net.Conn`（stderr 进入托盘日志，`Close` 关闭 stdin 并等待进程退出，2 秒后仍未退出则结束进程）；进程以退出码 3 结束且未产生任何输出时返回 `ErrNotRunning`，状态机据此转为调用 `status --json`。`internal/localclient` 的 `http.Transport` 设置 `MaxConnsPerHost` 2、空闲 90 秒，避免频繁新建桥接。桥接连接不承担保活职责（托盘退出即断开）。

detached：长生命周期进程以 `CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW` 创建，stdin/stdout/stderr 均为 NUL，不加入随托盘关闭而终止的 Job Object。托盘把 systemd 保活进程的 Windows PID 与创建时间写入 `%LOCALAPPDATA%\WebMux\keepalive.json`，重启后据此识别并在用户停止 WebMux 时结束它（用 `GetProcessTimes` 校验，防 PID 复用）。

### 4. 启动方式选择

`auto`：`/proc/1/comm` 为 `systemd` 且 `systemctl --user cat webmux.service` 成功 → systemd，否则托盘托管。选择结果在每次启动前重新计算并显示在状态行。“外部实例”判定：`status.record == false`，或启动方与当前方式不符（托盘托管期望 `windows-tray`，systemd 期望 `systemd`）。

### 5. 状态机

```
Stopped ──start──▶ Starting ──status running+healthy──▶ Running ──user stop──▶ Stopping ──stopped──▶ Stopped
   ▲                  │ 60s 未就绪                        │ 进程退出 / 连续 3 次无响应
   │                  ▼                                  ▼
   └──── user stop ─ Failed ◀──退避耗尽── Restarting（仅托盘托管：2s→60s，10 分钟内 ≤5 次）
External：status running 但不归当前方式管理；变为 stopped 后回到 Stopped（不自动接管启动）
Unreachable（Running 的子状态，图标为异常）：WSL 内健康但 Windows 侧不可达
```

单 goroutine 事件循环，对 UI 只发布不可变快照。事件来源：托管 `wsl.exe` 的 `Wait()`（托盘托管）、经桥接的 `/ws/sessions` 订阅（快照与增量，驱动会话数）、定时 `/local/v1/status`（60 秒，及订阅断开、菜单打开、操作完成时）、定时 Windows 侧可达性（10 秒）、用户操作。`wsl --shutdown` 会同时触发进程退出与状态变化，以先到者为准并去重。

### 6. 转发

**为什么需要**：NAT 模式下，手机经 Windows 主机的局域网或 Tailscale 地址访问 7681 端口时，流量不会进入 WSL。可选办法：

| 办法 | 结论 |
|---|---|
| 提示用户切换 mirrored 模式或在 WSL 内安装 Tailscale | 首选推荐，托盘检测到时提示“无需转发” |
| `netsh interface portproxy` | 否决：需要管理员；WSL 地址每次启动都会变；纯 TCP 转发后 Host 为主机 IP，会被 WebMux 的 Host 校验拒绝 |
| 纯 TCP 中转 | 否决：同样的 Host 问题，只能要求用户把主机 IP 写进 `allowed_origins` 并重启（丢会话），IP 变化后又失效 |
| **托盘内 HTTP 感知的反向代理** | **采用** |

**做法**：每个勾选地址一个 `net.Listen("tcp", "<ip>:<port>")`，`httputil.ReverseProxy` 指向本机链接（`127.0.0.1:<port>`，Go 1.12 起原生支持 `Upgrade: websocket` 双向中转）。代理前置校验：

1. `r.Host` 必须等于 `<ip>:<port>`（IP 字面量，不受 DNS 重绑定影响），否则 403；
2. `Origin` 为空或等于 `http://<ip>:<port>`，否则 403；
3. 通过后改写 `Host` 为 `127.0.0.1:<port>`；`Origin` 非空时改写为 `http://127.0.0.1:<port>`。

由于代理只放行与自身同源的请求，再把它们表达为与 WebMux 同源的请求，跨站请求在代理处就被拒绝，等价于保留 WebMux 的 Origin 校验；Host 校验同理。认证头、Cookie、`?token=` 原样透传，认证仍完全由 WebMux 判定。代理不打访问日志，`ErrorLog` 只记录连接级错误（不含 URL 查询串，避免记录 `?token=`）。删除 `X-Forwarded-*` 头，WebMux 不依赖它们。

**为什么不复用 `internal/api` 的 `hostCheck`/`originAllowed`**：它们所在的包依赖只支持 Unix 的 `internal/session`，不能编进 Windows 托盘；而代理需要的规则（单一 IP 字面量 + 严格同源）比服务端规则简单得多，单独实现并用测试锁定更清晰。

**其他**：并发上限 64（超出时立即关闭新连接）；非 WebSocket 连接 10 分钟空闲关闭；启用条件（运行中且健康、配置了 Token、Windows 侧可达）不满足时关闭监听，满足后恢复。地址列表来自 `net.Interfaces()`，排除回环、链路本地与 `vEthernet (WSL*)`，`100.64.0.0/10` 标为 Tailscale 置顶。首次在非回环地址监听时 Windows 防火墙可能弹出授权提示（允许需要管理员确认），文档说明；托盘不自动修改防火墙。

### 7. Windows 适配层

- 托盘：`fyne.io/systray`（Windows 实现基于 `Shell_NotifyIcon`，纯 Go）；4 个状态 `.ico` 用 `go:embed`。
- 通知：`Shell_NotifyIcon` 气泡（`NIF_INFO`），无需注册 AppUserModelID。
- 对话框：`MessageBoxW`；退出三选一用 `MB_YESNOCANCEL`，按钮文案在正文中说明（是 = 停止并退出，否 = 仅退出托盘）；后续如需自定义按钮再换 `TaskDialogIndirect`。
- 剪贴板：写入 `CF_UNICODETEXT` 并同时设置 `ExcludeClipboardContentFromMonitorProcessing`、`CanIncludeInClipboardHistory=0`、`CanUploadToCloudClipboard=0`；记录 `GetClipboardSequenceNumber`，60 秒后序号未变则清空。
- 登录启动：`HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 值 `WebMuxTray` = `"<exe 绝对路径>"`。
- 单实例：命名互斥量 `Local\WebMuxTray-<用户 SID>`；第二实例通过 `PostMessage` 向已有实例的隐藏窗口发自定义消息。
- 打开：浏览器、记事本、日志用 `ShellExecuteW`；WSL 内文件经 `\\wsl.localhost\<distro>\...` 路径打开（此时发行版已在运行）。主目录通过 `status` 返回的绝对 `logFile`/`config.path` 得到，不再另行查询。
- 文件：设置 `%APPDATA%\WebMux\tray.json`；托盘日志 `%LOCALAPPDATA%\WebMux\tray.log`（1 MiB 轮转，保留 1 份）。设置热加载用 2 秒 mtime 轮询。
- Windows 侧可达性：独立 `http.Client`（`Proxy: nil`），单次 3 秒，不带 Token；200 或 401 JSON 视为可达。

### 8. 构建与 CI

- `Makefile`：`tray` 目标 `CGO_ENABLED=0 GOOS=windows GOARCH={amd64,arm64} go build -trimpath -ldflags "$(LDFLAGS) -H=windowsgui" -o bin/webmux-tray-windows-<arch>.exe ./cmd/webmux-tray`；`release` 依赖 `tray`；托盘不依赖前端构建。
- exe 图标与版本资源：用 `go-winres` 生成并提交 `cmd/webmux-tray/rsrc_windows_*.syso`（工具不进入构建依赖）。
- CI：现有 `go` 作业追加两个架构的 Windows 交叉编译与 `GOOS=windows go vet ./cmd/webmux-tray ./internal/tray/...`；`internal/tray` 单元测试随 `go test -race ./...` 在 Ubuntu/macOS 运行。不新增 Windows runner（托管 runner 不提供可用的 WSL 2），真实 WSL 行为由手动验收覆盖。

## Risks / Trade-offs

- [转发把 shell 暴露给网络] → 默认关闭、必须已配置 Token、只监听显式勾选的具体地址、Tailscale 置顶推荐、镜像模式下提示无需转发。
- [转发随托盘退出而停止] → 退出对话框明确说明；需要常驻转发的用户应改用 mirrored 模式或 WSL 内的 Tailscale。
- [托盘与 WSL 中 `webmux` 版本不一致] → 以 `status --json` 的 `v` 为契约；缺失或更高的不兼容版本给出“请升级 webmux / 托盘”提示。
- [旧版 inbox WSL 不支持 `--exec`/`--cd`/`WSL_UTF8`] → 要求 Microsoft Store 版 WSL；启动时 `wsl.exe --version` 失败即提示更新。
- [每条桥接连接都要启动一次 `wsl.exe`（约 100–300 ms）] → 常驻一条订阅连接，状态请求复用 keep-alive 连接；高频的 10 秒可达性检查只走 Windows 侧 TCP。
- [桥接连接意外断开（`wsl --shutdown`、发行版重启）] → 订阅断开即刷新状态并以退避重连；与托管进程 `Wait()` 的事件去重。
- [systemd 保活进程在托盘崩溃后遗留] → 记录 PID+创建时间，下次启动托盘时识别；WebMux 已停止时结束它。
- [托盘退出后 WebMux 继续运行，用户以为“退出就全停了”] → 退出三选一明确说明。
- [Windows 侧 localhost 不可达（关闭转发、VPN 劫持回环）] → 区分“WSL 内健康”与“Windows 可达”，给出具体设置项。
- [企业代理拦截 127.0.0.1] → 可达性检查禁用系统代理。
- [Token 经剪贴板泄露] → 不进历史/云剪贴板、60 秒清除、仅在点击时从 WSL 取得。
- [`fyne.io/systray` 菜单能力有限（禁用项说明、动态子菜单）] → 说明文字放状态行；必要时退回直接调用 Win32 菜单 API。
- [`wsl --shutdown`、Windows 睡眠会结束会话] → WebMux“服务重启即结束会话”的延伸；通知中写明“会话已丢失”。

## Migration Plan

纯新增组件，无数据迁移。已用 systemd 在 WSL 中运行 WebMux 的用户在“自动”方式下会被识别为 systemd 方式并获得完整控制。回滚：取消“登录时自动启动”，删除 exe 与 `%APPDATA%\WebMux`、`%LOCALAPPDATA%\WebMux`。

## Open Questions

- 托盘图标视觉（是否沿用前端 `WM` 品牌标记）在实现阶段随截图确认。
- 最低 WSL 版本号待在真实机器上验证 `--exec`/`--cd`/`WSL_UTF8` 后写入文档。
