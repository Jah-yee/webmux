## MODIFIED Requirements

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

