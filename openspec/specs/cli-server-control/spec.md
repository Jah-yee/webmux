# cli-server-control Specification

## Purpose
为 `webmux` 提供面向客户端的服务控制命令：在不改变现有启动方式的前提下，以稳定的退出码和带版本号的 JSON 契约报告服务状态与访问链接、安全地停止本机实例，并把本机 API 桥接到标准输入输出，供脚本、托盘、Windows 控制台客户端及后续 CLI 复用。

## Requirements

### Requirement: 子命令分发与兼容

`webmux` SHALL 按第一个参数分发：

- 无参数，或第一个参数以 `-` 开头：启动服务，行为与本变更前完全相同；
- `serve`：启动服务，接受与无子命令时相同的参数，并额外接受 `--log-file`；
- `version`：输出版本，行为不变；
- `status`、`stop`、`bridge`：见下文；
- `config`：见 `cli-config`；
- `run`、`attach`、`ls`、`open`：见 `terminal-session-cli`；
- 其他值：向标准错误输出“未知子命令”与可用子命令列表，退出码 2，SHALL NOT 启动服务。

客户端子命令 SHALL 接受 `--config <path>`，配置解析规则（默认路径、缺失文件、显式路径不存在即失败、默认值）SHALL 与服务启动完全一致；客户端据此确定状态目录，进而定位运行时记录与本机套接字。

#### Scenario: 现有 systemd 模板不受影响

- **WHEN** 以 `webmux --config ~/.config/webmux/config.toml` 启动（现有 `deploy/webmux.service` 的写法）
- **THEN** 服务正常启动，与本变更前行为一致

#### Scenario: 未知子命令

- **WHEN** 用户执行 `webmux statsu`
- **THEN** 标准错误提示未知子命令及可用列表，退出码 2，没有进程监听端口

### Requirement: 运行状态判定

`webmux status` SHALL 优先连接本机套接字并请求 `/local/v1/status`，成功即以其结果为准。本机 API 不可用时 SHALL 退回：读取并校验运行时记录（记录存在且格式受支持、PID 存活、进程启动标识一致），再用记录中的实际地址发起 TCP 健康检查（配置了 Token 时携带认证）；没有有效记录时 SHALL 对配置中的监听地址做一次健康检查，以识别没有本机 API 与记录的实例（例如旧版本），并报告为“运行中、无记录”。单次健康检查与本机请求的超时 SHALL NOT 超过 3 秒。

`status` SHALL NOT 修改任何文件，包括失效的记录。

#### Scenario: 运行中

- **WHEN** WebMux 以默认配置运行且有 2 个会话，用户执行 `webmux status`
- **THEN** 输出运行中、2 个会话、版本、PID、启动方与本机链接，退出码 0

#### Scenario: 未运行

- **WHEN** 没有 WebMux 在运行且没有遗留记录
- **THEN** 输出未运行，退出码 3

#### Scenario: 本机 API 无响应

- **WHEN** 记录指向的 WebMux 进程存活，但本机请求与 TCP 健康检查都超时
- **THEN** 输出“进程存在但无响应”及原因，退出码 1

### Requirement: 状态 JSON 契约

`webmux status --json` SHALL 向标准输出写入恰好一个 JSON 对象，错误诊断只写标准错误。对象 SHALL 包含字段 `v`（契约版本，本变更为 `1`）、`state`（`running` / `stopped` / `unresponsive` / `error`）、`record`（是否来自有效运行时记录或本机 API）、`localApi`（是否经本机 API 获取）、`pid`、`version`、`launcher`、`listen`、`sessions`（健康时的会话数，否则为 `null`）、`health`（`ok` / `unauthorized` / `refused` / `timeout` / `not-webmux` / `error` 之一及可选 `message`）、`urls`（`local` 字符串与 `remote` 数组）、`tokenConfigured`、`drift`（配置与运行实例不一致的字段名数组，取值限于 `listen`、`token`）、`warnings`、`config`（`path`、`exists`、`error`）、`logFile`、`socket`。

字段语义在同一契约版本内 SHALL 保持向后兼容：只增加可选字段，不删除或改变既有字段含义；不兼容变更 SHALL 提升 `v`。

只有传入 `--reveal-token` 且配置了 Token 时，对象 SHALL 额外包含 `token` 字段（经本机 API 获取；本机 API 不可用时读取配置文件）；其他任何输出（含人类可读输出与标准错误）SHALL NOT 包含 Token。

#### Scenario: 默认不泄露 Token

- **WHEN** 配置了 Token，用户执行 `webmux status --json`
- **THEN** 输出含 `"tokenConfigured": true`，不含 `token` 字段，也不含 Token 字符串

#### Scenario: 配置已修改但未重启

- **WHEN** 运行中实例监听 `127.0.0.1:7681`，用户把配置改成 `127.0.0.1:7700` 且尚未重启
- **THEN** `drift` 为 `["listen"]`，`listen` 与 `urls` 仍对应运行中实例的 `127.0.0.1:7681`

### Requirement: 链接推导

状态中的链接 SHALL 使用 `http`、不带路径、查询参数或 Token，端口取实际监听端口：

- 本机链接：监听回环地址或 `localhost` 时主机为 `127.0.0.1`（IPv6 回环为 `[::1]`）；监听通配地址（`0.0.0.0`、`::` 或空主机）时为 `127.0.0.1`；监听具体非回环地址时为该地址。
- 远程链接：监听具体非回环地址时为该地址一项；监听通配地址时为服务所在主机所有处于启用状态的非回环、非链路本地 IPv4 地址，每项标注网卡名与类型（`tailscale`（100.64.0.0/10）或 `lan`），Tailscale 排在最前；监听回环地址时为空数组。在 WSL 的 NAT 网络模式下，WSL 虚拟网卡的地址 SHALL NOT 作为远程候选（局域网设备无法直接访问）。
- 监听通配地址且未配置 Token 时，`warnings` SHALL 包含暴露警告。

本机 API 与退回路径 SHALL 使用同一推导规则，结果一致。

#### Scenario: 通配监听

- **WHEN** `listen = "0.0.0.0:7681"`，主机有 `tailscale0` 100.64.0.5 与 `eth0` 192.168.1.20
- **THEN** 本机链接为 `http://127.0.0.1:7681`，远程链接依次为 `http://100.64.0.5:7681`（tailscale）与 `http://192.168.1.20:7681`（lan）

#### Scenario: 回环监听

- **WHEN** `listen = "127.0.0.1:7681"`
- **THEN** 远程链接为空数组

### Requirement: 停止服务

`webmux stop` SHALL 优先通过本机 API 请求停止（`--force` 对应请求体 `force`），随后等待记录中的进程退出（默认 15 秒，可用 `--timeout` 调整）。本机 API 不可用或无响应时，SHALL 退回到对通过“运行状态判定”校验的进程发送 SIGTERM；任一路径在超时后 SHALL 发送 SIGKILL 并报告“已强制结束”。

没有有效记录且本机 API 不可用时 SHALL NOT 向任何进程发送信号；没有运行中实例时 SHALL 报告“未运行”并以退出码 0 结束（幂等）。启动方为 `systemd` 或 `launchd` 时，未传 `--force` 的 `stop` SHALL 拒绝并提示对应的管理命令，退出码 1；退回到信号路径时也 SHALL 遵守同一规则。

`stop --json` SHALL 输出一个 JSON 对象，包含 `v`、`result`（`stopped` / `killed` / `not-running` / `refused` / `error`）与可选 `message`。

#### Scenario: 正常停止

- **WHEN** 由用户手动启动的 WebMux 有 3 个会话，执行 `webmux stop`
- **THEN** WebMux 清理会话后退出，命令输出已停止，退出码 0，运行时记录与套接字被删除

#### Scenario: 服务卡死

- **WHEN** WebMux 进程存活但本机 API 无响应，执行 `webmux stop`
- **THEN** 命令退回到发送 SIGTERM，15 秒后仍未退出则强制结束

#### Scenario: systemd 实例

- **WHEN** WebMux 由 systemd 用户服务启动，执行 `webmux stop`
- **THEN** 命令拒绝停止并提示 `systemctl --user stop webmux`，进程继续运行

#### Scenario: 无记录时不误杀

- **WHEN** 配置端口上有一个既无本机 API 也未写记录的 WebMux（旧版本），执行 `webmux stop`
- **THEN** 命令报告找不到可停止的实例，不向任何进程发送信号，退出码 1

### Requirement: 本机 API 桥接

`webmux bridge` SHALL 连接本机套接字，并在标准输入与套接字、套接字与标准输出之间原样双向复制字节，直到任一方向结束；它 SHALL NOT 解析、修改或记录所中转的内容，也 SHALL NOT 向标准输出写入任何其他数据。一次 `bridge` 调用只承载一条连接。

无法连接时（服务未运行或本机 API 不可用），`bridge` SHALL 在向标准输出写入任何字节之前退出，向标准错误输出原因，并以退出码 3（未运行）或 1（其他错误）结束。标准输出被关闭时 `bridge` SHALL 正常退出，而不是因 SIGPIPE 异常终止。

套接字的身份校验对桥接同样适用：桥接进程以调用者身份连接，`bridge` 本身 SHALL NOT 提升或改变权限。

#### Scenario: 经桥接请求状态

- **WHEN** 客户端启动 `webmux bridge`，向其标准输入写入一个 `GET /local/v1/status` HTTP/1.1 请求
- **THEN** 标准输出返回完整的 HTTP 响应，连接保持可复用，可继续发送下一个请求

#### Scenario: 经桥接附着终端

- **WHEN** 客户端经 `bridge` 发起 `/ws/s/{id}` 的 WebSocket 升级
- **THEN** 升级成功，终端帧双向传递，与 TCP 连接上的行为一致

#### Scenario: 服务未运行

- **WHEN** 没有 WebMux 运行，客户端启动 `webmux bridge`
- **THEN** 标准输出没有任何字节，标准错误说明服务未运行，退出码 3
