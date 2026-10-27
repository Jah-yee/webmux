## MODIFIED Requirements

### Requirement: 定位与查看生效配置

`webmux config path` SHALL 报告配置路径及是否存在。`config show` SHALL 默认只读解析磁盘 TOML 与专用覆盖，输出 view configured、字段值/来源和各存储健康摘要；SHALL NOT 初始化、加锁、chmod 或修复文件。`config show --live` SHALL 通过同 UID socket 读取已验证运行实例的内存快照，输出 view effective、实例身份、实际监听地址与观察时间；不可达 SHALL 报错且不回退磁盘。两者 SHALL 支持人类输出及单个 JSON 对象，不输出凭据正文、启动命令/env、推送订阅或快捷指令正文。

#### Scenario: 显式默认值

- **WHEN** TOML 显式写入与默认值相同的值
- **THEN** 字段来源仍为 toml

#### Scenario: 运行后修改磁盘

- **WHEN** 操作者修改 TOML 后尚未重启
- **THEN** 默认查看新磁盘值，--live 查看进程已采用的旧值

#### Scenario: 活进程不可达

- **WHEN** 使用 --live 但没有可验证实例或 socket 不可达
- **THEN** 退出非零且不输出 configured 冒充 effective

#### Scenario: 无配置文件

- **WHEN** 默认配置文件不存在且用户运行 config show
- **THEN** 输出默认配置和 default 来源，不创建配置文件或状态目录

#### Scenario: 不输出 Token

- **WHEN** 配置或运行进程包含登录 token 或隧道 token
- **THEN** 诊断仅报告凭据是否存在，不输出凭据内容
