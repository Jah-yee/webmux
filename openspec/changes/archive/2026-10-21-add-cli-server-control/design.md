## Context

动机见 proposal.md。相关现状：

- `cmd/webmux/main.go` 只在 `os.Args[1] == "version"` 时分流，其余参数全部交给 `flag.NewFlagSet("webmux")` 作为服务参数；`loadConfig` 已封装“默认路径 / 显式路径必须存在 / 命令行覆盖 / Validate”的完整规则，可直接复用。
- `api.New` 返回一个 `http.Handler`，路由注册在 `http.ServeMux` 上，`middleware` 统一做 Host 校验、认证与 Origin 校验；`run()` 用一个 `http.Server` 服务单个 TCP listener，`BaseContext` 绑定信号上下文，退出顺序为 `m.Shutdown` → `server.Shutdown`。
- `internal/workspace/registry.go` 已在 `state_dir` 中用 `flock(LOCK_EX|LOCK_NB)` 保证“一个状态目录一个实例”，冲突时只禁用仓库功能、服务照常运行——运行时记录与本机套接字沿用同一取舍。
- 进程信息读取已按平台拆分：`internal/session/proc_linux.go` 读 `/proc/<pid>/stat`，`proc_darwin.go` 用 sysctl / `proc_pidinfo`；`cleanup_linux.go` 的 `sessionMarked` 能按 `/proc/<pid>/environ` 中的 `WEBMUX_SESSION_ID` 识别会话进程；Linux 上服务是 subreaper。
- `internal/session/env.go` 的 `sessionEnv` 已有“不让宿主标识变量进入会话”的过滤表。
- 认证：`/api/*`、`/ws/*` 接受 `Authorization: Bearer`、`webmux_token` Cookie，`/ws/*` 另接受 `?token=`。会话创建 `POST /api/sessions` 已支持 `preset: "custom"` + `cmd` argv + `cwd` + `name`。
- 配置只在启动时读取一次，没有热重载。

## Goals / Non-Goals

**Goals:**

- 确立 WebMux 的客户端/服务端分工与通讯方案（决策 0），本变更实现其公共基础。
- 所有本机客户端（Linux/macOS CLI、经 WSL 桥接的 Windows 托盘与控制台客户端）只使用一种协议：现有 HTTP/WebSocket API，经本机套接字访问，按操作系统身份认证。
- 不新增网络暴露面；TCP 监听的认证、Host、Origin 行为不变；现有启动方式零改动可用。

**Non-Goals:**

- 拆分会话宿主与 HTTP 前端为两个进程、服务升级不丢会话。
- 配置热重载；以程序方式修改配置项（`config set`）。
- #67 的 `run/ls/attach/open` 与 #68 的 `events/wait` 本身——本变更只提供它们复用的核心、子命令框架与本机 API。
- Windows 原生运行 WebMux 服务；Windows 命名管道。

## Decisions

### 0. 整体架构：客户端/服务端分工与通讯方案

#### 0.1 角色

| 角色 | 形态 | 职责 | 不负责 |
|---|---|---|---|
| **服务端** | Linux/macOS `webmux serve` | 唯一持有 PTY、会话状态、回放缓冲；唯一的认证判定者；解析并校验配置 | 进程托管方式（交给 systemd/launchd/托盘/手动） |
| **本机客户端（CLI）** | 同一个 `webmux` 二进制的子命令，运行在服务端所在机器（含 WSL 内） | 启动与管理服务（`serve/status/stop`）、配置检查与初始化（`config`）、在终端中启动 agent 并纳入服务端管理（#67）、事件出口（#68） | 持有会话状态 |
| **Windows 托盘** | `webmux-tray.exe` | 在 WSL 中启动与管理服务、链接与常用操作、WSL 网络特殊情况的转发 | 解析服务端配置或进程表 |
| **Windows 控制台客户端** | `webmux.exe`（`add-windows-console-client`） | 在 PowerShell / Windows Terminal 中启动 agent 与附着会话（会话运行在 WSL） | 运行服务 |
| **浏览器** | 现有前端 | 终端交互 | — |

#### 0.2 通讯方案（已与用户确认）

```
                         ┌── TCP 监听（不变）：Token + Host + Origin
 浏览器 / 手机 / 托盘转发 ──┘     ↑ 托盘转发只能指向这里
                                  │
               webmux serve：同一套 /api、/ws 路由，两个监听器，按监听器选择认证策略
                                  │
                         └── Unix 套接字 state_dir/run/webmux.sock（0600，目录 0700）
                               认证：对端有效 UID == 服务端有效 UID；另有 /local/v1/*
  Linux/macOS CLI ─────────────────┤
  Windows 托盘 / webmux.exe ──▶ wsl.exe --exec webmux bridge（stdio ⇄ 套接字，一次一条连接）

  引导与兜底（服务未运行或无响应时）：webmux serve | config | status（读记录）| stop（发信号）
```

- **一种协议**：会话操作、状态、停止、取 Token 全部是 HTTP/WS。CLI、托盘、控制台客户端与浏览器使用同一套 `/api`、`/ws` 契约与 `docs/protocol.md` 的帧格式；本机专属能力放在 `/local/v1/*`，只在套接字上提供。
- **两种认证，按监听器区分**：TCP 是网络边界，继续用 Token + Host + Origin；套接字是本机边界，用对端进程身份（Linux `SO_PEERCRED`，macOS `LOCAL_PEERCRED`）。浏览器无法连接 Unix 套接字，因此套接字上没有 CSRF/DNS 重绑定面，不需要 Host/Origin 校验。
- **跨越 WSL**：WSL 2 的 Unix 套接字与 Windows 不互通，Windows 客户端通过启动 `wsl.exe --exec webmux bridge` 把它的标准输入输出当作一条字节流连接；Go 的 `http.Transport.DialContext` 与 WebSocket 拨号都可替换为该拨号器，上层协议不变。HTTP/1.1 keep-alive 让一条桥接承载多个请求，WebSocket 升级照常工作。这是 Docker 用户以 `npiperelay` + `socat` 桥接 docker.sock 的同一模式。
- **引导与兜底保留为子命令**：启动服务本身只能是执行进程；服务卡死时停止只能靠信号；配置检查不应要求服务在运行。

#### 0.3 信任边界

能连接套接字的只有同一 Linux 用户的进程，或能以该 Windows 用户身份运行 `wsl.exe` 的进程。它们原本就能读取 0600 的配置得到 Token、或直接向服务进程发信号，因此“免 Token”没有扩大权限。例外是 WebMux 会话内部的进程（例如被提示注入的 agent）：它们与服务同一用户，可以连接套接字；为此对来自会话内部的连接禁用 `/local/v1/shutdown` 与 `/local/v1/token`（已确认，见 0.5），但保留创建与附着会话的能力，使 agent 能在会话内用 `webmux run` 启动兄弟会话。

托盘转发必须指向 TCP 监听：若指向套接字，就把“同一用户免 Token”开放给了网络。

#### 0.4 对候选方案的取舍

| 方案 | 结论 | 理由 |
|---|---|---|
| 子命令输出 JSON + TCP/Token（上一版） | 被替代 | 两套协议与认证；Windows 侧要取得 Token；依赖 WSL localhost 转发；只能轮询 |
| 全走 TCP，加 `/api/admin/*` | 否决 | 服务无响应时不可用；网络侧可关机；Windows 仍要 Token 且依赖转发 |
| **本机套接字 + `wsl.exe` 桥接** | **采用** | 一种协议；本机免 Token；不依赖 localhost 转发；可推送；不开网络端口 |
| WSL 内常驻代理 + 自定义 JSON-RPC | 否决 | 私有协议与需要重同步的状态；桥接已能提供长连接 |
| Hyper-V socket / vsock 直连 | 否决 | WSL 虚拟机 ID 不公开且不稳定 |
| gRPC / Connect | 否决 | 需要重做现有 REST/WS 契约 |
| 会话守护进程 + 独立 HTTP 前端 | 否决 | 回放与订阅共享锁的无缝续接要跨进程重做；违背“服务重启即结束会话”；#76 已表明不引入分布式复杂度 |

#### 0.5 已与用户确认的决策

| # | 问题 | 结论 |
|---|---|---|
| D1 | 会话内进程能否使用本机 API | 能创建、列出、附着会话；不能 `shutdown` 与取 Token |
| D2 | 实施顺序 | 本变更 → #67（Linux CLI `run/attach/ls/open`）→ `add-windows-console-client`；`add-windows-wsl-tray` 与 #67 并行 |

#### 0.6 后续变更的落点

| 变更 | 使用的通道 |
|---|---|
| `add-windows-wsl-tray`（PR #82） | 桥接访问 `/local/v1/*` 与 `/ws/sessions`；引导用 `serve`、`config init`；Windows 侧可达性与转发走 TCP |
| #67 终端启动与附着 agent | 套接字上的 `POST /api/sessions`（新增可选 `env`）与 `/ws/s/{id}`；套接字不可用时退回 TCP + 配置中的 Token |
| #68 事件出口 | 套接字上的 `/ws/sessions` 订阅 + 客户端差分 |
| `add-windows-console-client` | 桥接访问 `/api`、`/ws`、`/local/v1/status` |

### 1. 分层与包结构

```
cmd/webmux/            子命令分发：serve(默认) / version / status / stop / bridge / config
internal/contract/     无依赖叶子包：StatusV1 / StopV1 / ConfigV1、健康分类、/local/v1 路径常量
internal/localclient/  只依赖标准库与 coder/websocket，可交叉编译到 Windows：
                       给定 Dialer 构造 HTTP/WS 客户端；Status / Shutdown / Token；UnixDialer
internal/client/       Unix：Locate(config) → 套接字路径、运行时记录、Token；退回路径；Links()
internal/runtime/      记录读写、flock、原子替换；ProcessIdentity(pid)、PeerCred(conn) 分 _linux.go / _darwin.go
internal/api/          新增 NewLocal(...)：同一 mux 包上本机策略中间件，并注册 /local/v1/*
deploy/embed.go        go:embed 暴露 config.example.toml，供 config init 使用
```

`internal/client` 依赖 `internal/config`，而后者依赖只支持 Unix 的 `internal/session`，所以 Windows 客户端只 import `internal/contract` 与 `internal/localclient`，并提供自己的桥接拨号器。

### 2. 两个监听器

`api.New` 拆为“路由注册”与“策略中间件”两部分：`New` = 路由 + 现有 TCP 中间件；`NewLocal` = 同一路由 + `/local/v1/*` + 本机中间件（跳过 Host/认证/Origin，只保留安全响应头与 panic 恢复）。`run()` 再创建一个 `http.Server` 服务 Unix listener，`ConnContext` 在接受连接时读取对端凭据：UID 不符立即关闭；同时记录对端 PID，供“会话内进程”判定。两个 server 共享 `BaseContext` 与关闭顺序（`m.Shutdown` 后依次 `Shutdown` 两个 server）。

`/local/v1/shutdown` 不直接退出：它取消 `run()` 使用的信号上下文（与 SIGTERM 同一路径），因此清理顺序、记录删除与日志完全一致。

会话内进程判定：Linux 先查 `/proc/<pid>/environ` 的 `WEBMUX_SESSION_ID`（复用 `sessionMarked`），再沿父进程链查找是否经过任一会话的根 PID；macOS 只用父进程链。判定结果只影响 `/local/v1/shutdown` 与 `/local/v1/token`。

### 3. 套接字路径

默认 `<state_dir>/run/webmux.sock`。`sun_path` 上限 Linux 108、macOS 104 字节；超长时改用 `$XDG_RUNTIME_DIR/webmux/<sha256(state_dir) 前 12 位>.sock`（目录 0700）；都不可用则禁用本机 API 并警告。实际路径写入运行时记录，客户端以记录为准，记录缺失时按同一规则推算。

### 4. `bridge`

两个 goroutine 分别 `io.Copy(conn, os.Stdin)` 与 `io.Copy(os.Stdout, conn)`；任一方向结束即半关闭（`CloseWrite`）另一方向并等待其结束。启动时 `signal.Ignore(syscall.SIGPIPE)`，标准输出写失败按 EOF 处理。连接失败时不写标准输出：`ENOENT`/`ECONNREFUSED` 退出码 3，其他 1。Windows 侧拨号器把 `wsl.exe` 进程的 stdin/stdout 包装为 `net.Conn`（`Close` 时关闭 stdin 并等待进程退出，超时则结束进程），把进程的退出码 3 映射为“服务未运行”错误。

### 5. 运行时记录

- 位置：`<state_dir>/run/server.json`，锁 `<state_dir>/run/server.lock`；放在状态目录而非 `$XDG_RUNTIME_DIR`，因为后者在未启用 systemd 的 WSL 与 macOS 上常不存在。
- 写入时机：两个 listener 都就绪后、开始 `Serve` 前；删除时机：`run()` 末尾清理之后。
- 进程身份：`(pid, startIdentity)`，Linux 额外比较 `/proc/<pid>/exe`（去掉 ` (deleted)`）。
- Token 指纹：`sha256("webmux-token-fingerprint\x00" + token)` 前 16 个十六进制字符，只用于 `drift`。
- 启动方：`WEBMUX_LAUNCHER=windows-tray` > `INVOCATION_ID`（systemd）> `XPC_SERVICE_NAME` 非空且非 `0`（launchd）> `manual`。

### 6. `--log-file`

服务启动时打开日志文件，把 fd 1、2 用 `dup3`（Linux）/`dup2`（darwin）指向它，并重建 `slog` 默认 handler；panic 输出也落盘，父进程关闭管道后不会触发 SIGPIPE 退出。只做“启动时 rename 为 `.1`”的轮转。

### 7. `config init` 的 Token

32 字节 `crypto/rand` 的 base64url（无填充）编码；以嵌入的 `deploy/config.example.toml` 为模板，只替换 `token`（与可选的 `listen`）一行；已存在时拒绝，`--force` 先改名为 `.bak`。

### 8. 退出码

`0` 运行中且健康 / 操作成功；`3` 未运行（`status`、`bridge`）；`1` 其他错误；`2` 用法错误。

## Risks / Trade-offs

- [第一个参数原本可能是被误写的位置参数] → 现在报错退出码 2，已与用户确认；`deploy/` 模板与文档写法都以 `-` 开头。
- [会话内进程同为该用户，可经套接字创建会话] → 与其现在就能读取配置 Token 调用 TCP API 等价；对停止与取 Token 加限制；macOS 识别为尽力而为。
- [每个 Windows 桥接连接都要启动一次 `wsl.exe`（约 100–300 ms）] → keep-alive 复用与常驻 WebSocket 摊薄；需要时再增加托盘侧命名管道复用（非本变更）。
- [套接字路径过长或 `$XDG_RUNTIME_DIR` 缺失] → 退回规则 + 记录中的实际路径；仍不可用时 TCP 服务不受影响，`status` 走退回路径。
- [旧版本服务没有记录与套接字] → `status` 以 TCP 健康检查兜底；`stop` 拒绝操作。
- [配置不能热更新] → 只提示漂移；如需可另立变更为安全子集增加 SIGHUP 重载。

## Migration Plan

纯新增；不需要迁移。升级后第一次启动即写记录并创建套接字。回滚到旧版本时遗留的 `run/` 目录无害。
