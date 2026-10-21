## Purpose

为同一用户的本机客户端（Linux/macOS CLI、经 WSL 桥接的 Windows 托盘与控制台客户端）提供不开放网络端口、按操作系统身份认证的本机 API：复用现有 HTTP/WebSocket 接口，并增加只在本机可用的状态、停止与取 Token 接口，使所有客户端只使用一种协议。

## ADDED Requirements

### Requirement: 本机套接字监听

服务端 SHALL 在开始接受 TCP 请求之前，于运行时子目录中创建 Unix 域套接字并监听本机 API；套接字文件权限 SHALL 为 0600，所在目录 SHALL 为 0700。只有持有运行时锁的实例 SHALL 创建该套接字；创建前遗留的同名套接字文件 SHALL 被删除。服务端正常退出时 SHALL 在会话清理完成后删除套接字文件。

套接字路径超过平台长度上限时，服务端 SHALL 改用 `$XDG_RUNTIME_DIR` 下由状态目录派生的固定路径；仍不可用时 SHALL 照常提供 TCP 服务、在日志中警告“本机 API 不可用”，并在运行时记录中留空套接字路径。

#### Scenario: 默认启动

- **WHEN** 用户以默认配置启动 WebMux
- **THEN** 运行时子目录中出现 0600 的套接字，运行时记录包含其路径；服务退出后套接字文件不存在

#### Scenario: 遗留套接字

- **WHEN** 上一次 WebMux 被 SIGKILL 结束，遗留了套接字文件，用户再次启动
- **THEN** 新实例删除旧文件并正常监听，客户端可以连接

### Requirement: 本机身份认证

本机套接字上的每个连接 SHALL 在处理任何请求之前校验对端进程的有效用户与服务端进程的有效用户相同；不同时 SHALL 立即关闭连接且不返回任何数据。

通过校验的连接 SHALL NOT 再要求 Token，SHALL NOT 执行 Host 与 Origin 校验（浏览器无法连接 Unix 域套接字）。TCP 监听的认证、Host 与 Origin 校验 SHALL 保持不变。

#### Scenario: 同一用户免 Token

- **WHEN** WebMux 配置了 Token，同一用户执行 `curl --unix-socket <套接字> http://webmux/api/sessions`
- **THEN** 返回会话列表，无需 Token

#### Scenario: 其他用户被拒绝

- **WHEN** 另一个系统用户（例如通过放宽的目录权限）连接该套接字
- **THEN** 连接被立即关闭，没有任何响应数据

### Requirement: 接口范围

本机套接字 SHALL 提供与 TCP 监听完全相同的 `/api/*` 与 `/ws/*` 接口（语义、请求与响应格式、WebSocket 帧均相同），并额外提供以下接口；`/local/*` SHALL NOT 在 TCP 监听上提供（返回 404）：

- `GET /local/v1/status`：返回状态对象，字段与 `cli-server-control` 的状态 JSON 契约 v1 相同（`record` 为 `true`），其中 `drift` 由服务端重新读取配置文件计算；
- `POST /local/v1/shutdown`：请求体为可选的 `{"force": bool}`；启动方为 `systemd` 或 `launchd` 且未设置 `force` 时 SHALL 返回 409 及对应的管理命令提示；否则 SHALL 返回 202，并按与收到 SIGTERM 相同的流程退出；
- `GET /local/v1/token`：配置了 Token 时返回 `{"token": "<Token>"}`，否则返回 404。

`/local/v1/*` 的响应 SHALL 为 JSON，错误使用现有 `{"error": "..."}` 格式；该接口版本内 SHALL 只增加可选字段，不兼容变更 SHALL 使用新的路径版本。服务端 SHALL NOT 在日志中记录 `/local/v1/token` 的响应内容。

#### Scenario: TCP 上不可用

- **WHEN** 客户端经 TCP 携带正确 Token 请求 `/local/v1/token`
- **THEN** 返回 404

#### Scenario: 本机停止

- **WHEN** 由用户手动启动的 WebMux 收到本机 `POST /local/v1/shutdown`
- **THEN** 返回 202，WebMux 清理会话后退出，运行时记录与套接字被删除

#### Scenario: systemd 实例拒绝停止

- **WHEN** 由 systemd 启动的 WebMux 收到不带 `force` 的本机停止请求
- **THEN** 返回 409，提示使用 `systemctl --user stop webmux`，服务继续运行

### Requirement: 会话内进程的限制

服务端 SHALL 识别来自 WebMux 会话内部的本机连接（对端进程属于某个会话的进程树，或在 Linux 上携带会话标识环境变量）。对这类连接，`POST /local/v1/shutdown` 与 `GET /local/v1/token` SHALL 返回 403；其余接口（包括创建会话、列出会话、附着终端）SHALL 照常可用。

macOS 无法读取其他进程环境，识别 SHALL 以进程树为准，并接受已脱离会话进程树的后台进程无法被识别这一限制。

#### Scenario: 会话中的 agent 取 Token

- **WHEN** WebMux 会话中运行的程序请求本机 `/local/v1/token`
- **THEN** 返回 403

#### Scenario: 会话中启动兄弟会话

- **WHEN** WebMux 会话中运行的程序通过本机套接字请求 `POST /api/sessions`
- **THEN** 新会话按正常规则创建
