## ADDED Requirements

### Requirement: Reviewed inventory coverage

仓库 SHALL 有真实存储清单，覆盖文件、锁、所有者、故障行为与网页可写边界，并与文档表格机械比较。尚未实现的 OpenSpec 存储 MUST 不冒充现有条目。非服务器状态 SHALL 以有原因的精确引用登记，而非目录级排除。

#### Scenario: Added persistence reference

- **WHEN** 生产 Go 源码新增未登记 JSON/锁名字或受监控文件 I/O 引用
- **THEN** make persistence-check 与 CI 失败，指出具体文件、函数或路径及登记要求

#### Scenario: Existing helper grows

- **WHEN** 已登记函数增加一次相同写函数引用
- **THEN** 引用计数变化导致失败，不能因函数已经被允许而静默放行

#### Scenario: Removed implementation or stale documentation

- **WHEN** 删除了存储源码引用，或文档遗漏/多出/改变清单行
- **THEN** 守卫失败，拒绝积累无效登记

### Requirement: Guard regression fixtures

守卫 SHALL 有负向样例覆盖新增/删除引用、导入别名、函数值、受监控包点导入、平台文件、缺失所有者、缺失理由、数据/锁名漏登和文档漂移。测试 SHALL 不依赖当前主机的 GOOS 过滤其他平台源文件。

#### Scenario: Writer alias on another platform

- **WHEN** Windows 源文件把 os.WriteFile 赋给变量或通过导入别名引用
- **THEN** Linux 上运行的守卫也识别该引用并要求登记

### Requirement: Contract and failure coverage after consolidation

统一存储实施后，每个 descriptor SHALL 对应共享契约适配器，验证权限、有界严格读取、跨进程锁、损坏原文保留、提交前/后失败、删除同步失败与重新打开状态。仅有清单守卫 MUST 不被声明为证明运行时安全。

#### Scenario: Descriptor lacks contract tests

- **WHEN** 新注册存储没有契约测试适配器
- **THEN** CI 失败，并指出缺失的 store ID

#### Scenario: Fault injection after rename

- **WHEN** 测试注入 rename 后目录同步失败
- **THEN** 断言结果为 commit_uncertain、后续写入被禁止、没有成功广播且重新打开可诊断

### Requirement: Mandatory CI entry

持久化守卫 SHALL 通过 make persistence-check、make check、标准 Go 测试与现有 CI 必跑 job 执行，不因 PR 是 draft 而跳过。

#### Scenario: CI detects inventory drift

- **WHEN** PR 含未登记持久化引用
- **THEN** CI 必需结果失败，开发者必须登记真实能力或移除违规入口
