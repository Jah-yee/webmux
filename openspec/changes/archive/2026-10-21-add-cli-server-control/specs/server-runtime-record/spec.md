## Purpose

让运行中的 WebMux 服务在本机留下一份可校验的运行时记录，使同一用户的客户端（CLI、托盘）无需猜测端口或进程名即可定位、识别和安全停止该实例。

## ADDED Requirements

### Requirement: 运行时记录内容与位置

服务端 SHALL 在成功开始监听之后、开始接受请求之前，在状态目录下的运行时子目录中写入运行时记录。记录 SHALL 为带版本号的 JSON，至少包含：记录格式版本、进程 PID、可用于识别 PID 复用的进程启动标识、WebMux 版本、实际监听地址（端口为 0 时为实际分配的端口）、配置文件路径（未使用配置文件时为空）、Token 是否配置及 Token 指纹、启动方、日志文件路径（未使用 `--log-file` 时为空）、本机 API 套接字路径（不可用时为空）、启动时间。

记录 SHALL NOT 包含 Token 明文。Token 指纹 SHALL 是单向摘要的截断值，只用于判断“配置中的 Token 是否与运行实例一致”。

运行时子目录权限 SHALL 为 0700，记录文件 SHALL 为 0600，并通过原子替换写入。

#### Scenario: 写入记录

- **WHEN** 用户以默认配置启动 `webmux`
- **THEN** 状态目录的运行时子目录中出现 0600 的记录文件，其中监听地址为 `127.0.0.1:7681`、PID 等于服务进程 PID，且文件内容不包含 Token

#### Scenario: 动态端口

- **WHEN** 用户以 `--listen 127.0.0.1:0` 启动
- **THEN** 记录中的监听地址为系统实际分配的端口，而不是 `0`

### Requirement: 启动方识别

服务端 SHALL 按以下优先级确定启动方并写入记录：环境变量 `WEBMUX_LAUNCHER` 的取值（仅接受 `windows-tray`）；存在 systemd 服务调用标识时为 `systemd`；作为 launchd 代理运行时为 `launchd`；否则为 `manual`。

`WEBMUX_LAUNCHER` SHALL NOT 出现在服务所创建会话的环境中。

#### Scenario: systemd 启动

- **WHEN** WebMux 由 `systemctl --user start webmux` 启动
- **THEN** 记录中的启动方为 `systemd`

#### Scenario: 启动方变量不泄漏到会话

- **WHEN** 托盘以 `WEBMUX_LAUNCHER=windows-tray` 启动 WebMux，用户在其中新建 shell 会话并执行 `env`
- **THEN** 输出中不包含 `WEBMUX_LAUNCHER`

### Requirement: 记录生命周期与单实例

服务端在其整个生命周期内 SHALL 持有运行时子目录中的独占文件锁。若锁已被另一个存活实例持有，后启动的实例 SHALL 照常提供服务，但 SHALL NOT 覆盖已有记录，并 SHALL 在日志中警告“状态目录已被另一实例使用，运行时记录未写入”。

服务端正常退出（含 SIGTERM/SIGINT）时 SHALL 在会话清理完成后删除自己写入的记录。异常终止遗留的记录 SHALL 能被读取方通过 PID 与进程启动标识判定为失效；下一个获得锁的实例 SHALL 覆盖失效记录。

#### Scenario: 正常退出

- **WHEN** 运行中的 WebMux 收到 SIGTERM 并完成退出
- **THEN** 运行时记录文件不再存在

#### Scenario: 遗留记录

- **WHEN** WebMux 被 SIGKILL 结束后，系统中另一个进程恰好复用了同一 PID
- **THEN** 读取方判定该记录失效，不把那个进程当作 WebMux；再次启动 WebMux 会覆盖该记录

#### Scenario: 同一状态目录两个实例

- **WHEN** 第一个实例运行中，用户以相同状态目录、不同端口再启动一个实例
- **THEN** 第二个实例正常提供终端服务，记录仍指向第一个实例，第二个实例日志中有警告

### Requirement: 日志文件输出

`webmux serve` SHALL 接受 `--log-file <path>`。指定后，服务的标准输出与标准错误（包括运行时崩溃信息）SHALL 写入该文件，SHALL NOT 再依赖启动方提供的输出管道；启动时若文件已存在，SHALL 先将其保留为同名 `.1` 文件（覆盖更早的 `.1`）。日志文件权限 SHALL 为 0600，父目录不存在时 SHALL 以 0700 创建。

未指定 `--log-file` 时行为保持不变（写标准错误）。日志文件中 SHALL NOT 出现 Token 或终端输入输出，与现有日志约定一致。

`--config` 与 `--log-file` 的值以 `~/` 开头时 SHALL 展开为当前用户主目录。

#### Scenario: 启动方的管道被关闭

- **WHEN** 以 `--log-file ~/.local/state/webmux/webmux.log` 启动 WebMux 后，启动它的父进程退出并关闭了原来的标准输出/标准错误
- **THEN** WebMux 继续运行并持续写入日志文件，不会因写入已关闭的管道而退出

#### Scenario: 日志轮转

- **WHEN** 日志文件已存在，用户再次启动 WebMux
- **THEN** 旧内容保留在 `webmux.log.1`，新的 `webmux.log` 只包含本次启动后的输出
