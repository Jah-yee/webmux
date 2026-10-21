## Why

#67：用户希望在本机或 SSH shell 中照常启动长任务、继续使用当前终端，离开电脑后用手机接入，回来再从原生终端接续同一个 PTY。WebMux 目前以网页为创建与操作会话的入口；`add-cli-server-control` 已提供本机 API，`add-windows-console-client` 为 Windows 先实现了共享的附着核心与 `run/attach/ls/open`，但这组终端入口在 Linux/macOS 上的行为、会话启动环境的约定都还没有自己的规格，`run` 默认附着时也不告诉用户会话 ID 与链接，用户无法在离开前把链接带到手机上。

## What Changes

- 为 `webmux run|attach|ls|open` 建立规格（Linux/macOS 经本机套接字访问本机服务；Windows 控制台客户端沿用同一语义，见 `add-windows-console-client`）。
- `webmux run` 默认附着前，在标准错误输出会话 ID、本机链接与分离键提示；`--detach` 时标准输出仍为 `<ID>\t<链接>`。
- 为 `POST /api/sessions` 的可选 `env` 字段建立规格：只为该会话追加或覆盖显式变量，不复制调用方环境。
- 子命令分发增加 `run`、`attach`、`ls`、`open`。

暂不包含（后续另立变更）：创建结果的 JSON 输出；本机套接字不可用时退回 TCP + Token。

## Capabilities

### New Capabilities

- `terminal-session-cli`: 从终端创建、附着、列出与打开 WebMux 会话的命令行为、附着语义（控制权、尺寸、分离、退出码、终端恢复）与输出约定。
- `session-launch-environment`: 会话创建请求中显式启动环境的契约与校验。

### Modified Capabilities

- `cli-server-control`: “子命令分发与兼容”增加 `run`、`attach`、`ls`、`open`。

## Impact

- `cmd/webmux/sessions.go`、`internal/sessioncmd`（`run` 附着前的提示）、`internal/attach`、`internal/session/env.go`、`internal/api/router.go`。
- 文档：`docs/usage.md`“从终端启动与附着”。
- 无新依赖；TCP 与浏览器行为不变。
