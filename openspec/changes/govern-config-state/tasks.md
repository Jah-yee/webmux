## 1. 调查与可运行守卫

- [x] 1.1 对照主分支与 issue，记录现有存储、错误策略、运行时发现边界与尚未实现能力。
- [x] 1.2 调查 XDG、fsync/flock、SQLite 故障测试、Go JSON 与 Tailscale 生命周期原始资料，写入 research.md。
- [x] 1.3 记录用户选择：统一 JSON、push 保留损坏文件、旧所有权阻止启动；后续授权扩展为完整实现与 #119。
- [x] 1.4 新增全平台 AST 引用守卫、精确清单、文档对照及故意违规样例。
- [x] 1.5 接入 make persistence-check / make check / CI，增加 US-L10 与文档导航。
- [x] 1.6 执行 OpenSpec 严格校验、定向 Go race、文档守卫、格式与故事索引检查并记录真实结果。
- [x] 1.7 落实用户确认：config show 默认磁盘，--live 读取进程。

## 2. 统一基础设施

- [x] 2.1 在 statefile 定义 descriptor、typed handle、故障 kind 与提交阶段；登记共享层边界。
- [x] 2.2 实现生命周期 flock、0600/0700、符号链接拒绝、有界严格读取；区分不存在与损坏。
- [x] 2.3 实现提交与删除的强制目录同步；commit_uncertain 禁止后续写，不回滚虚构状态。
- [x] 2.4 实现纯只读 ReadSnapshot，不加写锁、不初始化/恢复、不 chmod。
- [x] 2.5 建立 descriptor 与契约适配器一一对应测试；注入各写入/删除阶段失败并重新打开验证。
- [x] 2.6 Linux 原生运行跨进程锁、权限、严格解析与故障测试；macOS 由 PR 原生 CI 验证，记录文件系统保证边界。

## 3. 领域迁移与命名

- [x] 3.1 迁移 launch/repositories；移除原始锁与写入口登记，保留领域验证与故障隔离。
- [x] 3.2 迁移 push，取消损坏改名重建；验证原文件、密钥和订阅保留以及正常缺失初始化。
- [x] 3.3 迁移 tunnelsettings/tailscalesettings，覆盖 Save/Reset、关闭未持久化、Funnel 确认与旧覆盖 fail-closed。
- [x] 3.4 迁移 Tailscale ownership，持久确认 pending 后才执行外部操作；验证 active、撤销和启动前对账。
- [x] 3.5 实现破坏性文件/锁改名与旧 tailscale.json 阻止启动；测试新旧并存、损坏、不可读、无匹配入口和未验证入口。
- [x] 3.6 runtime 采用统一写原语，保留发现记录与 TCP 降级边界；不把领域锁策略误套为全局拒绝启动。
- [x] 3.7 同步真实清单、受影响能力 delta 与 US-K08/US-N/US-P 等已有故事；新增行为另补稳定 ID。

## 4. 配置来源与诊断

- [x] 4.1 经用户确认 CLI 默认后，实现字段存在性元数据与共享字段投影与只读领域解析。
- [x] 4.2 实现 configured 只读模式与 effective 同 UID socket 快照；声明实例身份与分组观察时间。
- [x] 4.3 增加安全 DTO、contract/wire、本机客户端和 CLI 人类/JSON 输出；API 不直接读盘。
- [x] 4.4 测试显式默认值、CLI 覆盖、生成值、web 覆盖、磁盘漂移、活进程不可达、锁占用与安全降级。
- [x] 4.5 以字段覆盖守卫及成功/损坏/不可达秘密哨兵测试验证显式安全 DTO；错误不附原始 JSON 内容。
- [x] 4.6 同步 CLI 契约、用法与相关故事；如另做网页来源 UI，提交真实截图和浏览器测试。

## 5. 完整偏好同步（本 PR 一并实施 #119）

- [x] 5.1 更新 add-server-settings-sync 的锁名、提交不确定语义、共享限长读取及无兼容迁移约定。
- [x] 5.2 settings 服务通过 descriptor/typed handle 接入；revision 与广播只在 durable commit 后前进。
- [x] 5.3 按该变更交付偏好 API/WebSocket/前端统一入口，移除独立本地偏好写入；保留设备数据与只读缓存边界。
- [x] 5.4 新存储与测试适配器自动进入清单；所有领域完成迁移后删除临时原始 I/O 例外。
