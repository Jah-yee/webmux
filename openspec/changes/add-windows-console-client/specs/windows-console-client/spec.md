## Purpose

让 Windows 用户在 PowerShell / Windows Terminal 中直接启动 agent 并交由 WSL 中的 WebMux 管理，随时附着、分离和查看会话，体验与 Linux 上的 `webmux run/attach` 一致，且不在 Windows 侧处理 Token。

## ADDED Requirements

### Requirement: 形态与连接

`webmux.exe` SHALL 作为 Windows 控制台子系统程序提供，支持 Windows 10 22H2 及以上与 Windows 11 的 amd64、arm64，要求已安装 Microsoft Store 版 WSL 2，且 SHALL NOT 要求管理员权限或其他运行时。

客户端 SHALL 只通过目标发行版中 WebMux 的本机 API 与服务通讯：每条连接启动一次 `wsl.exe` 运行目标发行版中的 `webmux bridge`，并以其标准输入输出作为连接。客户端 SHALL NOT 需要、读取、显示或保存 Token，SHALL NOT 依赖 WSL 的 localhost 转发或网络模式。

目标发行版 SHALL 按以下顺序确定：`--distro` 参数、环境变量 `WEBMUX_DISTRO`、托盘设置中的发行版、WSL 默认发行版。WSL 内 `webmux` 的路径 SHALL 按 `--webmux` 参数、环境变量 `WEBMUX_WSL_PATH`、托盘设置、`~/.local/bin/webmux` 的顺序确定。

#### Scenario: 关闭 localhost 转发后仍可用

- **WHEN** 用户在 `.wslconfig` 中关闭 `localhostForwarding`，WebMux 在 WSL 中运行，用户在 PowerShell 执行 `webmux ls`
- **THEN** 正常列出会话

#### Scenario: 与托盘使用同一发行版

- **WHEN** 托盘设置的目标发行版为 `Debian`，用户未指定 `--distro` 与 `WEBMUX_DISTRO`
- **THEN** 客户端连接 `Debian` 中的 WebMux

### Requirement: 子命令与输出约定

客户端 SHALL 提供 `run`、`attach`、`ls`、`open`、`status`、`version` 子命令；未知子命令或参数错误 SHALL 输出用法并以退出码 2 结束。带 `--json` 的命令 SHALL 只向标准输出写入机器可读结果，诊断信息只写标准错误。

服务未运行时，除 `status` 与 `version` 外的命令 SHALL 以退出码 3 结束，并提示可通过托盘或在 WSL 中执行 `webmux serve` 启动；客户端 SHALL NOT 隐式启动服务。目标发行版中的 `webmux` 缺失或不支持本机 API 时 SHALL 以退出码 1 结束并说明原因。

#### Scenario: 服务未运行

- **WHEN** WSL 中没有运行 WebMux，用户执行 `webmux run -- claude`
- **THEN** 不创建任何进程，标准错误提示服务未运行及启动方法，退出码 3

### Requirement: 在 WSL 中启动 agent

`webmux run [--name <名称>] [--cwd <路径>] [--env K=V]... [--detach] -- <argv...>` SHALL 在目标发行版的 WebMux 中创建一个自定义命令会话：`--` 之后的参数 SHALL 原样作为 argv 传给服务端，SHALL NOT 经过任何 shell 展开（参数边界以 Windows 命令行解析后的结果为准）。会话由服务端管理，客户端断开或退出 SHALL NOT 结束会话。

未指定 `--cwd` 时，客户端 SHALL 把当前 Windows 工作目录换算为 WSL 路径：

- 盘符路径（如 `C:\work\app`）SHALL 按目标发行版的自动挂载规则换算（默认 `/mnt/c/work/app`）；
- 指向目标发行版自身文件系统的路径（`\\wsl.localhost\<发行版>\...` 或 `\\wsl$\<发行版>\...`）SHALL 换算为对应的 Linux 绝对路径；
- 其他路径（网络共享、其他发行版等）无法换算时，SHALL 以退出码 2 结束并提示使用 `--cwd` 指定 Linux 路径。

`--cwd` 的值以 `/` 或 `~` 开头时 SHALL 作为 Linux 路径原样使用，否则 SHALL 按上述规则换算。`--env` 只传递显式列出的变量，SHALL NOT 复制 Windows 环境；服务端不支持该字段时 SHALL 报错而不是忽略。

默认创建后 SHALL 立即附着（等同 `attach`）；`--detach` 时 SHALL 只输出会话 ID 与本机链接后以退出码 0 结束。

#### Scenario: 从 Windows 目录启动

- **WHEN** 用户在 `C:\work\app` 执行 `webmux run -- claude --resume`
- **THEN** WSL 中以 cwd `/mnt/c/work/app`、argv `["claude", "--resume"]` 创建会话，当前控制台随即附着

#### Scenario: 参数包含特殊字符

- **WHEN** 用户执行 `webmux run -- printf "%s;$(id)" "a b"`
- **THEN** 会话 argv 为 `["printf", "%s;$(id)", "a b"]`，`$(id)` 不被执行

#### Scenario: 无法换算的目录

- **WHEN** 用户在 `\\fileserver\share\proj` 执行 `webmux run -- bash`
- **THEN** 不创建会话，提示使用 `--cwd` 指定 Linux 路径，退出码 2

#### Scenario: 仅创建

- **WHEN** 用户执行 `webmux run --detach --name review -- codex`
- **THEN** 输出新会话 ID 与 `http://127.0.0.1:7681/s/<id>`，退出码 0，会话在 WSL 中继续运行

### Requirement: 在 Windows 控制台中附着

`webmux attach <id> [--observe]` SHALL 在标准输入与标准输出都是 Windows 控制台时，把控制台切换到原始输入与虚拟终端序列处理模式并附着会话：键盘输入原样发送到会话，会话输出原样写入控制台，附着期间控制台输出代码页 SHALL 为 UTF-8。标准输入不是控制台时 SHALL 拒绝并以退出码 2 结束。

附着时 SHALL 默认取得会话控制权；`--observe` 时只观察，不发送输入与尺寸。持有控制权时，控制台窗口尺寸变化 SHALL 在 250 毫秒内同步给会话。

按下分离键（默认 `Ctrl+]`）时 SHALL 只断开连接、不向会话写入该按键，会话继续运行，客户端以退出码 0 结束。会话进程退出时 SHALL 以会话的退出码结束；连接中断或服务停止时 SHALL 提示原因并以退出码 1 结束。在分离、会话退出、连接中断以及 `Ctrl+Break` 等可捕获的中断下，客户端 SHALL 先恢复原有的控制台模式与代码页再退出；附着期间 `Ctrl+C` SHALL 作为普通输入发送给会话。

#### Scenario: 分离后在手机接管

- **WHEN** 用户在 Windows Terminal 中附着运行 agent 的会话，按 `Ctrl+]`，再用手机浏览器打开该会话
- **THEN** 本地控制台恢复为正常 PowerShell 提示符，手机上可以继续操作同一进程

#### Scenario: 调整窗口大小

- **WHEN** 附着期间用户把 Windows Terminal 窗口从 120 列拉宽到 160 列
- **THEN** 250 毫秒内会话的终端宽度变为 160 列，全屏程序按新尺寸重绘

#### Scenario: 会话退出

- **WHEN** 附着的会话进程以退出码 7 结束
- **THEN** 控制台模式恢复，`webmux.exe` 以退出码 7 结束

### Requirement: 列出、打开与状态

`webmux ls` SHALL 列出会话（ID、名称、状态、命令、启动目录）；`--json` 时 SHALL 输出服务端会话列表的原始 JSON 数组。`webmux open <id>` SHALL 输出该会话的本机链接（`<本机链接>/s/<id>`，不含 Token），`--browser` 时 SHALL 同时用默认浏览器打开。`webmux status [--json]` SHALL 输出与 `webmux status --json` 契约 v1 相同的内容；服务未运行时 SHALL 改为在目标发行版中执行 `webmux status --json` 取得结果。

#### Scenario: 打开会话链接

- **WHEN** 用户执行 `webmux open abcdefgh --browser`
- **THEN** 标准输出为 `http://127.0.0.1:7681/s/abcdefgh`，默认浏览器打开该地址

### Requirement: 安全约束

客户端 SHALL NOT 在任何输出、日志或文件中出现 Token，SHALL NOT 记录终端输入输出。会话 argv 与 cwd SHALL 只以结构化字段传给服务端，SHALL NOT 拼接成 shell 字符串。客户端调用的所有 `wsl.exe` 进程 SHALL 以参数数组创建。

#### Scenario: 附着过程中不泄露 Token

- **WHEN** WebMux 配置了 Token，用户执行 `run`、`attach`、`ls`、`open`、`status --json`
- **THEN** 所有标准输出与标准错误中都不出现 Token
