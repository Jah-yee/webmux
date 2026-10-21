# terminal-session-cli Specification

## Purpose
TBD - created by archiving change add-terminal-run-attach. Update Purpose after archive.

## Requirements

### Requirement: 从终端创建会话

`webmux run [--name <名称>] [--cwd <目录>] [--env NAME=VALUE]... [--detach] -- <argv...>` SHALL 在本机 WebMux 中创建自定义命令会话：`--` 之后的参数 SHALL 原样作为 argv，SHALL NOT 经过 shell 展开。未指定 `--cwd` 时 SHALL 使用调用者的当前目录；`--cwd` 为相对路径时 SHALL 相对当前目录解析为绝对路径，以 `~` 开头时 SHALL 交由服务端展开。缺少命令时 SHALL 输出用法并以退出码 2 结束。

`--detach` 时 SHALL 只向标准输出写入一行 `<会话 ID>\t<本机会话链接>` 并以退出码 0 结束。默认 SHALL 立即附着该会话；附着前 SHALL 在标准错误输出会话 ID、本机会话链接与分离键提示，SHALL NOT 向标准输出写入额外内容。

本机会话链接 SHALL 为服务报告的本机链接加 `/s/<会话 ID>`，SHALL NOT 包含 Token。

#### Scenario: 参数不被 shell 展开

- **WHEN** 用户执行 `webmux run --detach -- printf '%s;$(id)' 'a b'`
- **THEN** 会话 argv 为 `["printf", "%s;$(id)", "a b"]`，`$(id)` 不被执行，标准输出为会话 ID 与链接

#### Scenario: 附着前告知链接

- **WHEN** 用户在 `/home/u/proj` 执行 `webmux run -- claude`
- **THEN** 会话以 cwd `/home/u/proj` 创建，标准错误先出现会话 ID 与 `http://127.0.0.1:7681/s/<ID>`，随后终端附着到该会话

### Requirement: 附着会话

`webmux attach <会话 ID> [--observe]` SHALL 只在标准输入与标准输出都是终端时附着，否则 SHALL 以退出码 2 结束。附着期间终端 SHALL 处于原始模式：键盘输入（含 `Ctrl+C`）原样发送给会话，会话输出原样写入终端；附着开始时 SHALL 先复位终端再回放会话缓冲。

附着 SHALL 默认取得会话控制权，并在终端尺寸变化后 250 毫秒内同步；`--observe` 时 SHALL NOT 发送输入、尺寸或取得控制权。浏览器与原生终端 SHALL 遵循同一套控制权规则：任一端输入即取得控制权。

按下 `Ctrl+]` 时 SHALL 只断开连接、不把该按键发送给会话，以退出码 0 结束；会话进程退出时 SHALL 以会话退出码结束；连接中断或会话不存在时 SHALL 提示原因并以退出码 1 结束。任一结束方式（含 SIGINT/SIGTERM/SIGHUP）SHALL 先恢复终端原有模式。

#### Scenario: 手机接管后回到原生终端

- **WHEN** 用户在原生终端附着运行 agent 的会话，按 `Ctrl+]` 分离，在手机浏览器中打开该会话并输入，然后再次执行 `webmux attach <ID>`
- **THEN** 三次操作作用于同一进程；重新附着后原生终端显示最新内容，输入即取得控制权，终端尺寸按原生终端更新

#### Scenario: 退出码透传

- **WHEN** 附着的会话进程以退出码 7 结束
- **THEN** 终端模式恢复，`webmux` 以退出码 7 结束

### Requirement: 列出与打开会话

`webmux ls` SHALL 以表格列出会话（ID、名称、状态与退出码、命令、启动目录）；`--json` 时 SHALL 向标准输出写入服务端会话列表的原始 JSON 数组。`webmux open <会话 ID>` SHALL 输出该会话的本机会话链接，`--browser` 时 SHALL 同时用默认浏览器打开；会话不存在时 SHALL 以退出码 1 结束。

#### Scenario: 机器可读列表

- **WHEN** 用户执行 `webmux ls --json`
- **THEN** 标准输出恰好是服务端会话列表 JSON，可直接交给 `jq`

### Requirement: 连接与错误约定

这些命令 SHALL 经本机套接字访问本机服务，SHALL NOT 需要或输出 Token，并 SHALL 接受 `--config <path>` 以与服务相同的规则定位服务。服务未运行时 SHALL 以退出码 3 结束并提示启动方式，SHALL NOT 隐式启动服务。诊断信息 SHALL 只写标准错误。

#### Scenario: 服务未运行

- **WHEN** 没有 WebMux 运行，用户执行 `webmux run -- claude`
- **THEN** 不创建任何进程，标准错误提示服务未运行与启动方式，退出码 3
