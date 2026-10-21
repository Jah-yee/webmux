## ADDED Requirements

### Requirement: 显式启动环境

会话创建请求 SHALL 接受可选的 `env` 对象（变量名到值）。服务端 SHALL 以服务进程环境为基础创建会话，并只追加或覆盖 `env` 中显式列出的变量（包括覆盖默认设置的 `TERM` 等）；SHALL NOT 复制调用方的其他环境。`env` SHALL NOT 出现在会话信息中。

变量名 SHALL 匹配 `[A-Za-z_][A-Za-z0-9_]*`，值 SHALL NOT 含 NUL；`WEBMUX_SESSION_ID` SHALL 被拒绝。不合法的 `env` SHALL 以 400 拒绝且不创建会话。

`webmux run --env NAME=VALUE`（可重复）SHALL 映射为该字段；格式不是 `NAME=VALUE` 时 SHALL 以退出码 2 结束；服务端不支持该字段时客户端 SHALL 报告服务端错误，SHALL NOT 静默忽略。

#### Scenario: 虚拟环境

- **WHEN** 用户执行 `webmux run --env VIRTUAL_ENV=/w/.venv --env PATH=/w/.venv/bin:/usr/bin -- python app.py`
- **THEN** 会话中 `PATH` 与 `VIRTUAL_ENV` 为给定值，其余变量来自服务进程环境

#### Scenario: 保留变量

- **WHEN** 请求的 `env` 包含 `WEBMUX_SESSION_ID`
- **THEN** 服务端返回 400，不创建会话
