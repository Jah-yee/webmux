## Why

`webmux` 目前只有“启动服务”和 `version` 两种用法，外部程序要判断“服务是否在运行、实际监听在哪、该打开哪个链接、怎样安全地停止它”，只能各自解析 TOML、猜默认值、读取 Token、按端口或进程名去找 PID。Windows 托盘（`add-windows-wsl-tray`）、Windows 控制台客户端（`add-windows-console-client`）、原生终端入口（#67 `webmux run/ls/attach/open`）和事件出口（#68 `webmux events/wait`）都需要这一层，而它们只是同一个服务的不同客户端。先确立客户端/服务端的分工与通讯方案，再提供它们共用的本机 API、子命令与客户端核心，后续客户端不再各写一遍。

## What Changes

- 确立客户端/服务端架构（design.md 决策 0，已与用户确认）：服务端是会话、认证与配置解析的唯一权威；所有客户端只使用现有 HTTP/WebSocket 协议；网络访问走 TCP（Token + Host + Origin，不变），本机访问走 Unix 域套接字（按操作系统用户身份认证、免 Token）；Windows 客户端经 `wsl.exe --exec webmux bridge` 把套接字桥接到标准输入输出。
- **本机 API**：服务端在状态目录内额外监听 0600 的 Unix 套接字，提供与 TCP 相同的 `/api/*`、`/ws/*`，以及只在本机可用的 `/local/v1/status`、`/local/v1/shutdown`、`/local/v1/token`；来自 WebMux 会话内部的连接不能停止服务或取 Token。
- `webmux` 增加子命令分发：无子命令或以 `-` 开头的参数保持现有“启动服务”行为（**完全兼容**现有 systemd/launchd 模板）；新增 `serve`、`status`、`stop`、`bridge`、`config`；`version` 不变。未知子命令报错退出码 2。
- 服务端在监听就绪后写入**运行时记录**（PID、进程启动标识、版本、实际监听地址、配置路径、Token 指纹、启动方、日志路径、套接字路径），正常退出时删除。
- `webmux serve --log-file <path>`：服务输出写入文件并在启动时轮转为 `.1`；`--config` 与 `--log-file` 支持 `~/`。
- `webmux status [--json] [--reveal-token]`：优先经本机 API 获取状态，退回到运行时记录 + TCP 健康检查；JSON 契约 v1。
- `webmux stop [--timeout] [--force] [--json]`：优先经本机 API 停止，服务卡死时退回到信号；systemd/launchd 实例默认拒绝。
- `webmux bridge`：把本机套接字原样桥接到标准输入输出，供 Windows 客户端使用。
- `webmux config path|show|validate|init`：定位、查看生效配置（不含 Token）、校验、以随机 Token 初始化 0600 配置文件；不提供以程序方式改写已有配置的命令。
- 新增客户端核心：无依赖的契约包、可交叉编译到 Windows 的本机 API 客户端、Unix 上的服务定位与退回逻辑。

## Capabilities

### New Capabilities

- `server-local-api`: 本机套接字的创建与生命周期、按对端用户的认证、接口范围（`/api`、`/ws` 与 `/local/v1/*`）、会话内进程的限制。
- `server-runtime-record`: 服务端运行时记录的内容、位置、权限、生命周期、单实例约束，以及 `--log-file` 输出重定向。
- `cli-server-control`: 子命令分发与兼容性、`status`/`stop` 的行为与 JSON 契约（v1）、链接推导规则、`bridge`、退出码。
- `cli-config`: `webmux config` 的定位、生效配置查看、校验与初始化，以及“客户端不改写已有配置”的约束。

### Modified Capabilities

当前 `openspec/specs/` 无已登记主规格。服务启动参数、配置文件格式、TCP 上的 HTTP/WebSocket 契约、认证与 Origin/Host 校验均不变。

## Impact

- `cmd/webmux/main.go`：子命令分发；`serve` 增加 `--log-file`、第二个 `http.Server` 服务本机套接字、写入与删除运行时记录。
- `internal/api/`：路由注册与策略中间件拆分，新增 `NewLocal`（本机中间件与 `/local/v1/*`）；TCP 行为不变。
- 新增 `internal/contract/`（无依赖契约类型）、`internal/localclient/`（可交叉编译到 Windows 的 HTTP/WS 客户端与拨号器接口）、`internal/client/`（Unix 上的定位、退回、链接推导）、`internal/runtime/`（记录、锁、进程身份、对端凭据，分 `_linux.go`/`_darwin.go`）。
- `internal/session/env.go`：会话环境不继承 `WEBMUX_LAUNCHER`。
- 新增 `deploy/embed.go` 嵌入 `deploy/config.example.toml`，作为 `config init` 模板；`deploy/webmux.service` 与 `deploy/com.webmux.plist` 无需修改。
- 文档：`docs/architecture.md`（客户端与通讯方案）、`docs/protocol.md`（本机 API、`/local/v1`、控制面 JSON 契约）、`docs/deployment.md`。
- 不新增第三方依赖（`golang.org/x/sys` 已有）。
- 后续依赖方：`add-windows-wsl-tray`（PR #82）、`add-windows-console-client`、#67、#68。
