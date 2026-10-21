## Context

`add-cli-server-control` 让同一用户的进程经 Unix 套接字免 Token 访问本机 API；`add-windows-console-client` 已落地共享附着核心 `internal/attach`（控制权、250 ms 尺寸同步、`Ctrl+]` 分离、退出码透传、快照回放）与共享命令实现 `internal/sessioncmd`，Linux CLI 通过 `cmd/webmux/sessions.go` 暴露同一组命令。本变更为这些已存在的行为补上规格，并补齐 #67 剩余的“返回会话 ID 和可访问地址”。

## Goals / Non-Goals

**Goals:**

- 原生终端与浏览器附着同一会话，沿用现有观察、控制权交接与尺寸规则；分离后会话继续运行，重新附着可继续操作。
- 参数边界、启动目录与环境的处理可解释，链接不含 Token。

**Non-Goals:**

- 创建结果的 JSON 输出（`run --json`）；本机套接字不可用时退回 TCP + Token。
- 接管非 WebMux 管理的进程；跨主机的目录与环境约定；修改 shell rc 或复制整套环境。

## Decisions

### 1. 连接

Linux/macOS 只经本机套接字（`webmux bridge` 同一路径的直连），服务未运行时退出码 3 并提示启动方式，不隐式启动服务。会话内的进程也可以使用这些命令（`add-cli-server-control` 允许会话内进程创建与附着会话）。

### 2. `run` 附着前的提示

默认附着时，创建成功后、进入原始模式前向**标准错误**写一行 `webmux: session <ID> · <本机链接> · Ctrl+] to detach`；标准输出不写入任何内容，避免干扰被附着程序的输出与脚本。链接来自 `/local/v1/status` 的本机链接加 `/s/<ID>`，与 `open` 相同。

### 3. 附着语义

附着时把本机终端切换为原始模式，默认取得控制权（发送接管帧后发送当前尺寸），之后每 250 ms 检查尺寸变化；收到服务端为本连接单独发送的控制帧时重发尺寸。首次回放前写入终端复位序列，再写入快照，等同浏览器的 `reset()`。`Ctrl+]` 只断开连接；会话退出帧给出退出码；连接中断退出码 1。`--observe` 不发送输入与尺寸、不取得控制权。

### 4. 启动环境

`env` 是名称到值的对象。名称须匹配 `[A-Za-z_][A-Za-z0-9_]*`，值不含 NUL，`WEBMUX_SESSION_ID` 保留（进程清理依赖它）。显式变量覆盖服务环境与默认变量（如 `TERM`），按名称排序追加；不出现在 `SessionInfo` 中。旧服务端以“未知字段”拒绝，客户端据此报错而不是静默忽略。

## Risks / Trade-offs

- [附着开始时终端复位会清屏] → 与 tmux 附着一致；分离后历史仍可在浏览器或 `buffer` 下载中查看。
- [会话环境来自服务进程，与调用 shell 不同] → 文档说明，并提供 `--env` 显式传递必要变量。

## Migration Plan

纯新增；不需要迁移。
