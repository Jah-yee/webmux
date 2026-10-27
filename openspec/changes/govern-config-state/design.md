## Context

当前问题是策略重复与观察入口不完整，并非 JSON 数量本身。源码调查见 [research.md](research.md)，当前机器清单见 `internal/archtest/persistence_inventory.json`。本 PR 按用户后续确认实施下述设计，并完整实现 #119。

## Goals / Non-Goals

**Goals:**

- 新领域无需复制锁、严格 JSON、原子提交和故障模型；新增存储必须登记并通过共享契约测试。
- 操作者能区分磁盘配置意图、活进程实际生效值、覆盖来源与安全降级。
- 持久化失败不丢弃原文件、不静默丢失凭据、不用旧内存覆盖已发布的新文件。
- 允许破坏性更改，不增加兼容双读/双写与自动迁移负担。

**Non-Goals:**

- 数据库、跨文件事务、多用户或跨实例同步；已有 JSON 不直接合并成一个文件。
- 网页修改 listen/token/state_dir/allowed_origins 等引导与安全配置。
- 改变现有 Tailscale 风险确认与精确所有权核对语义，或自动迁移旧版文件。
- 承诺任意文件系统/硬件的断电安全，或对动态程序进行完整静态数据流证明。

## Decisions

### 1. 层与所有权

TOML 定义引导、安全、进程策略及专用访问服务默认值；普通偏好进入`settings.json`；凭据、外部入口所有权、启动配置、仓库清单保持独立领域文档。下载缓存、日志、设备身份、草稿、Windows 托盘状态各有独立生命周期，不纳入通用偏好。

公网访问配置不是“修改后即时生效所以就属于偏好”。Cloudflare 与已有 Tailscale 网页覆盖仍通过专用验证/启停/关闭入口协议；不向通用 settings 暴露凭据。Tailscale 网页设置已经落地；迁移时保留 Funnel 显式确认、关闭旧入口后应用和 command 仅 TOML 的安全契约。

### 2. JSON descriptor + typed handle

在共享 `internal/statefile` 内定义无业务依赖的 descriptor：稳定 ID、相对路径、格式版本、MaxBytes、失败策略与敏感字段类别。锁名从完整路径加 `.lock` 派生，禁止调用者自行拼接。业务校验函数由服务注入，只有 `cmd/` 负责构造与接线。

运行时 descriptor 是唯一运行时目录清单来源；静态清单只描述当前真实代码，迁移完成后删除逐存储原始写入登记，改为校验注册 ID 与 typed handle。不得让 JSON 元数据注册表导入领域服务，也不得让 API 直接打开状态文件。

统一 `Open[T]` / `ReadSnapshot[T]` / `Commit` / `Remove` 的职责：

- Open：确保目录私有、取得独立生命周期排他非阻塞锁，然后有界读取与校验，返回 handle 或带状态的不可用服务。
- ReadSnapshot：只读观察，不 mkdir/chmod/加锁/初始化/修复；文件原子替换保证单文档完整，跨文档快照不声称事务一致性，锁占用状态不得伪装成已验证可写。
- Commit：持有服务 mutex、校验新值、编码、写临时文件、同步、关闭、原子替换、同步父目录，最后才发布成功事件。
- Remove：删除覆盖也属于提交，必须报告删除前失败与删除后目录同步不确定；不为 Reset 留原始 os.Remove 旁路。
- Close：幂等、释放锁，关闭后写入返回 closed；不删除锁文件，避免锁 inode 被替换后出现两个持有者。

数据文件/锁不得是符号链接或非普通文件；确认不存在需区分悬空链接。读取使用最多 MaxBytes+1 字节的 reader，不能仅 Stat 后无界 ReadFile。精确字段名、未知字段、重复键、非法 UTF-8、第二个 JSON 值、错误版本、缺失必需字段和超限深度均拒绝；领域校验继续覆盖端口、ID、凭据等语义。

### 3. 提交状态与故障模型

| 阶段                                    | 返回状态         | 内存与下一步                                                                           |
| --------------------------------------- | ---------------- | -------------------------------------------------------------------------------------- |
| 校验、临时写/Sync/Close、rename 前失败  | not_committed    | 保留旧内存与旧磁盘，不发成功事件；按具体 I/O 原因决定是否重试                          |
| rename 或 unlink 完成，父目录 Sync 失败 | commit_uncertain | 不声称回滚；冻结该存储写入，不广播成功；暴露最后确认值及不确定标记，重启后重新严格读取 |
| 父目录 Sync 成功                        | committed        | 切换内存；需要并发协议的领域推进 revision 并广播                                       |

共享错误携带 store ID、操作、结构化 kind、阶段与受控路径。kind 包含 missing、locked、corrupt、unsupported_version、too_large、permission、io、commit_uncertain、closed；不存在文件的初始化/默认值语义由领域定义，不当作 corruption。

服务/共享包不产生 HTTP 状态；API 将受影响服务不可用映射到 503、输入错误到 400、并发 revision 冲突到 409。日志和 CLI 只输出受控摘要，禁止包装可能含 JSON 原文或凭据的底层解码消息。

不是所有领域都需要 revision 和 updatedAt：磁盘版本 `v` 必须定义；settings 的 revision 支持乐观并发；其他文档保留有业务意义的元数据，不为统一外形添加虚构并发字段。持久化与外部服务变更也不是一个事务：Tailscale pending 必须先持久确认，再调用外部命令，再回读并提交 active；不确定时不能继续开放入口。

### 4. 故障隔离与安全例外

- launch/repositories/settings：保留原文件，相关管理能力不可用；直接终端与现存会话继续。
- push：损坏不改名、不重建 VAPID、不丢订阅；禁用推送并报告路径；修复或显式移走文件后重启恢复。正常首次安装缺失文件仍可生成密钥。
- tunnel：覆盖损坏/锁冲突不回落到 TOML enabled=true；保持入口关闭。显式关闭即使持久化失败仍先关闭当前进程入口，并报告“未持久化，重启前修复”，不能把失败当作成功保存。
- Tailscale ownership：不删除无法证明属于本实例的入口；遗留入口安全性不可证明时在绑定本地监听前拒绝启动。保留现有精确目标核对和 pending 对账。
- runtime：仍是短生命周期发现记录，单独保留 PID/进程身份验证。当前 runtime 锁冲突退化为无本机控制面的 TCP 服务，不擅自把此次基础设施治理变成全局实例启动策略改动。

### 5. 有效配置视图

使用同一个纯 resolver 生成带字段来源的安全 DTO，TOML 解码保留显式键集合。值恰好等于默认值、false/0/空字符串仍可能是显式 TOML/web 设置，不能用值比较猜来源。

来源枚举包含 `default`、`toml`、`web`、`cli`、`generated`；安全降级另用 `status` / `reason` 表达，不伪称 default。每个字段有来源与安全值，各存储有健康摘要；凭据仅提供 configured 布尔，启动命令/env、推送端点、订阅密钥、快捷指令正文等不进入诊断快照。

- configured：从明确指定的配置路径与 state_dir 只读解析；展示磁盘解析结果及 observedAt，不宣称它是当前监听/入口状态，不初始化缺失文件，也不为诊断抢写锁。
- effective：通过现有同 UID 本机 socket，读取服务已采用的内存快照，包含实例身份、启动时间、实际绑定地址、配置路径和各组观察时间。磁盘变化不改变其已采用值；不可达报错，不回退伪造活状态。
- `--json` 与人类可读输出使用同一 allowlist DTO；字段新增必须明确敏感性与来源，不允许直接 Marshal config.Config。
- 建议命令：`config show [--json]` 为 configured，`config show --live [--json]` 为 effective。**默认入口已经用户确认**；两个视图的语义与只读/失败要求不依赖默认值选择。
- 当前公网页面是否复用来源 DTO 可在对应 UI 变更中处理；配置来源暂不新增网页入口；#119 设置页使用独立偏好接口。

### 6. 破坏性升级

| 当前                                              | 目标                                                     |
| ------------------------------------------------- | -------------------------------------------------------- |
| launch-configs.json v1/v2 / launch-configs.lock         | launch-configs.json v2 only / launch-configs.json.lock           |
| repositories.json / repositories.lock             | repositories.json / repositories.json.lock               |
| tunnel.json / tunnel-config.lock                  | tunnel-settings.json / tunnel-settings.json.lock         |
| tailscale-settings.json / tailscale-settings.lock | tailscale-settings.json / tailscale-settings.json.lock   |
| tailscale.json / tailscale.lock                   | tailscale-ownership.json / tailscale-ownership.json.lock |
| push.json / 无 flock                              | push.json / push.json.lock                               |
| run/server.json / run/server.lock                 | run/server.json / run/server.json.lock                   |
| 规划中的 settings.json / settings.lock            | settings.json / settings.json.lock                       |

不双读/双写、不后台重命名、不自动导入旧浏览器偏好；#119 的兼容迁移任务需在其实施前明确删除或改写。本次不改写已归档历史。

运维顺序：停止全部旧进程和相关入口 → 备份状态 → 按发布说明显式整理/重置旧文件 → 启动新版。修改锁名不会与旧进程互斥，因而禁止新旧版本共用目录并发运行。

旧 `tailscale.json` 存在（包括损坏或不可读）时，在监听前阻止启动；不因新文件缺失或新旧同时存在而选择一份继续。提示先使用旧版本撤销记录所属入口，或人工核实精确目标后处理旧文件；不执行全局 reset，不自动删除旧文件。这是用户已确认的保护措施，不是兼容读取。旧 tunnel.json 的存在也应让该入口保持关闭并报告 legacy_layout，避免忽略旧禁用覆盖而从 TOML 意外重新启用；不自动覆盖或导入。

### 7. 机械治理分层

第一层：全平台 AST 字符串/文件写入引用扫描，精确到文件/函数/符号/次数；存储与明确非服务器状态例外分类；机器清单与文档表格精确比较；负向样例；CI 必跑。登记删除后也必须清除，不能无限积累白名单。

第二层：领域文件禁止原始写/锁入口；每个注册存储必须绑定契约测试适配器，新增 ID 缺少测试即失败。可保留经过原因登记的 bootstrap/runtime/缓存等生命周期例外，不能使用整个目录豁免。

第三层是行为矩阵：私有权限、符号链接/悬空链接拒绝、跨进程锁、严格解析、大小/深度上限、每个提交阶段故障、删除的目录同步失败、重开后状态、广播时机与敏感值泄漏。静态 AST 不声称证明这些性质。

## Risks / Trade-offs

- 独立 JSON 不提供跨领域事务；需要跨文件原子操作时必须重新评估数据边界，不在通用层堆两阶段提交。
- 旧部署升级需要显式停机整理；不兼容版本并发共用目录不受支持。
- 故障测试只能证明所模拟的系统调用模型；Linux/macOS 原生执行覆盖平台差异，不声称验证所有文件系统。
- 活快照不是全服务全局事务；每组状态标记观察时间，不持有跨服务大锁。
- 第一阶段静态扫描对动态路径、外部命令和新 I/O 库有边界，已在治理文档明示；后续必须收敛写入能力而非永久扩展例外。

## Migration Plan

先合并规格与现状守卫；再扩展 statefile 并运行契约/故障测试；迁移各领域并收紧登记；最后交付 configured/effective 与来源测试。#119 使用新基础设施推进偏好同步，不与当前 PR 捆绑前端重构。每阶段在独立提交/PR 中更新真实清单，完成后才勾选运行时任务。

## Open Questions

- config show 默认显示磁盘 configured、显式 --live 显示进程 effective，还是默认读取进程？已向用户询问；默认值确认前不实施 CLI 路由。推荐前者，保持诊断可离线运行且无副作用。
