## Context

动机见 proposal.md。客户端/服务端分工与通讯方案见 `add-cli-server-control`（PR #83）design.md 决策 0。与实现相关的现状与前置：

- #83 提供：WSL 内 WebMux 的本机 API（Unix 套接字，同一用户免 Token，与 TCP 相同的 `/api`、`/ws` 及 `/local/v1/*`）；`webmux bridge` 把套接字原样桥接到标准输入输出，服务未运行时以退出码 3 结束且不写 stdout；无依赖的 `internal/contract` 与可交叉编译的 `internal/localclient`（给定拨号器即可构造 HTTP/WebSocket 客户端）。
- #67 提供（待其变更落地）：`POST /api/sessions` 的可选 `env` 字段；Linux CLI 的 `run/attach/ls/open` 语义（默认附着、`--detach`、分离键 `Ctrl+]`、附着默认取得控制、退出码透传）；附着协议沿用 `docs/protocol.md` 的帧（0x00 输出/输入、0x01 尺寸与快照、0x03 退出、0x04 位置、0x05/0x06 控制权）。
- `add-windows-wsl-tray`（PR #82）在 `%APPDATA%\WebMux\tray.json` 保存目标发行版与 WSL 内 `webmux` 路径，并需要同样的桥接拨号器。
- Windows 控制台：`SetConsoleMode` 的 `ENABLE_VIRTUAL_TERMINAL_INPUT`（输入端）与 `ENABLE_VIRTUAL_TERMINAL_PROCESSING`（输出端）分别让键盘产生 VT 序列、让控制台解释 VT 输出；关闭 `ENABLE_PROCESSED_INPUT` 后 `Ctrl+C` 作为 0x03 字节读入；没有 SIGWINCH。
- Go 在 Windows 上按 Microsoft C 运行时规则把命令行拆分为 `os.Args`；PowerShell 在传递给原生程序前会再处理一次引号。

## Goals / Non-Goals

**Goals:**

- 与 Linux CLI 共用附着核心，Windows 只实现控制台适配；行为差异只来自平台本身。
- 不在 Windows 侧接触 Token；不依赖 WSL 网络设置。
- 与托盘共用目标选择与桥接拨号器，用户只需配置一次发行版。

**Non-Goals:**

- 在 Windows 上运行服务、`stop`/`serve` 等管理命令（交给托盘与 WSL 内的 `webmux`）。
- 经托盘命名管道复用连接（后续可选优化）。
- 自动把 Windows 环境变量或 PATH 传入会话；自动启动服务。
- `events`/`wait`（#68）的 Windows 版本。

## Decisions

### 0. 已与用户确认的决策

| # | 问题 | 结论 |
|---|---|---|
| D1 | 无法换算为 WSL 路径的当前目录 | 报错（退出码 2），提示 `--cwd`；不退回服务端默认目录，避免 agent 在意料之外的目录里运行 |
| D2 | 可执行文件名 | 构建产物 `webmux-windows-<arch>.exe`，文档建议放入 PATH 并重命名为 `webmux.exe` |
| D3 | 实施顺序 | #83 → #67 → 本变更 |

### 1. 代码组织

```
cmd/webmux-windows/
  main_windows.go       // 子命令分发、显式依赖组装
  main_other.go         // !windows：打印“仅支持 Windows”并退出 1
internal/wslclient/      // 平台无关，与托盘共用，Linux CI 可测
  target.go             // 发行版与 webmux 路径解析（参数 > 环境变量 > tray.json > 默认）
  bridge.go             // 子进程 stdio → net.Conn；退出码 3 → ErrNotRunning
  paths.go              // Windows 路径 → WSL 路径分类与换算
  *_windows.go          // wsl.exe 进程创建（CREATE_NO_WINDOW、WSL_UTF8=1）
internal/attach/         // #67 引入的共享附着核心（帧状态机、控制权、分离键、退出码），只依赖 localclient
  term_unix.go / term_windows.go   // 终端适配：原始模式、尺寸、恢复
```

`internal/attach` 若在 #67 中尚未拆出平台无关部分，本变更负责把它拆成“核心 + 终端适配”，并保证 `GOOS=windows go build ./internal/attach` 成功。

### 2. 路径换算

| 输入 | 处理 |
|---|---|
| `X:\...`（含相对路径先 `filepath.Abs`） | 经桥接前调用 `wsl.exe -d D --exec wslpath -a -u '<路径>'`，结果作为 cwd；遵守发行版 `wsl.conf` 的 `automount.root` |
| `\\wsl.localhost\<D>\p`、`\\wsl$\<D>\p`，且 D 与目标发行版同名（不区分大小写） | 直接换算为 `/p`（反斜杠转正斜杠） |
| 其他 UNC、其他发行版、设备路径 | 报错退出码 2 |
| `--cwd` 以 `/` 或 `~` 开头 | 原样交给服务端（`~` 由服务端 `ExpandPath` 处理） |

`wslpath` 只接收一个参数，路径作为单个 argv 元素，不经 shell。

### 3. 控制台适配（`term_windows.go`）

- 进入：保存输入/输出句柄的原模式与 `GetConsoleOutputCP`；输入设为 `ENABLE_VIRTUAL_TERMINAL_INPUT`（清除 `ENABLE_LINE_INPUT`、`ENABLE_ECHO_INPUT`、`ENABLE_PROCESSED_INPUT`），输出加 `ENABLE_VIRTUAL_TERMINAL_PROCESSING | DISABLE_NEWLINE_AUTO_RETURN`，`SetConsoleOutputCP(65001)`。
- 读取：`ReadFile` 读取 stdin 字节流（VT 输入模式下即 UTF-8 + 转义序列），交给附着核心；核心识别分离键 0x1D。
- 尺寸：每 250 ms `GetConsoleScreenBufferInfo` 读取窗口（`srWindow`）尺寸，变化时交给核心发送 0x01。
- 恢复：`defer` 恢复模式与代码页；`SetConsoleCtrlHandler` 捕获 `CTRL_BREAK_EVENT`/`CTRL_CLOSE_EVENT`，先恢复再退出。

### 4. 连接与桥接

`internal/localclient` 的 HTTP 客户端使用 `wslclient` 的拨号器；`run` 与 `attach` 各需要一条 WebSocket 连接，REST 请求复用一条 keep-alive 连接，因此一次 `run` 通常启动两到三个 `wsl.exe`。服务未运行（`ErrNotRunning`）映射为退出码 3；`webmux bridge` 不存在（旧版本）映射为退出码 1。

### 5. 构建与 CI

- `Makefile`：`release` 增加 `CGO_ENABLED=0 GOOS=windows GOARCH={amd64,arm64} go build -trimpath -ldflags "$(LDFLAGS)" -o bin/webmux-windows-<arch>.exe ./cmd/webmux-windows`（控制台子系统，不加 `-H=windowsgui`）。
- CI：现有 `go` 作业追加两个架构的交叉编译与 `GOOS=windows go vet ./cmd/webmux-windows ./internal/wslclient/... ./internal/attach/...`；`internal/wslclient` 的平台无关测试随 `go test -race ./...` 在 Ubuntu/macOS 运行。真实控制台与 WSL 行为由手动验收覆盖。

## Risks / Trade-offs

- [PowerShell 对原生程序参数的引号处理与 cmd.exe 不同，argv 可能与用户预期不一致] → 文档说明以 `--%` 或单引号保持字面值；`run --dry-run` 不在本变更范围，但 `--detach` 后可用 `ls --json` 检查 argv。
- [每条连接启动一次 `wsl.exe`（约 100–300 ms），首次附着延迟可感知] → 只在建立连接时发生；后续可用托盘命名管道复用（非本变更）。
- [WSL 冷启动时 `wsl.exe` 首次调用可能需要数秒] → 显示“正在连接 WSL…”提示（标准错误），不计入失败。
- [旧版 conhost 对 VT 输入支持不完整] → 要求 Windows 10 22H2+；推荐 Windows Terminal。
- [会话在服务重启时结束] → 与 WebMux 既有语义一致；附着中断时提示原因。

## Migration Plan

纯新增组件，无数据迁移。回滚：删除 `webmux.exe`。

## Open Questions

- 分离键是否需要可配置（`--detach-key`）随 #67 的决定保持一致。
