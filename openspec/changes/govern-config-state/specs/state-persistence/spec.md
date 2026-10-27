## ADDED Requirements

### Requirement: Registered storage boundaries

系统 SHALL 区分 TOML 引导安全项、普通偏好与领域状态。每个持久化存储 MUST 有稳定 ID、路径、版本、大小上限、所有者、失败策略和敏感性定义；锁路径由完整数据文件名追加 `.lock` 派生。安全访问覆盖 MUST 不进入通用偏好。

#### Scenario: New store registration

- **WHEN** 开发者添加新的领域持久化
- **THEN** 必须通过已注册 typed handle 获取读写能力，并绑定共享契约测试适配器
- **AND** 未登记的路径或缺失适配器使 CI 失败

### Requirement: Private bounded strict documents

持久文件和锁 SHALL 为 0600，所属应用目录 SHALL 为 0700；读取 SHALL 在 MaxBytes+1 字节内停止，拒绝符号链接、非普通文件、重复键、未知或大小写不匹配字段、非法 UTF-8、额外 JSON 值、错误版本、过深结构和非法领域值。

#### Scenario: Corrupt or oversized file

- **WHEN** 已有文件损坏、版本不支持或超过限制
- **THEN** 不修改、改名、删除或自动重建该文件，报告结构化错误而不回显数据

#### Scenario: Dangling link

- **WHEN** 数据路径是悬空符号链接
- **THEN** 不将其视为首次安装，不初始化覆盖

### Requirement: Independent lifetime locks

可变领域存储 SHALL 在读取前取得独立非阻塞排他锁并持有到 Close；锁文件 SHALL 不因数据替换或 Close 被删除。Close SHALL 幂等，关闭后的写入 SHALL 失败。

#### Scenario: Competing process

- **WHEN** 第二个进程尝试取得相同领域锁
- **THEN** 不读取或修改被占用存储，报告 locked
- **AND** 按该领域失败策略隔离故障；runtime 与 Tailscale 的特殊边界必须独立测试

### Requirement: Explicit commit outcomes

写入和删除 SHALL 区分 not_committed、committed 与 commit_uncertain。提交 SHALL 同步文件、原子发布、同步父目录；只有 committed 才能发布成功事件和并发修订。

#### Scenario: Failure before publication

- **WHEN** 校验、临时写入、文件同步、关闭或发布前失败
- **THEN** 保留上次确认的内存与磁盘，不发送成功事件

#### Scenario: Directory sync fails after publication

- **WHEN** rename 或 unlink 已完成但父目录同步失败
- **THEN** 标记 commit_uncertain，禁止后续写入，不声称旧值已回滚
- **AND** 重启重新读取后才恢复可用状态

### Requirement: Preserve data and fail closed by domain

push SHALL 在损坏时保留文件并禁用推送；只有确认文件不存在才允许初次生成密钥。安全入口覆盖错误 SHALL 不回落到启用入口的 TOML 默认值；无法证明遗留 Tailscale 入口安全时 SHALL 在绑定监听前拒绝启动。

#### Scenario: Corrupt push key

- **WHEN** push.json 的密钥或订阅结构无效
- **THEN** 不生成新密钥，不丢弃订阅，推送不可用而终端继续

#### Scenario: Uncertain ownership

- **WHEN** Tailscale 所有权记录不可读或外部入口不能验证
- **THEN** 不删除未验证入口，不继续可能暴露旧目标的启动流程

### Requirement: Breaking layout without silent exposure

新布局 SHALL 不提供旧文件兼容双读/双写或自动迁移。旧 tailscale.json 存在时 SHALL 阻止启动并给出先撤销或核实旧入口的指引；旧 tunnel.json 存在时 SHALL 阻止该入口回落启用。

#### Scenario: Both ownership filenames exist

- **WHEN** 旧 tailscale.json 与新 tailscale-ownership.json 同时存在
- **THEN** 不自动选择或合并，在监听前拒绝启动并保留两份文件

#### Scenario: Old disabled tunnel override

- **WHEN** 旧 tunnel.json 存在且 TOML 默认启用隧道
- **THEN** 不忽略旧覆盖而重新启用，报告 legacy_layout 并保持入口关闭
