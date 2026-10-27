## ADDED Requirements

### Requirement: Configured and effective views

诊断 SHALL 明确区分磁盘 configured 与运行进程 effective。前者 SHALL 只读解析，不创建目录、加写锁、初始化或修复文件；后者 SHALL 经现有同 UID 本机 socket 查询并提供实例身份与观察时间。CLI 默认选择在实施前确认，不改变本语义。

#### Scenario: Disk changes after startup

- **WHEN** 用户修改 TOML 或磁盘状态但进程尚未采用
- **THEN** configured 反映磁盘，effective 仍报告进程实际采用值
- **AND** 两者均明确视图类型，不把磁盘结果称为活进程状态

#### Scenario: Live instance unavailable

- **WHEN** 请求 effective 但 socket 不可达或实例身份无效
- **THEN** 命令失败并报告原因，不回退到磁盘伪装为有效配置

#### Scenario: Read-only inspection of missing state

- **WHEN** configured 检查缺失或损坏文件
- **THEN** 报告 missing/损坏状态，不产生任何文件或权限修改

### Requirement: Field-level provenance

诊断 SHALL 基于真实解析元数据输出字段来源，至少包括 default、toml、web、cli、generated；安全降级 SHALL 单独标记状态和原因。来源不得通过值是否等于默认值来猜测。

#### Scenario: Explicit default-valued setting

- **WHEN** TOML 显式写入恰好等于默认值的 false、0 或字符串
- **THEN** 来源仍为 toml，不能显示 default

#### Scenario: Security override unavailable

- **WHEN** 磁盘或进程中的访问覆盖因存储错误被关闭
- **THEN** 显示关闭后的安全状态和原因，而不是声称 TOML 的启用值已经生效

### Requirement: Allowlisted diagnostic data

人类输出、JSON、日志和来源摘要 SHALL 使用安全 allowlist DTO。token、VAPID 私钥、订阅鉴权材料、完整端点、启动 env 值与快捷指令正文 MUST 不出现在诊断中；凭据只能输出 configured 等布尔元数据。

#### Scenario: Secret-bearing corruption

- **WHEN** 解码错误涉及带秘密的 JSON 内容
- **THEN** 错误只包含安全 kind、操作和受控路径，不包含原始错误中的字段值或原文

#### Scenario: New configuration field

- **WHEN** 新增配置字段或诊断分组
- **THEN** 必须声明来源与敏感性，并增加来源/脱敏测试；不能直接序列化内部配置对象
