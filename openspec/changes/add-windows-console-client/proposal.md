## Why

Windows 用户的日常终端是 PowerShell / Windows Terminal，而 WebMux 服务运行在 WSL 中。想“在当前终端里启动一个 agent，离开电脑后用手机接管，回来再接着用”（#67 的场景），目前必须先进入 WSL shell。提供一个 Windows 控制台客户端，让用户直接在 Windows 终端中启动 agent 并纳入 WSL 中 WebMux 的管理、随时附着与分离，与 Linux 上的 `webmux run/attach` 体验一致。

## What Changes

**前置依赖：`add-cli-server-control`（PR #83，本机 API 与 `bridge`）与 #67（Linux CLI 的 `run/attach/ls/open` 语义、会话创建的可选 `env` 字段与共享附着核心）。**

- 新增 Windows 控制台程序 `webmux.exe`（Windows amd64/arm64，控制台子系统，只含客户端功能），子命令 `run`、`attach`、`ls`、`open`、`status`、`version`。
- 连接方式：每条连接启动一次 `wsl.exe --exec webmux bridge`，经 WSL 内 WebMux 的本机 API 访问，**不需要、不读取、不保存 Token**，也不依赖 WSL 的 localhost 转发或网络模式。目标发行版与 WSL 内 `webmux` 路径可由参数、环境变量或托盘设置决定。
- `run [--name] [--cwd] [--env K=V]... [--detach] -- <argv...>`：在 WSL 中创建会话运行 argv（不经 shell 展开），默认立即附着；当前 Windows 目录自动换算为 WSL 路径，无法换算时报错并提示使用 `--cwd`。
- `attach <id> [--observe]`：在 Windows 控制台中以原始模式附着会话，支持窗口尺寸跟随、`Ctrl+]` 分离、会话退出码透传，退出时恢复控制台状态。
- `ls [--json]`、`open <id> [--browser]`（只给不含 Token 的本机链接）、`status [--json]`（与 `webmux status --json` v1 同一契约）。
- 服务未运行时明确报错，不隐式启动服务（启动交给托盘或 `webmux serve`）。
- 构建：`make release` 额外产出 `webmux-windows-{amd64,arm64}.exe`。

## Capabilities

### New Capabilities

- `windows-console-client`: Windows 控制台客户端的形态与连接、目标选择、子命令与输出约定、`run` 的 argv/cwd/env 语义、`attach` 的控制台处理、`ls`/`open`/`status`、安全约束。

### Modified Capabilities

当前 `openspec/specs/` 无已登记主规格。本变更只消费 `add-cli-server-control` 的本机 API / `bridge` 与 #67 的会话创建与附着协议，不改变其要求。

## Impact

- 新增 `cmd/webmux-windows/`（Windows 入口；非 Windows 构建为打印不支持并退出的占位）。
- 新增 `internal/wslclient/`（平台无关：目标发行版与 `webmux` 路径解析、读取托盘设置、桥接拨号器、Windows 路径 → WSL 路径换算规则），与 `add-windows-wsl-tray` 共用；Windows 专属部分（控制台模式、窗口尺寸、打开浏览器）在 `//go:build windows` 文件中。
- 复用 `internal/contract`、`internal/localclient`（#83）与 #67 的共享附着核心（帧处理、控制权、分离键、退出码），要求其可交叉编译到 Windows。
- 依赖：`golang.org/x/sys/windows`（已有模块）；不新增第三方依赖。
- `Makefile` 的 `release` 增加两个 Windows 目标；CI 增加交叉编译与 `GOOS=windows go vet`。
- 文档：`docs/usage.md` 增加“在 Windows 终端中使用”一节。
- 不涉及服务端、前端与协议的修改。
